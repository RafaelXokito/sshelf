//go:build unix

package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// Session output safety end to end (T-042: SEC-SESSION-005, FR-TAB-003,
// FR-TAB-007, SEC-LOG-001).

// forbidden is what remote output must never make sshelf write to the
// user's terminal: string sequences (OSC, DCS, APC, PM, SOS), a reset,
// the modes sshelf alone sets on its terminal, paste markers, terminal
// queries and their answers, and the hostile corpus' payloads.
var forbidden = []struct {
	name string
	re   *regexp.Regexp
}{
	{"OSC", regexp.MustCompile(`\x1b\]`)},
	{"DCS", regexp.MustCompile(`\x1bP`)},
	{"APC", regexp.MustCompile(`\x1b_`)},
	{"PM", regexp.MustCompile(`\x1b\^`)},
	{"SOS", regexp.MustCompile(`\x1bX`)},
	{"RIS", regexp.MustCompile(`\x1bc`)},
	{"alternate screen toggle", regexp.MustCompile(`\x1b\[\?1049[hl]`)},
	{"bracketed paste toggle", regexp.MustCompile(`\x1b\[\?2004[hl]`)},
	{"focus events toggle", regexp.MustCompile(`\x1b\[\?1004[hl]`)},
	{"kitty keyboard", regexp.MustCompile(`\x1b\[[<>=?][0-9;]*u`)},
	{"paste marker", regexp.MustCompile(`\x1b\[20[01]~`)},
	{"DA or DA2 query or answer", regexp.MustCompile(`\x1b\[[>?=]?[0-9;]*c`)},
	{"DSR or CPR query or answer", regexp.MustCompile(`\x1b\[\??[0-9;]*[nR]`)},
	{"DECRQM query or answer", regexp.MustCompile(`\x1b\[\??[0-9;]*\$[py]`)},
	{"XTVERSION", regexp.MustCompile(`\x1b\[>[0-9;]*q`)},
	{"window report", regexp.MustCompile(`\x1b\[[0-9;]*t`)},
	{"hostile text", regexp.MustCompile(`evil`)},
	{"clipboard data", regexp.MustCompile(`c2VjcmV0|ZXZpbA`)},
	{"graphics data", regexp.MustCompile(`iVBOR|AAAA|~~@@vv`)},
}

// forbiddenIn returns what b holds of forbidden, and every C1 control,
// raw or UTF-8 encoded.
func forbiddenIn(b []byte) []string {
	var out []string
	for _, f := range forbidden {
		if loc := f.re.FindIndex(b); loc != nil {
			lo, hi := max(loc[0]-20, 0), min(loc[1]+20, len(b))
			out = append(out, fmt.Sprintf("%s: %q", f.name, b[lo:hi]))
		}
	}
	for i := 0; i < len(b); {
		r, n := utf8.DecodeRune(b[i:])
		if (r >= 0x80 && r <= 0x9f) || (r == utf8.RuneError && n == 1 && b[i] >= 0x80 && b[i] <= 0x9f) {
			out = append(out, fmt.Sprintf("C1 control at byte %d: %q", i, b[max(i-20, 0):min(i+20, len(b))]))
			break
		}
		i += n
	}
	return out
}

// TestSEC_SESSION_005_HostileOutputPTYScan runs sshtest's hostile script
// (HostileCorpus) in a framed session, then has sshelf repaint the whole
// frame by going to the Hosts tab and back. sshelf's PTY output over that
// time holds none of the forbidden sequences or C1 controls; the
// emulator's answers to the queries reached the remote, not the terminal
// (also ARCH-SESS-016's PTY output scan); and the tab still shows the
// host label.
func TestSEC_SESSION_005_HostileOutputPTYScan(t *testing.T) {
	s := startSlice(t)
	off := s.waitText(0, "web-prod demo@127.0.0.1")
	s.openSession(s.srv, 1)
	off = s.waitText(off, "connected · demo@127.0.0.1")
	start := s.out.Len()

	s.typeKeys("hostile\r")
	answers := []string{"\x1b[?62;1;6;22c", "\x1b[>1;10;0c", "0n", "R", "\x1b[?2004;"}
	answered := func() bool {
		got := s.srv.Received(1, 1)
		for _, a := range answers {
			if !bytes.Contains(got, []byte(a)) {
				return false
			}
		}
		return true
	}
	if !s.srv.WaitFor(10*time.Second, answered) {
		t.Fatalf("the remote received %q, want the emulator's answers %q", s.srv.Received(1, 1), answers)
	}
	// The shell took the answers as typed input: Ctrl+C drops that line.
	s.typeKeys("\x03size\r")
	off = s.waitText(off, "80x22")
	s.prefix("h")
	off = s.waitText(off, "[Hosts]")
	s.prefix("1")
	s.waitText(off, "[1 web-prod]")
	time.Sleep(100 * time.Millisecond) // the last frame
	window := s.out.From(start)

	for _, f := range forbiddenIn(window) {
		t.Error("sshelf wrote " + f)
	}
	if text := screenText(window); strings.Contains(text, "evil") || !strings.Contains(text, "80x22") {
		t.Errorf("the terminal shows %q", text)
	}
	if got := s.srv.Received(1, 1); bytes.Contains(got, []byte("evil")) || bytes.Contains(got, []byte("\x1b]")) {
		t.Errorf("a title or colour report went back to the remote: %q", got)
	}
	s.prefix("q")
	s.typeKeys("y")
	waitCloses(t, s, s.srv, 1, 2*time.Second)
	s.quit()
	s.checkStty()
}

// TestSEC_SESSION_005_NoRemoteTitleInTab: after the remote sets window
// and icon titles (the hostile corpus), the tab still shows the host
// label, the view sets no window title, and no frame line holds the
// remote's title (D-014, FR-TAB-003).
func TestSEC_SESSION_005_NoRemoteTitleInTab(t *testing.T) {
	l := newLiveModel(t)
	l.m, _ = l.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	l.connect()
	if !l.srv.WaitFor(5*time.Second, func() bool { return len(l.srv.RequestsOfType("shell")) == 1 }) {
		t.Fatal("no shell")
	}
	for _, r := range "hostile\r" {
		l.m, _ = l.m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if !l.srv.WaitFor(5*time.Second, func() bool { return bytes.Contains(l.received(1), []byte("\x1b[?62;1;6;22c")) }) {
		t.Fatalf("the hostile corpus was not processed; received %q", l.received(1))
	}
	v := l.m.View()
	if v.WindowTitle != "" {
		t.Errorf("window title %q", v.WindowTitle)
	}
	lines := strings.Split(ansi.Strip(v.Content), "\n")
	if !strings.Contains(lines[0], "[1 web-prod]") {
		t.Errorf("header %q, want the focused tab [1 web-prod]", lines[0])
	}
	for i, line := range lines {
		if strings.Contains(line, "evil") || strings.Contains(line, "title") {
			t.Errorf("line %d shows a remote title: %q", i, line)
		}
	}
}

// echoFlow connects to a host whose password is typed into sshelf's
// prompt, then types that password into the session, where the remote
// echoes it (and reports it as an unknown command). The session then
// ends, is reconnected (the password typed again) and ended again, and
// sshelf quits. It returns the run and the password, which check does not
// look for in the terminal output.
func echoFlow(t *testing.T, env ...string) (*canaryRun, string) {
	t.Helper()
	const pw = "CANARY-echoed-pw-7a3c91" //nolint:gosec // a made-up test secret
	srv := sshtest.New(t, sshtest.WithPassword("bob", pw))
	c := startCanary(t, canaries{{"typed password", pw}}, func(_, cfgDir string) {
		writeKnownHosts(t, cfgDir, srv.KnownHostsLine())
	}, env...)
	c.echoed = canaries{{"typed password", pw}}
	c.waitText(0, "Add host")
	c.addHost(hostForm{label: "echo-host", srv: srv, user: "bob"})

	off := c.out.Len()
	c.typeKeys(keyEnter)
	c.answerPrompt(off, srv, 1, "Password for bob@127.0.0.1", pw)
	c.tabs++
	c.waitText(off, "[1 echo-host]")
	c.waitText(off, "connected · bob@127.0.0.1")

	off = c.out.Len()
	c.typeKeys(pw + keyEnter) // the remote echoes it
	c.waitText(off, "unknown command: "+pw)
	off = c.out.Len()
	c.typeKeys("exit 3" + keyEnter)
	c.waitText(off, "exited (3) · r reconnect · x close")

	off = c.out.Len()
	c.typeKeys("r")
	c.answerPrompt(off, srv, 2, "Password for bob@127.0.0.1", pw)
	c.waitText(off, "connected · bob@127.0.0.1")
	off = c.out.Len()
	c.typeKeys("echo " + pw + keyEnter)
	c.waitText(off, "echo "+pw)
	c.endSession(off, "exit 0", "exited (0)")
	c.quit()
	return c, pw
}

// answerPrompt waits from off for the prompt titled prompt, types answer
// and waits until srv has started its n-th shell.
func (c *canaryRun) answerPrompt(off int, srv *sshtest.Server, n int, prompt, answer string) {
	c.t.Helper()
	c.waitText(off, prompt)
	c.typeKeys(answer + keyEnter)
	if !srv.WaitFor(10*time.Second, func() bool { return shells(srv) == n }) {
		c.t.Fatalf("no shell %d; output:\n%q", n, c.out.From(0))
	}
}

// chromeHits fails t for every line holding pw, and fails if no line
// holds want, so that an empty recording does not pass.
func chromeHits(t *testing.T, what string, lines []string, pw string, want ...string) {
	t.Helper()
	for _, w := range want {
		found := false
		for _, l := range lines {
			if strings.Contains(ansi.Strip(l), w) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no recorded %s shows %q", what, w)
		}
	}
	for i, l := range lines {
		if strings.Contains(ansi.Strip(l), pw) {
			t.Errorf("recorded %s %d holds the typed password", what, i)
		}
	}
}

// TestFR_TAB_003_CanaryNotInTabBar: a password typed into sshelf's
// prompt, then echoed by the remote, never appears in the header of any
// frame; the tab shows the host label.
func TestFR_TAB_003_CanaryNotInTabBar(t *testing.T) {
	c, pw := echoFlow(t)
	headers, _ := c.chrome()
	chromeHits(t, "header", headers, pw, "[1 echo-host]", "1 echo-host ✕", "[Hosts]")
	c.check()
}

// TestFR_TAB_007_CanaryNotInStatusBar: the same password never appears
// in the status bar of any frame, live, ended or reconnected.
func TestFR_TAB_007_CanaryNotInStatusBar(t *testing.T) {
	c, pw := echoFlow(t)
	_, bars := c.chrome()
	chromeHits(t, "status bar", bars, pw, "connected · bob@127.0.0.1", "exited (3) · r reconnect · x close", "exited (0)")
	c.check()
}

// TestSEC_LOG_001_CanaryHarnessM2 runs the M2 flows with SSHELF_DEBUG=1:
// two sessions (one with a stored password, one with a password typed
// into the prompt and echoed by the remote), tab switching, the help
// overlay, scrollback mode, an ended tab, a reconnect, the disconnect and
// quit confirmations. The terminal output (but for the echoed password),
// the chrome of every frame, the log and every file hold no canary.
func TestSEC_LOG_001_CanaryHarnessM2(t *testing.T) {
	const (
		storedPw = "CANARY-m2-stored-pw-0b5e7d" //nolint:gosec // made-up test secrets
		typedPw  = "CANARY-m2-typed-pw-c4182f"  //nolint:gosec
	)
	srvA := sshtest.New(t, sshtest.WithPassword("alice", storedPw))
	srvB := sshtest.New(t, sshtest.WithPassword("bob", typedPw))
	c := startCanary(t, canaries{{"stored password", storedPw}, {"typed password", typedPw}}, func(_, cfgDir string) {
		writeKnownHosts(t, cfgDir, srvA.KnownHostsLine(), srvB.KnownHostsLine())
	}, "SSHELF_DEBUG=1")
	c.echoed = canaries{{"typed password", typedPw}}
	c.waitText(0, "Add host")
	c.addHost(hostForm{label: "m2-stored", srv: srvA, user: "alice", password: storedPw})
	c.addHost(hostForm{label: "m2-typed", srv: srvB, user: "bob"})

	// step types keys and waits for every text in want, each from the
	// output before the keys.
	step := func(keys string, want ...string) {
		t.Helper()
		off := c.out.Len()
		c.typeKeys(keys)
		for _, w := range want {
			c.waitText(off, w)
		}
	}

	// Tab 1: the typed password, echoed by the remote.
	off := c.out.Len()
	c.typeKeys(keyEnter)
	c.answerPrompt(off, srvB, 1, "Password for bob@127.0.0.1", typedPw)
	c.waitText(off, "[1 m2-typed]")
	step(typedPw+keyEnter, "unknown command: "+typedPw)

	// Tab 2: the stored password.
	step("\x1dh", "enter connect")
	// m2-typed moved to the top on connecting (FR-NAV-018): m2-stored is
	// below it.
	step("j"+keyEnter, "[2 m2-stored]", "connected · alice@127.0.0.1")
	step("echo in-tab-2"+keyEnter, "in-tab-2")

	// Switching, help and scrollback.
	step("\x1d1", "[1 m2-typed]")
	step("\x1dn", "[2 m2-stored]")
	step("\x1dp", "[1 m2-typed]")
	step("\x1d?", "Press Ctrl+], then:")
	step(keyEsc, "connected · bob@127.0.0.1")
	step("\x1d[", "scrollback")
	step(keyEsc, "connected · bob@127.0.0.1")

	// Tab 1 ends and is reconnected with the password typed again.
	step("exit 5"+keyEnter, "exited (5) · r reconnect · x close")
	off = c.out.Len()
	c.typeKeys("r")
	c.answerPrompt(off, srvB, 2, "Password for bob@127.0.0.1", typedPw)
	c.waitText(off, "connected · bob@127.0.0.1")

	// Disconnect tab 1 with its confirmation, then quit with tab 2 open.
	step("\x1dq", "Disconnect m2-typed?")
	step("y", "enter connect")
	step("q", "Quit and close 1 tab?")
	c.typeKeys("y")
	c.waitQuit()

	headers, bars := c.chrome()
	chromeHits(t, "header", headers, typedPw, "[1 m2-typed]", "[2 m2-stored]", "1 m2-typed ✕")
	chromeHits(t, "header", headers, storedPw)
	chromeHits(t, "status bar", bars, typedPw, "connected · alice@127.0.0.1:"+strconv.Itoa(srvA.Port()), "exited (5)")
	chromeHits(t, "status bar", bars, storedPw)
	c.check()
	c.wantLog("method=password", `msg="connect: authenticated"`, `msg="session: ended" result="exited (5)"`, "host=m2-typed", "host=m2-stored")
}
