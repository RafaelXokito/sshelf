package sshclient

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// corruptLine reproduces a real ~/.ssh/known_hosts whose newline went
// missing: an ECDSA entry runs straight into the next host's entry, so
// its base64 key ends in a hostname and does not decode.
func corruptLine(t *testing.T) string {
	t.Helper()
	ecdsa := strings.Fields(knownhosts.Line([]string{"172.22.21.132"}, otherKey(t, sshtest.ECDSA)))
	ed := keyField(otherKey(t, sshtest.Ed25519))
	return ecdsa[0] + " " + ecdsa[1] + " " + ecdsa[2] + "raspberrypi.local " + ed
}

// TestFR_KH_003_SkipsUnparseableLines checks that, like OpenSSH, one
// unparseable line does not make every host unreachable: the other lines
// still trust, block and report their original file and line.
func TestFR_KH_003_SkipsUnparseableLines(t *testing.T) {
	t.Run("trusted entry after a corrupt line connects", func(t *testing.T) {
		s := newTrustSetup(t)
		s.writeOpenSSH(t, corruptLine(t), s.srv.KnownHostsLine())
		p := &recordingPrompter{}
		if err := s.connect(t, p); err != nil {
			t.Fatal(err)
		}
		if len(p.prompts) != 0 {
			t.Fatalf("prompted: %+v", p.prompts)
		}
	})

	t.Run("changed key still blocks with original path and line", func(t *testing.T) {
		s := newTrustSetup(t)
		s.writeOpenSSH(t, "# hosts", corruptLine(t), s.line(otherKey(t, sshtest.Ed25519))) // line 3
		err := s.connect(t, &recordingPrompter{trust: true})
		me, ok := errors.AsType[*HostKeyMismatchError](err)
		if !ok || len(me.Stale) != 1 || me.Stale[0].File != s.openssh || me.Stale[0].Line != 3 {
			t.Fatalf("err = %#v", err)
		}
	})

	t.Run("host whose only entry is corrupt is unknown", func(t *testing.T) {
		s := newTrustSetup(t)
		write(t, s.kh, corruptLine(t))
		p := &recordingPrompter{trust: false}
		if err := s.connect(t, p); !errors.Is(err, ErrHostKeyRejected) {
			t.Fatalf("err = %v, want ErrHostKeyRejected", err)
		}
		if len(p.prompts) != 1 {
			t.Fatalf("prompts = %d, want 1", len(p.prompts))
		}
	})

	t.Run("no temporary copies are left behind", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("TMPDIR", tmp)
		s := newTrustSetup(t)
		s.writeOpenSSH(t, corruptLine(t), s.srv.KnownHostsLine())
		if err := s.connect(t, &recordingPrompter{}); err != nil {
			t.Fatal(err)
		}
		left, _ := filepath.Glob(filepath.Join(tmp, "sshelf-known_hosts-*"))
		if len(left) != 0 {
			t.Fatalf("left behind: %v", left)
		}
	})

	t.Run("a file with too many bad lines fails", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "known_hosts")
		bad := make([]string, maxSkippedLines+1)
		for i := range bad {
			bad[i] = corruptLine(t)
		}
		if err := os.WriteFile(path, []byte(strings.Join(bad, "\n")), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadKnownHosts([]string{path}); err == nil {
			t.Fatal("loaded a file that is all garbage")
		}
	})
}
