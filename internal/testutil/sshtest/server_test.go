package sshtest

import (
	"bytes"
	"errors"
	"io"
	"net"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/RafaelXokito/sshelf/internal/testutil"
)

func TestMain(m *testing.M) { testutil.VerifyTestMain(m) }

func dial(t *testing.T, s *Server, user string, auth ...ssh.AuthMethod) (*ssh.Client, error) {
	t.Helper()
	c, err := ssh.Dial("tcp", s.Addr(), &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: ssh.FixedHostKey(s.HostKeys()[0]),
		Timeout:         5 * time.Second,
	})
	if err == nil {
		t.Cleanup(func() { _ = c.Close() })
	}
	return c, err
}

func methods(attempts []AuthAttempt) []string {
	var out []string
	for _, a := range attempts {
		out = append(out, a.Method)
	}
	return out
}

func TestARCH_TEST_001_AuthMethods(t *testing.T) {
	clientKey, err := GenerateSigner(Ed25519)
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := GenerateSigner(Ed25519)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("password", func(t *testing.T) {
		s := New(t, WithPassword("alice", "pw"))
		if _, err := dial(t, s, "alice", ssh.Password("wrong")); err == nil {
			t.Fatal("wrong password accepted")
		}
		if _, err := dial(t, s, "alice", ssh.Password("pw")); err != nil {
			t.Fatalf("right password: %v", err)
		}
		got := s.AuthAttempts()
		last := got[len(got)-1]
		if last.Method != "password" || !last.Success || last.User != "alice" || last.Conn != 2 {
			t.Fatalf("last attempt = %+v", last)
		}
	})

	t.Run("publickey", func(t *testing.T) {
		s := New(t, WithAuthorizedKey("bob", clientKey.PublicKey()))
		if _, err := dial(t, s, "bob", ssh.PublicKeys(otherKey)); err == nil {
			t.Fatal("unknown key accepted")
		}
		if _, err := dial(t, s, "bob", ssh.PublicKeys(clientKey)); err != nil {
			t.Fatalf("authorized key: %v", err)
		}
		if _, err := dial(t, s, "carol", ssh.PublicKeys(clientKey)); err == nil {
			t.Fatal("key accepted for another user")
		}
	})

	t.Run("any user", func(t *testing.T) {
		s := New(t, WithAuthorizedKey(AnyUser, clientKey.PublicKey()))
		if _, err := dial(t, s, "whoever", ssh.PublicKeys(clientKey)); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("keyboard-interactive", func(t *testing.T) {
		s := New(t, WithKeyboardInteractive("dave",
			Challenge{Questions: []Question{{Prompt: "Password: ", Echo: false, Answer: "pw"}}},
			Challenge{Instruction: "2FA", Questions: []Question{{Prompt: "OTP: ", Echo: true, Answer: "123456"}}},
		))
		var seen [][]bool
		answer := func(otp string) ssh.KeyboardInteractiveChallenge {
			return func(_, _ string, prompts []string, echos []bool) ([]string, error) {
				seen = append(seen, slices.Clone(echos))
				out := make([]string, len(prompts))
				for i, p := range prompts {
					if strings.HasPrefix(p, "OTP") {
						out[i] = otp
					} else {
						out[i] = "pw"
					}
				}
				return out, nil
			}
		}
		if _, err := dial(t, s, "dave", ssh.KeyboardInteractive(answer("000000"))); err == nil {
			t.Fatal("wrong OTP accepted")
		}
		seen = nil
		if _, err := dial(t, s, "dave", ssh.KeyboardInteractive(answer("123456"))); err != nil {
			t.Fatalf("right answers: %v", err)
		}
		if want := [][]bool{{false}, {true}}; !slices.EqualFunc(seen, want, slices.Equal) {
			t.Fatalf("echo flags = %v, want %v", seen, want)
		}
	})

	t.Run("attempts in order", func(t *testing.T) {
		s := New(t, WithPassword("erin", "pw"), WithAuthorizedKey("erin", clientKey.PublicKey()))
		if _, err := dial(t, s, "erin", ssh.PublicKeys(otherKey), ssh.Password("pw")); err != nil {
			t.Fatal(err)
		}
		got := methods(s.AuthAttempts())
		want := []string{"none", "publickey", "password"}
		if !slices.Equal(got, want) {
			t.Fatalf("attempts = %v, want %v", got, want)
		}
	})
}

func TestARCH_TEST_001_RecordsRequestsInOrder(t *testing.T) {
	s := New(t, WithPassword("u", "pw"))
	c, err := dial(t, s, "u", ssh.Password("pw"))
	if err != nil {
		t.Fatal(err)
	}
	if v := s.ClientVersions(); len(v) != 1 || !strings.HasPrefix(v[0], "SSH-2.0-") {
		t.Fatalf("client versions = %q", v)
	}
	sess, err := c.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	if err := sess.Setenv("LANG", "C"); err != nil {
		t.Fatal(err)
	}
	if err := sess.RequestPty("xterm-256color", 24, 80, ssh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	sess.Stdout = &out
	sess.Stdin = strings.NewReader("echo hello\rexit 0\r")
	if err := sess.Shell(); err != nil {
		t.Fatal(err)
	}
	if err := sess.Wait(); err != nil {
		t.Fatal(err)
	}

	got := s.Requests()
	var types []string
	for _, r := range got {
		types = append(types, r.Type)
	}
	if want := []string{"session", "env", "pty-req", "shell"}; !slices.Equal(types, want) {
		t.Fatalf("requests = %v, want %v", types, want)
	}
	if got[1].Name != "LANG" || got[1].Value != "C" {
		t.Errorf("env = %+v", got[1])
	}
	if got[2].Term != "xterm-256color" || got[2].Width != 80 || got[2].Height != 24 {
		t.Errorf("pty-req = %+v", got[2])
	}
	if !strings.Contains(out.String(), "hello\r\n") {
		t.Errorf("shell output %q does not contain the echo", out.String())
	}
}

func TestARCH_TEST_001_WindowChangeRecorded(t *testing.T) {
	s := New(t, WithPassword("u", "pw"))
	c, err := dial(t, s, "u", ssh.Password("pw"))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := c.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	if err := sess.RequestPty("xterm", 24, 80, ssh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Shell(); err != nil {
		t.Fatal(err)
	}
	if err := sess.WindowChange(40, 120); err != nil {
		t.Fatal(err)
	}
	ok := s.WaitFor(2*time.Second, func() bool {
		wc := s.RequestsOfType("window-change")
		return len(wc) == 1 && wc[0].Width == 120 && wc[0].Height == 40
	})
	if !ok {
		t.Fatalf("window-change not recorded: %+v", s.Requests())
	}
	if _, err := io.WriteString(stdin, "size\rexit 0\r"); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "120x40") {
		t.Fatalf("size output %q does not contain 120x40", out)
	}
	if err := sess.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestARCH_TEST_001_ExitStatus(t *testing.T) {
	s := New(t, WithPassword("u", "pw"))
	c, err := dial(t, s, "u", ssh.Password("pw"))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("exec", func(t *testing.T) {
		sess, err := c.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = sess.Close() }()
		var ee *ssh.ExitError
		if err := sess.Run("exit 3"); !errors.As(err, &ee) || ee.ExitStatus() != 3 {
			t.Fatalf("Run(exit 3) = %v, want exit status 3", err)
		}
		if r := s.RequestsOfType("exec"); len(r) != 1 || r[0].Command != "exit 3" {
			t.Fatalf("exec requests = %+v", r)
		}
	})

	t.Run("shell", func(t *testing.T) {
		sess, err := c.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = sess.Close() }()
		if err := sess.RequestPty("xterm", 24, 80, ssh.TerminalModes{}); err != nil {
			t.Fatal(err)
		}
		sess.Stdin = strings.NewReader("exit 42\r")
		sess.Stdout = io.Discard
		var ee *ssh.ExitError
		if err := sess.Run(""); err != nil && !errors.As(err, &ee) {
			t.Fatal(err)
		}
		// Run("") sends exec with an empty command, which the shell treats as a shell.
		if ee == nil || ee.ExitStatus() != 42 {
			t.Fatalf("exit status = %v, want 42", ee)
		}
	})
}

func TestARCH_TEST_001_DropConnections(t *testing.T) {
	s := New(t, WithPassword("u", "pw"))
	c, err := dial(t, s, "u", ssh.Password("pw"))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	s.DropConnections()
	select {
	case err := <-done:
		t.Logf("client.Wait after drop: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("client did not notice the dropped connection")
	}
}

func TestARCH_TEST_001_LoopbackOnly(t *testing.T) {
	s := New(t)
	ip := net.ParseIP(s.Host())
	if ip == nil || !ip.IsLoopback() || s.Host() != "127.0.0.1" {
		t.Fatalf("server listens on %q, want 127.0.0.1", s.Addr())
	}
	host, port, err := net.SplitHostPort(s.Addr())
	if err != nil || host != "127.0.0.1" || port == "0" {
		t.Fatalf("Addr() = %q", s.Addr())
	}
}

// TestARCH_TEST_001_WithPortStaysOnLoopback: a fixed port (sshtestd
// -port) still listens on 127.0.0.1 only.
func TestARCH_TEST_001_WithPortStaysOnLoopback(t *testing.T) {
	free := New(t)
	_, port, err := net.SplitHostPort(free.Addr())
	if err != nil {
		t.Fatal(err)
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	if err := free.Close(); err != nil {
		t.Fatal(err)
	}
	s := New(t, WithPort(p))
	if s.Addr() != net.JoinHostPort("127.0.0.1", port) {
		t.Fatalf("Addr() = %q, want 127.0.0.1:%s", s.Addr(), port)
	}
}

func TestARCH_TEST_001_HostKeyTypesAndKnownHosts(t *testing.T) {
	s := New(t, WithHostKeyTypes(Ed25519, ECDSA, RSA), WithPassword("u", "pw"))
	lines := s.KnownHostsLines()
	if len(lines) != 3 {
		t.Fatalf("known_hosts lines = %d, want 3", len(lines))
	}
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := writeLines(path, lines); err != nil {
		t.Fatal(err)
	}
	cb, err := knownhosts.New(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, algo := range []string{ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoRSASHA256} {
		c, err := ssh.Dial("tcp", s.Addr(), &ssh.ClientConfig{
			User: "u", Auth: []ssh.AuthMethod{ssh.Password("pw")},
			HostKeyCallback: cb, HostKeyAlgorithms: []string{algo},
		})
		if err != nil {
			t.Fatalf("%s: %v", algo, err)
		}
		_ = c.Close()
	}
}

func TestWriteKeyFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "id")
	if _, err := WriteKeyFile(path, Ed25519, 0o644); err != nil {
		t.Fatal(err)
	}
	if fi, err := statMode(path); err != nil || fi != 0o644 {
		t.Fatalf("mode = %o, %v", fi, err)
	}
}
