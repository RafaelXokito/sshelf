package sshtest

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// execPTY runs command with a PTY on a new connection and returns the
// session, its stdin and stdout.
func execPTY(t *testing.T, s *Server, command string) (*ssh.Session, io.WriteCloser, io.Reader) {
	t.Helper()
	c, err := dial(t, s, "u", ssh.Password("pw"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	sess, err := c.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.RequestPty("xterm-256color", 24, 80, ssh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	in, err := sess.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := sess.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Start(command); err != nil {
		t.Fatal(err)
	}
	return sess, in, out
}

func runExec(t *testing.T, s *Server, command string) []byte {
	t.Helper()
	sess, _, out := execPTY(t, s, command)
	b, err := io.ReadAll(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Wait(); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestARCH_TEST_003_Commands(t *testing.T) {
	t.Run("flood", func(t *testing.T) {
		s := New(t, WithPassword("u", "pw"))
		out := runExec(t, s, "flood 10M")
		if len(out) != 10<<20 || !bytes.HasSuffix(out, []byte(FloodEnd)) {
			t.Fatalf("flood 10M sent %d bytes ending %q, want %d ending with FloodEnd", len(out), out[max(0, len(out)-20):], 10<<20)
		}
		if !bytes.Contains(out[:300], []byte("\x1b[32m00000001 the quick")) {
			t.Fatalf("flood lines %q", out[:300])
		}
	})
	t.Run("stream", func(t *testing.T) {
		s := New(t, WithPassword("u", "pw"))
		sess, in, out := execPTY(t, s, "stream 100K")
		buf := make([]byte, 64<<10)
		var n int
		start := time.Now()
		for time.Since(start) < time.Second {
			k, err := out.Read(buf)
			if err != nil {
				t.Fatal(err)
			}
			n += k
		}
		rate := float64(n) / time.Since(start).Seconds()
		_, _ = in.Write([]byte("q"))
		_, _ = io.Copy(io.Discard, out)
		if err := sess.Wait(); err != nil {
			t.Fatal(err)
		}
		if rate < 0.9*100<<10 || rate > 1.1*100<<10 {
			t.Fatalf("stream 100K ran at %.0f B/s, want 102400 ± 10%%", rate)
		}
	})
	t.Run("replay", func(t *testing.T) {
		s := New(t, WithPassword("u", "pw"))
		path := filepath.Join(t.TempDir(), "fixture.bin")
		data := []byte("\x1b[?1049h\x1b[1;1Hrecorded\xe6\x97\xa5")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if out := runExec(t, s, "replay "+path); !bytes.Equal(out, data) {
			t.Fatalf("replay sent %q", out)
		}
	})
	t.Run("modes", func(t *testing.T) {
		s := New(t, WithPassword("u", "pw"))
		if out := string(runExec(t, s, "modes 1000,1006")); out != "\x1b[?1000h\x1b[?1006h" {
			t.Fatalf("modes sent %q", out)
		}
	})
	t.Run("query", func(t *testing.T) {
		s := New(t, WithPassword("u", "pw"))
		sess, in, out := execPTY(t, s, "query")
		got := make([]byte, len(QuerySequences))
		if _, err := io.ReadFull(out, got); err != nil || string(got) != QuerySequences {
			t.Fatalf("query sent %q, %v", got, err)
		}
		replies := "\x1b[?62;1;6;22c\x1b[1;1R\x1b]11;rgb:0000/0000/0000\x1b\\"
		_, _ = in.Write([]byte(replies))
		_, _ = io.Copy(io.Discard, out)
		if err := sess.Wait(); err != nil {
			t.Fatal(err)
		}
		if rec := string(s.Received(1, 1)); rec != replies {
			t.Fatalf("recorded %q, want %q", rec, replies)
		}
	})
	t.Run("hostile", func(t *testing.T) {
		s := New(t, WithPassword("u", "pw"))
		if out := string(runExec(t, s, "hostile")); out != strings.Join(HostileCorpus, "") {
			t.Fatalf("hostile sent %q", out)
		}
	})
	t.Run("echo-hex", func(t *testing.T) {
		s := New(t, WithPassword("u", "pw"))
		sess, in, out := execPTY(t, s, "echo-hex")
		_, _ = in.Write([]byte("\x1b[1;5A\x00"))
		got := make([]byte, len("1b 5b 31 3b 35 41 00 "))
		if _, err := io.ReadFull(out, got); err != nil || string(got) != "1b 5b 31 3b 35 41 00 " {
			t.Fatalf("echo-hex sent %q, %v", got, err)
		}
		_ = sess.Close()
	})
	t.Run("closes recorded", func(t *testing.T) {
		s := New(t, WithPassword("u", "pw"))
		runExec(t, s, "modes 1")
		s.DropConnections()
		ok := s.WaitFor(time.Second, func() bool {
			c := s.Closes()
			return len(c) == 2 && c[0] == Request{Conn: 1, Channel: 1, Type: "close"} && c[1] == Request{Conn: 1, Type: "disconnect"}
		})
		if !ok {
			t.Fatalf("closes = %+v", s.Closes())
		}
	})
}
