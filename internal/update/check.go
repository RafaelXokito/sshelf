package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/paths"
)

const (
	// Repo is the GitHub repository releases come from.
	Repo = "RafaelXokito/sshelf"
	// ReleasesURL is where a user downloads a release by hand.
	ReleasesURL = "https://github.com/" + Repo + "/releases/latest"
	// DisableEnv turns the startup check off when set to "1" (D-110).
	DisableEnv = "SSHELF_NO_UPDATE_CHECK"
	// Interval is how long a check's answer is reused (D-110).
	Interval = 24 * time.Hour
	// StateFile is the check's state file in the config directory.
	StateFile = "update-check.json"

	defaultBaseURL = "https://api.github.com"
	checkTimeout   = 10 * time.Second
	maxAPIResponse = 1 << 20 // the releases/latest JSON
	maxStateFile   = 4 << 10
)

// Client talks to the GitHub releases API for one running version.
type Client struct {
	current string // the running version, sent as the User-Agent
	// baseURL and http are fixed in production; tests in this package
	// point them at an httptest TLS server on 127.0.0.1.
	baseURL string
	http    *http.Client
}

// New returns a client for the running version current.
func New(current string) *Client {
	return &Client{current: current, baseURL: defaultBaseURL, http: &http.Client{CheckRedirect: httpsOnly}}
}

// httpsOnly refuses a redirect to anything but HTTPS (SEC-UPD-001).
func httpsOnly(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "https" {
		return errors.New("update: redirect to a non-HTTPS URL refused")
	}
	if len(via) >= 10 {
		return errors.New("update: too many redirects")
	}
	return nil
}

// Enabled reports whether the startup check runs: not for a development
// build (current is not a release version), and not when
// SSHELF_NO_UPDATE_CHECK=1 (D-110).
func Enabled(getenv func(string) string, current string) bool {
	_, ok := Parse(current)
	return ok && getenv(DisableEnv) != "1"
}

// Release is the latest release as the API describes it.
type Release struct {
	Version Version
	Assets  map[string]string // asset name -> HTTPS download URL
}

// get fetches u over HTTPS and returns at most limit bytes of its body; a
// longer body is an error.
func (c *Client) get(ctx context.Context, u string, limit int64, accept string) ([]byte, error) {
	parsed, err := url.Parse(u)
	if err != nil || parsed.Scheme != "https" {
		return nil, errors.New("update: not an HTTPS URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	// The User-Agent is the only thing sshelf says about itself (D-110).
	req.Header.Set("User-Agent", "sshelf/"+c.current)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update: %s answered HTTP %d", parsed.Host, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("update: response from %s is larger than %d bytes", parsed.Host, limit)
	}
	return b, nil
}

// Latest asks the API for the latest release. Its tag must be a release
// version and its asset URLs HTTPS; other assets are left out.
func (c *Client) Latest(ctx context.Context) (Release, error) {
	b, err := c.get(ctx, c.baseURL+"/repos/"+Repo+"/releases/latest", maxAPIResponse, "application/vnd.github+json")
	if err != nil {
		return Release{}, err
	}
	var body struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		return Release{}, errors.New("update: the release description is not valid JSON")
	}
	v, ok := Parse(body.TagName)
	if !ok {
		return Release{}, fmt.Errorf("update: the latest release tag %q is not a version", domain.Sanitize(truncate(body.TagName, 40)))
	}
	r := Release{Version: v, Assets: map[string]string{}}
	for _, a := range body.Assets {
		if strings.HasPrefix(a.URL, "https://") {
			r.Assets[a.Name] = a.URL
		}
	}
	return r, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// State is what the last check found, kept in StateFile (0600).
type State struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

// LoadState reads the state file in dir. A missing or unreadable file is
// the zero State: the next check simply runs.
func LoadState(dir string) State {
	f, err := os.Open(filepath.Join(dir, StateFile)) //nolint:gosec // dir comes from internal/paths
	if err != nil {
		return State{}
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxStateFile))
	var s State
	if err != nil || json.Unmarshal(b, &s) != nil {
		return State{}
	}
	return s
}

// SaveState writes s to the state file in dir, mode 0600, through a
// temporary file and a rename.
func SaveState(dir string, s State) error {
	if _, err := paths.EnsureDir(dir); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".update-check-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // fails once renamed
	if err := tmp.Chmod(paths.FileMode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, StateFile))
}

// Check is the startup check (FR-UPD-001). It reuses the state file's
// answer when it is less than Interval old, and otherwise asks the API
// (bounded by a 10-second timeout) and records the answer. It returns the
// latest version and true only when that is newer than the running one.
// Every failure is silent: no version and false.
func (c *Client) Check(ctx context.Context, dir string, now time.Time) (Version, bool) {
	cur, ok := Parse(c.current)
	if !ok {
		return Version{}, false
	}
	s := LoadState(dir)
	latest, cached := Parse(s.Latest)
	if age := now.Sub(s.CheckedAt); !cached || age < 0 || age >= Interval {
		ctx, cancel := context.WithTimeout(ctx, checkTimeout)
		defer cancel()
		r, err := c.Latest(ctx)
		if err != nil {
			// Not recorded: a failed check is retried at the next start.
			return Version{}, false
		}
		latest = r.Version
		_ = SaveState(dir, State{CheckedAt: now.UTC().Truncate(time.Second), Latest: latest.String()})
	}
	return latest, cur.Less(latest)
}
