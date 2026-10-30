package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/files"
	"github.com/RafaelXokito/sshelf/internal/sshclient"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// reconnectable stands in for the app.Browser that T-054 makes of a
// *files.Browser: only what ReconnectSFTP uses.
type reconnectable struct {
	Browser
	fb *files.Browser
}

func (r reconnectable) HostID() domain.ID                   { return r.fb.HostID() }
func (r reconnectable) Attach(lease *sshclient.Lease) error { return r.fb.Attach(lease) }

// waitTransfers waits until cond holds for fb's transfers.
func waitTransfers(t *testing.T, fb *files.Browser, cond func([]files.Transfer) bool) []files.Transfer {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		ts := fb.Transfers()
		if cond(ts) {
			return ts
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out: %+v", ts)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestFR_SFTP_004_DropPausesSftp: when the connection a session and an
// SFTP tab share drops, the session ends and the browser pauses its
// transfer (FR-SFTP-004, FR-SFTP-018); ReconnectSFTP gives the browser a
// connection of its own through the normal connect flow and the transfer
// resumes (FR-SFTP-017).
func TestFR_SFTP_004_DropPausesSftp(t *testing.T) {
	const size = 64 << 20
	a, srv := sliceFixture(t, true, sshtest.WithSFTP(t.TempDir()),
		sshtest.WithSFTPZeroFile("zero.bin", size), sshtest.WithSFTPFault("zero.bin", 16<<20, sshtest.FaultDrop))
	if _, err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := a.Connect(context.Background(), "h1", Size{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	lease, err := s.(connHolder).Conn().Lease()
	if err != nil {
		t.Fatal(err)
	}
	fb, err := files.Open(lease, files.Options{HostID: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fb.Close() }()
	attempts := len(srv.AuthAttempts())

	down := t.TempDir()
	fb.Copy(files.Remote, sshtest.SFTPHome, []string{"zero.bin"}, down)
	for _, done := range []<-chan struct{}{s.Done(), fb.Done()} {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("the drop did not end the session and the browser")
		}
	}
	if fb.State() != files.Disconnected {
		t.Fatal("browser still connected")
	}
	waitTransfers(t, fb, func(ts []files.Transfer) bool { return ts[0].State == files.Paused })
	read := srv.SFTPBytesRead("zero.bin")

	if err := a.ReconnectSFTP(context.Background(), reconnectable{fb: fb}); err != nil {
		t.Fatal(err)
	}
	if fb.State() != files.Connected || fb.ConnID() == s.ConnID() {
		t.Fatalf("after the reconnect: state %v, connection %d (session's %d)", fb.State(), fb.ConnID(), s.ConnID())
	}
	if len(srv.AuthAttempts()) == attempts {
		t.Fatal("the reconnect did not authenticate")
	}
	ts := waitTransfers(t, fb, func(ts []files.Transfer) bool { return ts[0].State.Finished() })
	if ts[0].State != files.Done {
		t.Fatalf("transfer %v: %s", ts[0].State, ts[0].Reason)
	}
	if fi, err := os.Stat(filepath.Join(down, "zero.bin")); err != nil || fi.Size() != size {
		t.Fatalf("downloaded: %v, %v", fi, err)
	}
	if again := srv.SFTPBytesRead("zero.bin") - read; again >= size-(8<<20) {
		t.Fatalf("read %d bytes after the reconnect: not resumed", again)
	}
}

func TestFR_SFTP_018_ReconnectSFTPErrors(t *testing.T) {
	a, _ := sliceFixture(t, true, sshtest.WithSFTP(t.TempDir()))
	if err := a.ReconnectSFTP(context.Background(), nil); !errors.Is(err, ErrCannotReconnect) {
		t.Fatalf("ReconnectSFTP(fake) = %v", err)
	}
	if _, err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := a.Connect(context.Background(), "h1", Size{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	lease, err := s.(connHolder).Conn().Lease()
	if err != nil {
		t.Fatal(err)
	}
	fb, err := files.Open(lease, files.Options{HostID: "h2"}) // h2 refuses connections
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fb.Close() }()
	if err := a.ReconnectSFTP(context.Background(), reconnectable{fb: fb}); err == nil {
		t.Fatal("reconnect to an unreachable host succeeded")
	}
	if fb.State() != files.Connected { // a failed reconnect changes nothing
		t.Fatal("state changed")
	}
}
