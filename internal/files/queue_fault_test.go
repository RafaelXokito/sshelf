//go:build sshelf_faultinject

package files

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// A panic in a transfer fails that transfer with "internal error"; the
// browser, its listings and the next transfers keep working
// (ARCH-SFTP-003, ARCH-SEC-004).
func TestARCH_SFTP_003_PanicFailsOnlyJob(t *testing.T) {
	srv := sftpSrv(t)
	b := openBrowser(t, srv)
	up := t.TempDir()
	writeFile(t, filepath.Join(up, "a"), "a")
	writeFile(t, filepath.Join(up, "b"), "b")
	t.Setenv("SSHELF_FAULT", "sftp-transfer")
	b.Copy(Local, up, []string{"a"}, sshtest.SFTPHome)
	ts := waitFor(t, b, 10*time.Second, settled)
	if ts[0].State != Failed || ts[0].Reason != ErrInternal.Error() {
		t.Fatalf("transfers:\n%s", dump(ts))
	}
	if err := os.Unsetenv("SSHELF_FAULT"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.List(bg, Remote, sshtest.SFTPHome); err != nil {
		t.Fatalf("listing after the panic: %v", err)
	}
	b.Copy(Local, up, []string{"b"}, sshtest.SFTPHome)
	waitFor(t, b, 10*time.Second, func(ts []Transfer) bool { return ts[1].State == Done })
	b.Retry(ts[0].ID)
	waitSettled(t, b)
	if readFile(t, srv.SFTPDir("a")) != "a" || readFile(t, srv.SFTPDir("b")) != "b" {
		t.Fatal("transfers after the panic did not copy")
	}
}
