package sshtest

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// The sftp subsystem (ARCH-TEST-004), driven by a plain pkg/sftp client.

func sftpServer(t *testing.T, opts ...Option) *Server {
	t.Helper()
	return New(t, append([]Option{WithPassword("u", "pw")}, opts...)...)
}

func sshDial(t *testing.T, srv *Server) *ssh.Client {
	t.Helper()
	c, err := ssh.Dial("tcp", srv.Addr(), &ssh.ClientConfig{
		User:            "u",
		Auth:            []ssh.AuthMethod{ssh.Password("pw")},
		HostKeyCallback: ssh.FixedHostKey(srv.HostKeys()[0]),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func sftpClient(t *testing.T, srv *Server) *sftp.Client {
	t.Helper()
	sc, err := sftp.NewClient(sshDial(t, srv))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sc.Close() })
	return sc
}

func TestARCH_TEST_004_ServesDir(t *testing.T) {
	dir := t.TempDir()
	srv := sftpServer(t, WithSFTP(dir))
	if err := os.WriteFile(srv.SFTPDir("hello.txt"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := sftpClient(t, srv)
	if subs := srv.RequestsOfType("subsystem"); len(subs) != 1 || subs[0].Name != "sftp" {
		t.Fatalf("subsystem requests = %+v", subs)
	}
	wd, err := c.Getwd()
	if err != nil || wd != SFTPHome {
		t.Fatalf("Getwd = %q, %v", wd, err)
	}
	if rp, err := c.RealPath("."); err != nil || rp != SFTPHome {
		t.Fatalf("RealPath(.) = %q, %v", rp, err)
	}
	// list, read
	ents, err := c.ReadDir(SFTPHome)
	if err != nil || len(ents) != 1 || ents[0].Name() != "hello.txt" {
		t.Fatalf("ReadDir = %v, %v", ents, err)
	}
	f, err := c.Open("hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	_ = f.Close()
	if string(b) != "hi\n" {
		t.Fatalf("read %q", b)
	}
	// write, mkdir, rename, posix-rename, setstat, symlink, remove, rmdir
	w, err := c.OpenFile("new.txt", os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	if err := c.Mkdir("sub"); err != nil {
		t.Fatal(err)
	}
	if err := c.Rename("new.txt", "hello.txt"); err == nil {
		t.Fatal("SFTP v3 rename over an existing file succeeded")
	}
	if err := c.Rename("new.txt", "sub/moved.txt"); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.HasExtension("posix-rename@openssh.com"); !ok {
		t.Fatal("posix-rename not offered")
	}
	if err := c.PosixRename("sub/moved.txt", "hello.txt"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(srv.SFTPDir("hello.txt")); string(b) != "data" {
		t.Fatalf("after posix-rename hello.txt = %q", b)
	}
	mt := time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC)
	if err := c.Chmod("hello.txt", 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.Truncate("hello.txt", 2); err != nil {
		t.Fatal(err)
	}
	if err := c.Chtimes("hello.txt", mt, mt); err != nil {
		t.Fatal(err)
	}
	fi, err := c.Stat("hello.txt")
	if err != nil || fi.Mode().Perm() != 0o600 || !fi.ModTime().Equal(mt) || fi.Size() != 2 {
		t.Fatalf("Stat = %v %v %v %v", fi.Mode(), fi.ModTime(), fi.Size(), err)
	}
	if err := c.Symlink("hello.txt", "link"); err != nil {
		t.Fatal(err)
	}
	if fi, err := c.Lstat("link"); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("Lstat(link) = %v, %v", fi, err)
	}
	if tgt, err := c.ReadLink("link"); err != nil || tgt != "hello.txt" {
		t.Fatalf("ReadLink = %q, %v", tgt, err)
	}
	for _, p := range []string{"link", "hello.txt"} {
		if err := c.Remove(p); err != nil {
			t.Fatalf("Remove(%s): %v", p, err)
		}
	}
	if err := c.RemoveDirectory("sub"); err != nil {
		t.Fatal(err)
	}
	if left, _ := os.ReadDir(srv.SFTPDir("")); len(left) != 0 {
		t.Fatalf("left in home: %v", left)
	}
}

func TestARCH_TEST_004_ConfinedToRoot(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := sftpServer(t, WithSFTP(t.TempDir()))
	if err := os.Symlink("/", srv.SFTPDir("root")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, srv.SFTPDir("out")); err != nil {
		t.Fatal(err)
	}
	c := sftpClient(t, srv)
	rel, _ := filepath.Rel("/", secret)
	for _, p := range []string{
		secret,
		"../../../../" + rel,
		"/../../" + rel,
		"root/" + rel,
		"out/secret.txt",
	} {
		if _, err := c.Stat(p); err == nil {
			t.Errorf("Stat(%q) succeeded", p)
		}
		if _, err := c.Lstat(p); err == nil {
			t.Errorf("Lstat(%q) succeeded", p)
		}
		if f, err := c.Open(p); err == nil {
			_ = f.Close()
			t.Errorf("Open(%q) succeeded", p)
		}
		if f, err := c.Create(p); err == nil {
			_ = f.Close()
			t.Errorf("Create(%q) succeeded", p)
		}
	}
	for _, d := range []string{"root", "out", "root/etc"} {
		if _, err := c.ReadDir(d); err == nil {
			t.Errorf("ReadDir(%q) succeeded", d)
		}
	}
	if err := c.Remove("out/secret.txt"); err == nil {
		t.Error("Remove through a link succeeded")
	}
	if b, _ := os.ReadFile(secret); string(b) != "secret" { //nolint:gosec // the test's own temp file
		t.Fatal("the outside file changed")
	}
	// The links themselves are visible inside the dir.
	if fi, err := c.Lstat("root"); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("Lstat(root) = %v, %v", fi, err)
	}
}

func TestARCH_TEST_004_HostileListing(t *testing.T) {
	srv := sftpServer(t, WithSFTP(t.TempDir()), WithSFTPHostile(FullHostile()))
	c := sftpClient(t, srv)

	ents, err := c.ReadDir(HostileDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	// pkg/sftp drops "." and ".." and reduces names with path.Base
	// (ADR-016 limitation 2), so the rest arrive as the library makes them.
	for _, want := range []string{"ok.txt", "evil", "b", "..", ".", "x\x00y", "bell\x07", "esc\x1b[31m", "c1\u009b", "del\x7f", "tab\tname"} {
		if !slices.Contains(names, want) {
			t.Errorf("hostile listing lacks %q: %q", want, names)
		}
	}
	if len(names) != len(HostileNames)-2 {
		t.Errorf("%d names, want %d", len(names), len(HostileNames)-2)
	}
	f, err := c.Open(HostileDir + "/evil")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	_ = f.Close()
	if string(b) != HostileContent {
		t.Fatalf("hostile file = %q", b)
	}

	many, err := c.ReadDir(ManyDir)
	if err != nil || len(many) != 150_000 {
		t.Fatalf("many: %d entries, %v", len(many), err)
	}

	deep := DeepDir
	for range 70 {
		ents, err := c.ReadDir(deep)
		if err != nil || len(ents) != 1 || ents[0].Name() != "d" || !ents[0].IsDir() {
			t.Fatalf("ReadDir(%s) = %v, %v", deep, ents, err)
		}
		deep += "/d"
	}
	if ents, err := c.ReadDir(deep); err != nil || len(ents) != 1 || ents[0].Name() != "bottom.txt" {
		t.Fatalf("bottom: %v, %v", ents, err)
	}
	if _, err := c.ReadDir(deep + "/d"); err == nil {
		t.Fatal("chain goes past its depth")
	}

	// The oversize file returns data past its reported size.
	of, err := c.Open(OversizeFile)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := of.Stat()
	got, _ := io.Copy(io.Discard, io.LimitReader(of, st.Size()*2))
	_ = of.Close()
	if st.Size() != 1<<20 || got != 2<<20 {
		t.Fatalf("oversize: size %d, read %d", st.Size(), got)
	}

	// Status messages carry escapes. pkg/sftp quotes the message in
	// StatusError.Error, so they arrive as Go escape text.
	_, err = c.Open(DeniedFile)
	if err == nil || !strings.Contains(err.Error(), `\x1b]52;`) || !strings.Contains(err.Error(), `\n`) {
		t.Fatalf("Open(denied) = %v", err)
	}
	if _, err := c.ReadDir(DeniedFile); err == nil || !strings.Contains(err.Error(), `\x1b]0;`) {
		t.Fatalf("ReadDir(denied) = %v", err)
	}
	if rp, err := c.RealPath("."); err != nil || !strings.Contains(rp, "\x1b") {
		t.Fatalf("RealPath(.) = %q, %v", rp, err)
	}

	// The hostile entries are listed in the home directory.
	home, err := c.ReadDir(SFTPHome)
	if err != nil || len(home) != 5 {
		t.Fatalf("home = %v, %v", home, err)
	}
}

func TestARCH_TEST_004_FaultAfterBytes(t *testing.T) {
	const size = 4 << 20
	data := bytes.Repeat([]byte("0123456789abcdef"), size/16)
	t.Run("fail request", func(t *testing.T) {
		srv := sftpServer(t, WithSFTP(t.TempDir()), WithSFTPFault("big.bin", 1<<20, FaultFail))
		if err := os.WriteFile(srv.SFTPDir("big.bin"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		c := sftpClient(t, srv)
		read := func() error {
			f, err := c.Open("big.bin")
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			_, err = f.WriteTo(io.Discard)
			return err
		}
		if err := read(); err == nil {
			t.Fatal("the first read did not fail")
		}
		if n := srv.SFTPBytesRead("big.bin"); n < 1<<20 || n >= size {
			t.Fatalf("read %d bytes before the fault", n)
		}
		if err := read(); err != nil { // fires once
			t.Fatalf("second read: %v", err)
		}
	})
	t.Run("drop connection on upload", func(t *testing.T) {
		srv := sftpServer(t, WithSFTP(t.TempDir()), WithSFTPFault(SFTPHome+"/up.bin", 1<<20, FaultDrop))
		c := sftpClient(t, srv)
		f, err := c.Create("up.bin")
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.ReadFrom(bytes.NewReader(data))
		_ = f.Close()
		if err == nil {
			t.Fatal("upload survived the drop")
		}
		if !srv.WaitFor(2*time.Second, func() bool {
			for _, c := range srv.Closes() {
				if c.Type == "disconnect" {
					return true
				}
			}
			return false
		}) {
			t.Fatal("connection not dropped")
		}
		if n := srv.SFTPBytesWritten("up.bin"); n < 1<<20 || n >= size {
			t.Fatalf("wrote %d bytes before the drop", n)
		}
	})
}

func TestARCH_TEST_004_WithoutSFTP(t *testing.T) {
	for name, opts := range map[string][]Option{
		"no WithSFTP":       nil,
		"WithoutSFTP":       {WithSFTP(t.TempDir()), WithoutSFTP()},
		"WithoutSFTP first": {WithoutSFTP(), WithSFTP(t.TempDir())},
	} {
		t.Run(name, func(t *testing.T) {
			srv := sftpServer(t, opts...)
			if _, err := sftp.NewClient(sshDial(t, srv)); err == nil {
				t.Fatal("the subsystem was served")
			}
			if subs := srv.RequestsOfType("subsystem"); len(subs) != 1 {
				t.Fatalf("subsystem requests = %+v", subs)
			}
		})
	}
}

func TestARCH_TEST_004_MaxSessions(t *testing.T) {
	srv := sftpServer(t, WithSFTP(t.TempDir()), WithMaxSessions(1))
	client := sshDial(t, srv)
	first, err := sftp.NewClient(client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sftp.NewClient(client); err == nil {
		t.Fatal("a second channel was accepted")
	}
	if len(srv.RequestsOfType("session-refused")) != 1 {
		t.Fatalf("requests = %+v", srv.Requests())
	}
	_ = first.Close()
	if !srv.WaitFor(time.Second, func() bool { return len(srv.Closes()) == 1 }) {
		t.Fatal("first channel close not seen")
	}
	second, err := sftp.NewClient(client)
	if err != nil {
		t.Fatalf("a channel after the first closed: %v", err)
	}
	_ = second.Close()
}

func TestARCH_TEST_004_ZeroFile(t *testing.T) {
	const size = 64 << 20
	srv := sftpServer(t, WithSFTP(t.TempDir()), WithSFTPZeroFile("zero.bin", size))
	c := sftpClient(t, srv)
	ents, err := c.ReadDir(".")
	if err != nil || len(ents) != 1 || ents[0].Name() != "zero.bin" || ents[0].Size() != size {
		t.Fatalf("ReadDir = %v, %v", ents, err)
	}
	f, err := c.Open("zero.bin")
	if err != nil {
		t.Fatal(err)
	}
	n, err := f.WriteTo(io.Discard)
	_ = f.Close()
	if err != nil || n != size {
		t.Fatalf("read %d, %v", n, err)
	}
	if _, err := os.Stat(srv.SFTPDir("zero.bin")); err == nil {
		t.Fatal("zero file is on disk")
	}
	if _, err := c.Create("zero.bin"); err == nil {
		t.Fatal("zero file is writable")
	}
}

func TestARCH_TEST_004_ByteCounters(t *testing.T) {
	srv := sftpServer(t, WithSFTP(t.TempDir()))
	c := sftpClient(t, srv)
	data := bytes.Repeat([]byte("x"), 300_000)
	f, err := c.Create("a.bin")
	if err != nil {
		t.Fatal(err)
	}
	if srv.SFTPOpenHandles() != 1 {
		t.Fatalf("open handles = %d, want 1", srv.SFTPOpenHandles())
	}
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if !srv.WaitFor(time.Second, func() bool { return srv.SFTPOpenHandles() == 0 }) {
		t.Fatalf("open handles = %d after close", srv.SFTPOpenHandles())
	}
	for range 2 {
		r, err := c.Open(SFTPHome + "/a.bin")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = r.WriteTo(io.Discard)
		_ = r.Close()
	}
	if w, r := srv.SFTPBytesWritten("a.bin"), srv.SFTPBytesRead("a.bin"); w != 300_000 || r != 600_000 {
		t.Fatalf("written %d, read %d", w, r)
	}
	if srv.SFTPMaxOpenWriters() != 1 {
		t.Fatalf("max writers = %d", srv.SFTPMaxOpenWriters())
	}
	if srv.SFTPBytesRead("other") != 0 {
		t.Fatal("counter for an unused file")
	}
}

// TestARCH_TEST_004_RealpathFollowsLinks: realpath resolves links in the
// path as OpenSSH's does (edit-locally, FR-SFTP-019), keeps names that do
// not exist, and stops a loop.
func TestARCH_TEST_004_RealpathFollowsLinks(t *testing.T) {
	srv := sftpServer(t, WithSFTP(t.TempDir()))
	if err := os.MkdirAll(srv.SFTPDir("d"), 0o700); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{"rel": "d/f.txt", "abs": SFTPHome + "/d", "up": "../pi/rel", "loop": "loop"} {
		if err := os.Symlink(target, srv.SFTPDir(link)); err != nil {
			t.Fatal(err)
		}
	}
	c := sftpClient(t, srv)
	for in, want := range map[string]string{
		"rel":         SFTPHome + "/d/f.txt",
		"abs/x.txt":   SFTPHome + "/d/x.txt",
		"up":          SFTPHome + "/d/f.txt",
		"missing/a":   SFTPHome + "/missing/a",
		"/home/pi/up": SFTPHome + "/d/f.txt",
	} {
		if got, err := c.RealPath(in); err != nil || got != want {
			t.Errorf("RealPath(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
	if got, err := c.RealPath("loop"); err == nil {
		t.Errorf("RealPath(loop) = %q, want an error", got)
	}
}
