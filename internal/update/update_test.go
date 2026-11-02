package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeGitHub is the releases API and its downloads, over TLS on
// 127.0.0.1. Tests never reach the real network.
type fakeGitHub struct {
	srv      *httptest.Server
	tag      string
	binary   []byte
	sums     string // checksums.txt; "" builds a correct one
	apiBody  string // overrides the releases/latest body
	apiCalls atomic.Int32
	mu       sync.Mutex
	extra    http.HandlerFunc // handles any other path
	override http.HandlerFunc // when set, handles every request (setHandler)
}

// setHandler makes h handle every request; next is the fake's own
// handler, for wrapping.
func (f *fakeGitHub) setHandler(h func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.override = func(w http.ResponseWriter, r *http.Request) { h(w, r, f.serveFake) }
}

func newFake(t *testing.T, tag string) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{tag: tag, binary: []byte("#!new sshelf binary\n")}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	o := f.override
	f.mu.Unlock()
	if o != nil {
		o(w, r)
		return
	}
	f.serveFake(w, r)
}

func (f *fakeGitHub) serveFake(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/repos/" + Repo + "/releases/latest":
		f.apiCalls.Add(1)
		if f.apiBody != "" {
			_, _ = io.WriteString(w, f.apiBody)
			return
		}
		assets := []map[string]string{}
		for _, n := range []string{"sshelf_linux_amd64", "sshelf_darwin_arm64", ChecksumsAsset} {
			assets = append(assets, map[string]string{"name": n, "browser_download_url": f.srv.URL + "/download/" + n})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": f.tag, "assets": assets, "body": "\x1b]52;c;ZXZpbA==\x07"})
	case "/download/" + ChecksumsAsset:
		sums := f.sums
		if sums == "" {
			h := sha256.Sum256(f.binary)
			sums = hex.EncodeToString(h[:]) + "  sshelf_linux_amd64\n" + strings.Repeat("0", 64) + "  sshelf_darwin_arm64\n"
		}
		_, _ = io.WriteString(w, sums)
	case "/download/sshelf_linux_amd64", "/download/sshelf_darwin_arm64":
		_, _ = w.Write(f.binary)
	default:
		if f.extra != nil {
			f.extra(w, r)
			return
		}
		http.NotFound(w, r)
	}
}

func (f *fakeGitHub) client(current string) *Client {
	c := New(current)
	c.baseURL = f.srv.URL
	hc := f.srv.Client()
	hc.CheckRedirect = httpsOnly
	c.http = hc
	return c
}

func TestFR_UPD_001_ParseIsStrict(t *testing.T) {
	for s, want := range map[string]bool{
		"v0.1.0": true, "v1.20.300": true, "v0.0.0": true,
		"0.1.0": false, "v1.2": false, "v1.2.3.4": false, "v01.2.3": false, "v1.2.3-rc1": false,
		"v0.0.0-e2e": false, "v1.2.3+meta": false, "v1.2.3-4-gabcdef": false, "dev": false, "": false,
		"v1.2.x": false, "v1..3": false, "v1234567890.0.0": false, "v1.2.3\x1b[31m": false, "v١.2.3": false,
	} {
		if _, ok := Parse(s); ok != want {
			t.Errorf("Parse(%q) ok = %v, want %v", s, ok, want)
		}
	}
	a, _ := Parse("v0.9.10")
	b, _ := Parse("v0.10.0")
	if !a.Less(b) || b.Less(a) || a.Less(a) || a.String() != "v0.9.10" {
		t.Errorf("ordering or String wrong: %v %v", a, b)
	}
}

func TestFR_UPD_001_Enabled(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == DisableEnv {
				return v
			}
			return ""
		}
	}
	for _, tc := range []struct {
		cur, env string
		want     bool
	}{
		{"v0.1.0", "", true},
		{"v0.1.0", "0", true},
		{"v0.1.0", "1", false},
		{"dev", "", false},
		{"v0.0.0-e2e", "", false},
	} {
		if got := Enabled(env(tc.env), tc.cur); got != tc.want {
			t.Errorf("Enabled(%q, %s=%q) = %v, want %v", tc.cur, DisableEnv, tc.env, got, tc.want)
		}
	}
}

func TestFR_UPD_001_NewerReportedAndCached(t *testing.T) {
	f := newFake(t, "v0.4.0")
	dir := t.TempDir()
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	c := f.client("v0.1.0")
	v, ok := c.Check(context.Background(), dir, now)
	if !ok || v.String() != "v0.4.0" {
		t.Fatalf("Check = %v, %v; want v0.4.0, true", v, ok)
	}
	if fi, err := os.Stat(filepath.Join(dir, StateFile)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("state file: %v, mode %v; want 0600", err, fi)
	}
	// Within 24 h the answer comes from the state file.
	v, ok = c.Check(context.Background(), dir, now.Add(23*time.Hour))
	if !ok || v.String() != "v0.4.0" || f.apiCalls.Load() != 1 {
		t.Fatalf("second check = %v, %v with %d API calls; want v0.4.0, true, 1", v, ok, f.apiCalls.Load())
	}
	// After 24 h it asks again.
	f.tag = "v0.5.0"
	v, ok = c.Check(context.Background(), dir, now.Add(25*time.Hour))
	if !ok || v.String() != "v0.5.0" || f.apiCalls.Load() != 2 {
		t.Fatalf("third check = %v, %v with %d API calls; want v0.5.0, true, 2", v, ok, f.apiCalls.Load())
	}
	// A clock set back asks again rather than trusting a future stamp.
	if _, _ = c.Check(context.Background(), dir, now); f.apiCalls.Load() != 3 {
		t.Fatalf("clock set back: %d API calls, want 3", f.apiCalls.Load())
	}
}

func TestFR_UPD_001_SameOrOlderNotReported(t *testing.T) {
	for _, tag := range []string{"v0.4.0", "v0.3.9"} {
		f := newFake(t, tag)
		if v, ok := f.client("v0.4.0").Check(context.Background(), t.TempDir(), time.Now()); ok {
			t.Errorf("latest %s, running v0.4.0: reported %v", tag, v)
		}
	}
}

func TestSEC_UPD_001_OnlyUserAgentSent(t *testing.T) {
	f := newFake(t, "v0.4.0")
	var hdr http.Header
	var mu sync.Mutex
	f.setHandler(func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
		mu.Lock()
		hdr = r.Header.Clone()
		mu.Unlock()
		if r.URL.RawQuery != "" || r.Method != http.MethodGet {
			t.Errorf("request %s %s: want a plain GET", r.Method, r.URL)
		}
		next(w, r)
	})
	f.client("v0.1.0").Check(context.Background(), t.TempDir(), time.Now())
	mu.Lock()
	defer mu.Unlock()
	if hdr.Get("User-Agent") != "sshelf/v0.1.0" {
		t.Errorf("User-Agent %q, want sshelf/v0.1.0", hdr.Get("User-Agent"))
	}
	for k := range hdr {
		switch k {
		case "User-Agent", "Accept", "Accept-Encoding":
		default:
			t.Errorf("unexpected header %s: %q", k, hdr.Get(k))
		}
	}
}

func TestSEC_UPD_001_FailuresAreSilent(t *testing.T) {
	big := `{"tag_name":"v9.0.0","pad":"` + strings.Repeat("x", maxAPIResponse) + `"}`
	for name, body := range map[string]string{
		"not JSON":       "<html>rate limited</html>",
		"bad tag":        `{"tag_name":"v1.2.3-rc1"}`,
		"escape tag":     `{"tag_name":"v9.0.0\u001b[2J"}`,
		"oversized body": big,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFake(t, "")
			f.apiBody = body
			dir := t.TempDir()
			if v, ok := f.client("v0.1.0").Check(context.Background(), dir, time.Now()); ok {
				t.Fatalf("reported %v", v)
			}
			if _, err := os.Stat(filepath.Join(dir, StateFile)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("a failed check was recorded: %v", err)
			}
		})
	}
	t.Run("HTTP error", func(t *testing.T) {
		f := newFake(t, "v0.4.0")
		f.setHandler(func(w http.ResponseWriter, _ *http.Request, _ http.HandlerFunc) {
			http.Error(w, "nope", http.StatusForbidden)
		})
		if _, ok := f.client("v0.1.0").Check(context.Background(), t.TempDir(), time.Now()); ok {
			t.Fatal("reported a version on HTTP 403")
		}
	})
	t.Run("plain HTTP refused", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("a plain HTTP server was contacted")
			_, _ = w.Write([]byte(`{"tag_name":"v9.0.0"}`))
		}))
		defer srv.Close()
		c := New("v0.1.0")
		c.baseURL = srv.URL
		if _, ok := c.Check(context.Background(), t.TempDir(), time.Now()); ok {
			t.Fatal("reported a version over plain HTTP")
		}
	})
	t.Run("timeout", func(t *testing.T) {
		f := newFake(t, "v0.4.0")
		release := make(chan struct{})
		defer close(release)
		f.setHandler(func(http.ResponseWriter, *http.Request, http.HandlerFunc) { <-release })
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		start := time.Now()
		if _, ok := f.client("v0.1.0").Check(ctx, t.TempDir(), time.Now()); ok {
			t.Fatal("reported a version")
		}
		if d := time.Since(start); d > 5*time.Second {
			t.Fatalf("check took %v", d)
		}
	})
	t.Run("corrupt state file", func(t *testing.T) {
		f := newFake(t, "v0.4.0")
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, StateFile), []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		if v, ok := f.client("v0.1.0").Check(context.Background(), dir, time.Now()); !ok || v.String() != "v0.4.0" {
			t.Fatalf("Check = %v, %v; want v0.4.0 from the API", v, ok)
		}
	})
	t.Run("dev build never asks", func(t *testing.T) {
		f := newFake(t, "v0.4.0")
		if _, ok := f.client("dev").Check(context.Background(), t.TempDir(), time.Now()); ok || f.apiCalls.Load() != 0 {
			t.Fatalf("dev build checked: %d calls", f.apiCalls.Load())
		}
	})
}

// installed writes a fake old binary and returns its path.
func installed(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "sshelf")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil { //nolint:gosec // a fake executable
		t.Fatal(err)
	}
	return exe
}

func TestFR_UPD_003_ReplacesBinary(t *testing.T) {
	f := newFake(t, "v0.4.0")
	exe := installed(t)
	// The binary may be reached through a symlink, as in ~/bin/sshelf.
	link := filepath.Join(t.TempDir(), "sshelf-link")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := f.client("v0.1.0").Apply(context.Background(), Target{Exe: link, GOOS: "linux", GOARCH: "amd64"}, &out); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(exe) //nolint:gosec // the test's own file
	if !bytes.Equal(b, f.binary) {
		t.Fatalf("binary is %q, want the release", b)
	}
	fi, _ := os.Stat(exe)
	if fi.Mode().Perm() != 0o755 {
		t.Errorf("mode %v, want 0755", fi.Mode().Perm())
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced instead of its target")
	}
	for _, want := range []string{"v0.1.0 → v0.4.0", "SHA-256 verified"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output %q lacks %q", out.String(), want)
		}
	}
	assertNoTemp(t, filepath.Dir(exe))
}

func TestFR_UPD_003_AlreadyLatest(t *testing.T) {
	f := newFake(t, "v0.4.0")
	exe := installed(t)
	var out bytes.Buffer
	if err := f.client("v0.4.0").Apply(context.Background(), Target{Exe: exe, GOOS: "linux", GOARCH: "amd64"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "sshelf v0.4.0 is the latest version.") {
		t.Errorf("output %q", out.String())
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" { //nolint:gosec // the test's own file
		t.Error("binary changed")
	}
}

func TestFR_UPD_003_DevBuildRefused(t *testing.T) {
	f := newFake(t, "v0.4.0")
	err := f.client("dev").Apply(context.Background(), Target{Exe: installed(t), GOOS: "linux", GOARCH: "amd64"}, &bytes.Buffer{})
	if !errors.Is(err, ErrDevBuild) || f.apiCalls.Load() != 0 {
		t.Fatalf("err %v with %d API calls; want ErrDevBuild and none", err, f.apiCalls.Load())
	}
}

func TestSEC_UPD_002_ChecksumMismatchKeepsBinary(t *testing.T) {
	for name, sums := range map[string]string{
		"wrong sum":   strings.Repeat("ab", 32) + "  sshelf_linux_amd64\n",
		"no line":     strings.Repeat("ab", 32) + "  sshelf_darwin_arm64\n",
		"short sum":   "abcd  sshelf_linux_amd64\n",
		"not hex":     strings.Repeat("zz", 32) + "  sshelf_linux_amd64\n",
		"prefix name": strings.Repeat("ab", 32) + "  sshelf_linux_amd64.tar.gz\n",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFake(t, "v0.4.0")
			f.sums = sums
			exe := installed(t)
			err := f.client("v0.1.0").Apply(context.Background(), Target{Exe: exe, GOOS: "linux", GOARCH: "amd64"}, &bytes.Buffer{})
			if err == nil {
				t.Fatal("update succeeded")
			}
			if b, _ := os.ReadFile(exe); string(b) != "old" { //nolint:gosec // the test's own file
				t.Fatal("binary changed")
			}
			assertNoTemp(t, filepath.Dir(exe))
		})
	}
	t.Run("matching sum in a *binary line", func(t *testing.T) {
		f := newFake(t, "v0.4.0")
		h := sha256.Sum256(f.binary)
		f.sums = hex.EncodeToString(h[:]) + " *sshelf_linux_amd64\n"
		if err := f.client("v0.1.0").Apply(context.Background(), Target{Exe: installed(t), GOOS: "linux", GOARCH: "amd64"}, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSEC_UPD_002_MissingAssetOrPlatform(t *testing.T) {
	f := newFake(t, "v0.4.0")
	exe := installed(t)
	err := f.client("v0.1.0").Apply(context.Background(), Target{Exe: exe, GOOS: "plan9", GOARCH: "386"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "no sshelf_plan9_386 binary") {
		t.Fatalf("err %v", err)
	}
}

func TestSEC_UPD_002_OversizedDownloadRefused(t *testing.T) {
	f := newFake(t, "v0.4.0")
	f.binary = bytes.Repeat([]byte{0}, maxBinary+1)
	exe := installed(t)
	err := f.client("v0.1.0").Apply(context.Background(), Target{Exe: exe, GOOS: "linux", GOARCH: "amd64"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("err %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" { //nolint:gosec // the test's own file
		t.Fatal("binary changed")
	}
	assertNoTemp(t, filepath.Dir(exe))
}

func TestSEC_UPD_002_RedirectToHTTPRefused(t *testing.T) {
	f := newFake(t, "v0.4.0")
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("followed a redirect to plain HTTP")
		_, _ = w.Write([]byte("evil"))
	}))
	defer plain.Close()
	f.extra = func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/evil", http.StatusFound)
	}
	f.apiBody = fmt.Sprintf(`{"tag_name":"v0.4.0","assets":[{"name":"sshelf_linux_amd64","browser_download_url":%q},{"name":"checksums.txt","browser_download_url":%q}]}`,
		f.srv.URL+"/redirect/bin", f.srv.URL+"/download/checksums.txt")
	exe := installed(t)
	if err := f.client("v0.1.0").Apply(context.Background(), Target{Exe: exe, GOOS: "linux", GOARCH: "amd64"}, &bytes.Buffer{}); err == nil {
		t.Fatal("update succeeded")
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" { //nolint:gosec // the test's own file
		t.Fatal("binary changed")
	}
}

func TestFR_UPD_003_NotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	f := newFake(t, "v0.4.0")
	exe := installed(t)
	dir := filepath.Dir(exe)
	if err := os.Chmod(dir, 0o500); err != nil { //nolint:gosec // a read-only test dir
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // restore for cleanup
	err := f.client("v0.1.0").Apply(context.Background(), Target{Exe: exe, GOOS: "linux", GOARCH: "amd64"}, &bytes.Buffer{})
	if !errors.Is(err, ErrNotWritable) {
		t.Fatalf("err %v, want ErrNotWritable", err)
	}
}

func TestFR_UPD_003_PackageManagerTree(t *testing.T) {
	for _, p := range []string{"/opt/homebrew/Cellar/sshelf/0.1.0/bin/sshelf", "/usr/local/Cellar/sshelf/0.1.0/bin/sshelf", "/nix/store/abc-sshelf/bin/sshelf"} {
		if !managed(p) {
			t.Errorf("managed(%q) = false", p)
		}
	}
	if managed("/home/me/.local/bin/sshelf") {
		t.Error("a user bin dir counted as managed")
	}
}

func assertNoTemp(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".sshelf-update-") {
			t.Errorf("temporary file %s left behind", e.Name())
		}
	}
}
