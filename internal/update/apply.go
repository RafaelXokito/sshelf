package update

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	// ChecksumsAsset is the release's SHA-256 list, as written by
	// sha256sum: "<hex>  <name>" per line.
	ChecksumsAsset = "checksums.txt"

	maxChecksums = 64 << 10
	maxBinary    = 64 << 20 // NFR-SIZE-002 keeps a release under 20 MB
)

// Errors of Apply.
var (
	// ErrDevBuild means the running binary is not a release, so there is
	// nothing to compare with or replace (D-110).
	ErrDevBuild = errors.New("this is a development build, not a release")
	// ErrNotWritable means sshelf cannot replace its own binary; the
	// message says how to update instead (D-111).
	ErrNotWritable = errors.New("cannot replace the installed binary")
	// ErrChecksum means the download did not match checksums.txt.
	ErrChecksum = errors.New("the download does not match checksums.txt")
)

// AssetName is the release asset for an OS and architecture.
func AssetName(goos, goarch string) string { return "sshelf_" + goos + "_" + goarch }

// Target is the binary to replace and the platform to download for.
type Target struct {
	Exe    string // os.Executable() of the running sshelf
	GOOS   string
	GOARCH string
}

// Apply runs `sshelf update` (FR-UPD-003): it prints the running and the
// latest version, and when the latest is newer downloads the asset for
// t's platform, checks its SHA-256 against the release's checksums.txt
// and swaps it in for t.Exe with a rename in the same directory. Progress
// goes to out. The binary is untouched unless every step succeeded.
func (c *Client) Apply(ctx context.Context, t Target, out io.Writer) error {
	cur, ok := Parse(c.current)
	if !ok {
		return ErrDevBuild
	}
	r, err := c.Latest(ctx)
	if err != nil {
		return err
	}
	if !cur.Less(r.Version) {
		_, _ = fmt.Fprintf(out, "sshelf %s is the latest version.\n", cur)
		return nil
	}
	_, _ = fmt.Fprintf(out, "Updating sshelf %s → %s\n", cur, r.Version)

	name := AssetName(t.GOOS, t.GOARCH)
	binURL, ok := r.Assets[name]
	if !ok {
		return fmt.Errorf("release %s has no %s binary", r.Version, name)
	}
	sumURL, ok := r.Assets[ChecksumsAsset]
	if !ok {
		return fmt.Errorf("release %s has no %s", r.Version, ChecksumsAsset)
	}

	exe, err := filepath.EvalSymlinks(t.Exe)
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	if managed(exe) {
		return fmt.Errorf("%w: %s is managed by a package manager", ErrNotWritable, exe)
	}
	tmp, err := os.CreateTemp(dir, ".sshelf-update-*")
	if err != nil {
		// Permission denied, a read-only file system: either way sshelf
		// cannot put a new binary next to the old one.
		return fmt.Errorf("%w: %s is not writable", ErrNotWritable, dir)
	}
	done := false
	defer func() {
		if !done {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	sums, err := c.get(ctx, sumURL, maxChecksums, "")
	if err != nil {
		return err
	}
	want, err := checksumFor(sums, name)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "Downloading %s\n", name)
	got, err := c.download(ctx, binURL, tmp)
	if err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		return ErrChecksum
	}
	if err := tmp.Chmod(0o755); err != nil { //nolint:gosec // an executable, as installed
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), exe); err != nil {
		return err
	}
	done = true
	_, _ = fmt.Fprintf(out, "Updated %s to %s (SHA-256 verified).\n", exe, r.Version)
	return nil
}

// download writes the body of u to f, at most maxBinary bytes, and
// returns its SHA-256.
func (c *Client) download(ctx context.Context, u string, f *os.File) ([]byte, error) {
	if !strings.HasPrefix(u, "https://") {
		return nil, errors.New("update: not an HTTPS URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "sshelf/"+c.current)
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update: download answered HTTP %d", resp.StatusCode)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxBinary+1))
	if err != nil {
		return nil, err
	}
	if n > maxBinary {
		return nil, fmt.Errorf("update: download is larger than %d bytes", maxBinary)
	}
	return h.Sum(nil), nil
}

// checksumFor finds name's SHA-256 in a sha256sum listing.
func checksumFor(sums []byte, name string) ([]byte, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		sum, err := hex.DecodeString(fields[0])
		if err != nil || len(sum) != sha256.Size {
			return nil, fmt.Errorf("update: %s has a malformed line for %s", ChecksumsAsset, name)
		}
		return sum, nil
	}
	return nil, fmt.Errorf("update: %s has no line for %s", ChecksumsAsset, name)
}

// managed reports whether exe lives in a package manager's tree, which
// sshelf must not change behind the manager's back (Homebrew's Cellar,
// Nix's store).
func managed(exe string) bool {
	return strings.Contains(exe, "/Cellar/") || strings.HasPrefix(exe, "/nix/store/")
}

// HowToUpdate is the advice printed when sshelf cannot replace itself.
const HowToUpdate = `Update sshelf the way you installed it:
  Homebrew:    brew upgrade sshelf
  Go:          go install github.com/RafaelXokito/sshelf/cmd/sshelf@latest
  By hand:     download sshelf_<os>_<arch> from ` + ReleasesURL + `
               and replace the binary (check it against checksums.txt).`
