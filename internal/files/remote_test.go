package files

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/crypto/ssh"

	"github.com/RafaelXokito/sshelf/internal/sshclient"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// The remote side against sshtest's sftp subsystem (ARCH-TEST-004).

var bg = context.Background()

func sftpSrv(t *testing.T, opts ...sshtest.Option) *sshtest.Server {
	t.Helper()
	return sshtest.New(t, append([]sshtest.Option{sshtest.WithPassword("u", "pw"), sshtest.WithSFTP(t.TempDir())}, opts...)...)
}

func dialLease(t *testing.T, srv *sshtest.Server) *sshclient.Lease {
	t.Helper()
	c, err := ssh.Dial("tcp", srv.Addr(), &ssh.ClientConfig{
		User:            "u",
		Auth:            []ssh.AuthMethod{ssh.Password("pw")},
		HostKeyCallback: ssh.FixedHostKey(srv.HostKeys()[0]),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, l := sshclient.NewConn(c)
	return l
}

func openRemote(t *testing.T, srv *sshtest.Server) *RemoteFS {
	t.Helper()
	r, err := OpenRemote(dialLease(t, srv))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func countType(srv *sshtest.Server, typ string) int {
	n := 0
	for _, c := range srv.Closes() {
		if c.Type == typ {
			n++
		}
	}
	return n
}

func names(l Listing) []string {
	var out []string
	for _, e := range l.Entries {
		out = append(out, e.Name)
	}
	slices.Sort(out)
	return out
}

func writeRemote(t *testing.T, srv *sshtest.Server, name, data string) {
	t.Helper()
	p := srv.SFTPDir(name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestARCH_SFTP_001_TwoChannels(t *testing.T) {
	srv := sftpSrv(t)
	writeRemote(t, srv, "a.txt", "a")
	r := openRemote(t, srv)
	if r.Channels() != 2 {
		t.Fatalf("Channels() = %d, want 2", r.Channels())
	}
	if subs := srv.RequestsOfType("subsystem"); len(subs) != 2 {
		t.Fatalf("subsystem requests = %+v, want 2", subs)
	}
	if r.ConnID() == 0 {
		t.Fatal("ConnID() = 0")
	}
	l, err := r.List(bg, r.Start())
	if err != nil || !slices.Equal(names(l), []string{"a.txt"}) {
		t.Fatalf("List = %v, %v", names(l), err)
	}
	if r.counter.n.Load() == 0 {
		t.Fatal("the browse channel's bytes were not counted")
	}
}

func TestARCH_SFTP_001_OneChannelFallback(t *testing.T) {
	srv := sftpSrv(t, sshtest.WithMaxSessions(1))
	writeRemote(t, srv, "a.txt", "a")
	r := openRemote(t, srv)
	if r.Channels() != 1 {
		t.Fatalf("Channels() = %d, want 1", r.Channels())
	}
	if _, err := r.List(bg, r.Start()); err != nil {
		t.Fatal(err)
	}
	f, err := r.transfer.Create(path.Join(r.Start(), "b.txt"))
	if err != nil {
		t.Fatalf("transfer on the shared channel: %v", err)
	}
	if _, err := f.Write([]byte("b")); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if b, _ := os.ReadFile(srv.SFTPDir("b.txt")); string(b) != "b" {
		t.Fatalf("b.txt = %q", b)
	}
}

func TestARCH_SFTP_001_CloseReleasesOnce(t *testing.T) {
	srv := sftpSrv(t)
	lease := dialLease(t, srv)
	other, err := lease.Conn().Lease()
	if err != nil {
		t.Fatal(err)
	}
	r, err := OpenRemote(lease)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	_ = r.Close()
	if !srv.WaitFor(time.Second, func() bool { return countType(srv, "close") == 2 }) {
		t.Fatalf("closes = %+v, want both channels closed", srv.Closes())
	}
	if countType(srv, "disconnect") != 0 {
		t.Fatal("the connection closed while another lease is held")
	}
	s, err := other.Client().NewSession()
	if err != nil {
		t.Fatalf("other holder: %v", err)
	}
	_ = s.Close()
	other.Release()
	if !srv.WaitFor(time.Second, func() bool { return countType(srv, "disconnect") == 1 }) {
		t.Fatalf("closes = %+v, want one connection close", srv.Closes())
	}
}

func TestARCH_SFTP_001_NoSFTPReleases(t *testing.T) {
	srv := sshtest.New(t, sshtest.WithPassword("u", "pw"))
	if _, err := OpenRemote(dialLease(t, srv)); !errors.Is(err, ErrNoSFTP) {
		t.Fatalf("OpenRemote = %v, want ErrNoSFTP", err)
	}
	if !srv.WaitFor(time.Second, func() bool { return countType(srv, "disconnect") == 1 }) {
		t.Fatalf("closes = %+v, want the connection closed", srv.Closes())
	}
}

func TestARCH_SFTP_002_ListingBudget(t *testing.T) {
	old := listBudget
	listBudget = 256 << 10
	t.Cleanup(func() { listBudget = old })
	srv := sftpSrv(t, sshtest.WithSFTPHostile(sshtest.SFTPHostile{Entries: 20_000, NameLen: 64}))
	r := openRemote(t, srv)
	l, err := r.List(bg, sshtest.ManyDir)
	if err != nil {
		t.Fatal(err)
	}
	if !l.Truncated || len(l.Entries) == 0 || len(l.Entries) >= 20_000 {
		t.Fatalf("Truncated %v with %d entries; want a cut listing", l.Truncated, len(l.Entries))
	}
	// The channel still works after the cancelled listing.
	if l, err := r.List(bg, r.Start()); err != nil || l.Truncated {
		t.Fatalf("next listing: %v, truncated %v", err, l.Truncated)
	}
}

func TestARCH_SFTP_002_ListingTimeout(t *testing.T) {
	old := listTimeout
	listTimeout = time.Nanosecond
	t.Cleanup(func() { listTimeout = old })
	r := openRemote(t, sftpSrv(t))
	if _, err := r.List(bg, r.Start()); err == nil {
		t.Fatal("listing outlived its timeout")
	}
}

func TestSEC_SFTP_001_HostileNames(t *testing.T) {
	r := openRemote(t, sftpSrv(t, sshtest.WithSFTPHostile(sshtest.SFTPHostile{Names: true})))
	l, err := r.List(bg, sshtest.HostileDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(l); !slices.Equal(got, []string{"b", "evil", "ok.txt"}) {
		t.Fatalf("names = %q", got)
	}
	if l.Skipped != 8 {
		t.Fatalf("Skipped = %d, want 8", l.Skipped)
	}
	for _, e := range l.Entries {
		if CheckName(e.Name) != nil || strings.ContainsFunc(e.Display, unicode.IsControl) {
			t.Errorf("unsafe entry %q", e.Name)
		}
	}
	n, err := r.Count(bg, sshtest.HostileDir, 100, false)
	if err != nil || n != 3 {
		t.Fatalf("Count = %d, %v; want the 3 safe names", n, err)
	}
	for _, bad := range []string{"../x", "a/b", "..", "", "x\x00"} {
		if err := r.Mkdir(bg, r.Start(), bad); !errors.Is(err, ErrBadName) {
			t.Errorf("Mkdir(%q) = %v", bad, err)
		}
		if err := r.Rename(bg, r.Start(), "ok", bad); !errors.Is(err, ErrBadName) {
			t.Errorf("Rename to %q = %v", bad, err)
		}
		if _, err := r.Delete(bg, r.Start(), []string{bad}); !errors.Is(err, ErrBadName) {
			t.Errorf("Delete(%q) = %v", bad, err)
		}
	}
}

func FuzzSEC_SFTP_001_NameStaysInDir(f *testing.F) {
	for _, n := range sshtest.HostileNames {
		f.Add(n)
	}
	f.Add("ok")
	f.Add("\xff\xfe")
	f.Fuzz(func(t *testing.T, name string) {
		if CheckName(name) != nil {
			return
		}
		dir := filepath.Join("/srv", "target")
		if got := filepath.Dir(filepath.Join(dir, name)); got != dir {
			t.Fatalf("%q joined leaves the directory: %q", name, got)
		}
		if strings.ContainsFunc(name, unicode.IsControl) {
			t.Fatalf("%q holds a control rune", name)
		}
		if got := path.Dir(path.Join("/home/pi", name)); got != "/home/pi" {
			t.Fatalf("%q joined leaves the remote directory: %q", name, got)
		}
	})
}

func TestSEC_SFTP_004_ListingCap(t *testing.T) {
	r := openRemote(t, sftpSrv(t, sshtest.WithSFTPHostile(sshtest.SFTPHostile{Entries: 150_000})))
	l, err := r.List(bg, sshtest.ManyDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != MaxEntries || !l.Truncated {
		t.Fatalf("%d entries, truncated %v; want %d and true", len(l.Entries), l.Truncated, MaxEntries)
	}
}

func TestSEC_SFTP_004_DepthCap(t *testing.T) {
	srv := sftpSrv(t, sshtest.WithSFTPHostile(sshtest.SFTPHostile{Depth: 70}))
	r := openRemote(t, srv)
	n, err := r.Count(bg, sshtest.DeepDir, 1_000, true)
	if err != nil || n != MaxDepth {
		t.Fatalf("Count = %d, %v; want %d (one directory per level, %d levels)", n, err, MaxDepth, MaxDepth)
	}
	// Delete refuses to descend past MaxDepth on a real 70-level chain.
	p := srv.SFTPDir("chain")
	for range 70 {
		p = filepath.Join(p, "d")
	}
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Delete(bg, r.Start(), []string{"chain"}); !errors.Is(err, ErrTooDeep) {
		t.Fatalf("Delete = %v, want ErrTooDeep", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the deepest level was removed: %v", err)
	}
}

func TestSEC_SFTP_008_StatusMessageSanitised(t *testing.T) {
	r := openRemote(t, sftpSrv(t, sshtest.WithSFTPHostile(sshtest.SFTPHostile{Status: sshtest.HostileStatus})))
	_, err := r.List(bg, sshtest.DeniedFile)
	if err == nil {
		t.Fatal("listing the denied file succeeded")
	}
	msg := err.Error()
	if strings.ContainsFunc(msg, unicode.IsControl) || strings.Contains(msg, "\x1b") {
		t.Fatalf("control rune in %q", msg)
	}
	if w := ansi.StringWidth(msg); w > MaxReasonWidth || !strings.HasSuffix(msg, "…") {
		t.Fatalf("message %q is %d columns, want a cut at %d", msg, w, MaxReasonWidth)
	}
	if !strings.Contains(msg, "permission denied second line") {
		t.Fatalf("message %q lost its text or newline folding", msg)
	}
	var se *ServerError
	if !errors.As(err, &se) || se.Unwrap() == nil {
		t.Fatalf("err %T is not a *ServerError", err)
	}
	// Library-mapped codes keep working with errors.Is.
	if _, err := r.Stat(bg, "/nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Stat(/nope) = %v, want ErrNotExist", err)
	}
}

func TestSEC_SFTP_008_RealpathSanitised(t *testing.T) {
	h := sshtest.FullHostile()
	r := openRemote(t, sftpSrv(t, sshtest.WithSFTPHostile(sshtest.SFTPHostile{Realpath: h.Realpath})))
	if r.Start() != h.Realpath {
		t.Fatalf("Start() = %q, want the exact bytes %q", r.Start(), h.Realpath)
	}
	l, _ := r.List(bg, r.Start())
	if strings.ContainsFunc(l.Title, unicode.IsControl) || !strings.HasPrefix(l.Title, "/home/pi/") || l.Dir != h.Realpath {
		t.Fatalf("Title %q, Dir %q", l.Title, l.Dir)
	}
}

func TestFR_SFTP_005_RemoteStartsAtRealpath(t *testing.T) {
	r := openRemote(t, sftpSrv(t))
	if r.Start() != sshtest.SFTPHome {
		t.Fatalf("Start() = %q, want %q", r.Start(), sshtest.SFTPHome)
	}
	l, err := r.List(bg, r.Start())
	if err != nil || l.Title != "/home/pi" || l.Side != Remote {
		t.Fatalf("List = %+v, %v", l, err)
	}
}

func TestFR_SFTP_010_RenameRefusesExisting(t *testing.T) {
	srv := sftpSrv(t)
	writeRemote(t, srv, "a.txt", "a")
	writeRemote(t, srv, "b.txt", "b")
	r := openRemote(t, srv)
	if err := r.Rename(bg, r.Start(), "a.txt", "b.txt"); !errors.Is(err, ErrExists) {
		t.Fatalf("Rename onto b.txt = %v, want ErrExists", err)
	}
	if b, _ := os.ReadFile(srv.SFTPDir("b.txt")); string(b) != "b" {
		t.Fatal("b.txt was overwritten")
	}
	if err := r.Rename(bg, r.Start(), "a.txt", "c.txt"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(srv.SFTPDir("c.txt")); string(b) != "a" {
		t.Fatal("rename did not happen")
	}
	if err := r.Rename(bg, r.Start(), "missing", "d.txt"); err == nil {
		t.Fatal("renaming a missing entry succeeded")
	}
}

func TestFR_SFTP_011_DeleteDoesNotFollowLinks(t *testing.T) {
	srv := sftpSrv(t)
	writeRemote(t, srv, "/keep/precious.txt", "precious")
	writeRemote(t, srv, "tree/sub/f.txt", "f")
	writeRemote(t, srv, "file.txt", "x")
	// Relative targets: sshtest's os.Root treats an absolute target as
	// leaving the served directory.
	if err := os.Symlink("../../../keep", srv.SFTPDir("tree/link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../keep/precious.txt", srv.SFTPDir("flink")); err != nil {
		t.Fatal(err)
	}
	r := openRemote(t, srv)
	n, err := r.Count(bg, path.Join(r.Start(), "tree"), 100, true)
	if err != nil || n != 3 { // sub, sub/f.txt, link (not entered)
		t.Fatalf("Count = %d, %v; want 3", n, err)
	}
	if e, err := r.Stat(bg, path.Join(r.Start(), "flink")); err != nil || e.Kind != File || e.Size != 8 {
		t.Fatalf("Stat follows the link: %+v, %v", e, err)
	}
	n, err = r.Delete(bg, r.Start(), []string{"tree", "flink", "file.txt"})
	if err != nil || n != 3 {
		t.Fatalf("Delete = %d, %v", n, err)
	}
	if b, _ := os.ReadFile(srv.SFTPDir("/keep/precious.txt")); string(b) != "precious" {
		t.Fatal("the link's target was touched")
	}
	if left, _ := os.ReadDir(srv.SFTPDir("")); len(left) != 0 {
		t.Fatalf("left: %v", left)
	}
	// Stops at the first error with the count so far.
	writeRemote(t, srv, "a", "a")
	n, err = r.Delete(bg, r.Start(), []string{"a", "missing", "b"})
	if n != 1 || err == nil {
		t.Fatalf("Delete = %d, %v; want 1 and an error", n, err)
	}
}

func TestFR_SFTP_011_CountStopsAt100k(t *testing.T) {
	r := openRemote(t, sftpSrv(t, sshtest.WithSFTPHostile(sshtest.SFTPHostile{Entries: 150_000})))
	n, err := r.Count(bg, sshtest.ManyDir, MaxEntries+1, true)
	if err != nil || n != MaxEntries+1 {
		t.Fatalf("Count = %d, %v; want %d", n, err, MaxEntries+1)
	}
}

func TestFR_SFTP_012_MkdirExisting(t *testing.T) {
	srv := sftpSrv(t)
	writeRemote(t, srv, "x/f", "f")
	r := openRemote(t, srv)
	if err := r.Mkdir(bg, r.Start(), "x"); !errors.Is(err, ErrExists) {
		t.Fatalf("Mkdir(x) = %v, want ErrExists", err)
	}
	if err := r.Mkdir(bg, r.Start(), "new"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(srv.SFTPDir("new")); err != nil || !fi.IsDir() {
		t.Fatalf("new: %v, %v", fi, err)
	}
	if err := r.Mkdir(bg, "/nonexistent", "y"); err == nil {
		t.Fatal("mkdir in a missing directory succeeded")
	}
}
