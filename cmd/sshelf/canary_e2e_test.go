//go:build unix

package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// Terminal keys.
const (
	keyTab   = "\t"
	keyEnter = "\r"
	keyEsc   = "\x1b"
	keyRight = "\x1b[C"
	keySave  = "\x13" // ctrl+s
)

// paste wraps s in a bracketed paste.
func paste(s string) string { return "\x1b[200~" + s + "\x1b[201~" }

// canaryRun is sshelf, built with sshelf_faultinject so that its
// credential store is an in-memory one, running in a PTY with its own
// HOME, XDG_CONFIG_HOME and TMPDIR. The build records the header and the
// bottom line of every frame in framesFile (frames_fault.go), inside
// TMPDIR, so that check scans sshelf's own chrome on its own too.
type canaryRun struct {
	*slice
	tmp    string
	frames string
	cs     canaries
	// echoed are canaries the remote was sent and printed back in the
	// session area: the remote's choice (T-042). The terminal output is
	// not scanned for them; the chrome, the log and every file still are.
	echoed canaries
}

// startCanary starts the fault-injection build with env added. setup,
// when not nil, prepares HOME and the sshelf config dir (which exists
// already, mode 0700).
func startCanary(t *testing.T, cs canaries, setup func(home, cfgDir string), env ...string) *canaryRun {
	t.Helper()
	tmp := t.TempDir()
	frames := filepath.Join(tmp, "frames.txt")
	s := startFreshBin(t, faultBinary(t), func(home, xdg string) {
		cfgDir := filepath.Join(xdg, "sshelf")
		if _, err := paths.EnsureDir(cfgDir); err != nil {
			t.Fatal(err)
		}
		if setup != nil {
			setup(home, cfgDir)
		}
	}, append([]string{"TMPDIR=" + tmp, "SSHELF_FAULT_FRAMES=" + frames}, env...)...)
	return &canaryRun{slice: s, tmp: tmp, frames: frames, cs: cs}
}

// chrome returns the headers and the bottom lines of the frames sshelf
// drew, as recorded in the frames file.
func (c *canaryRun) chrome() (headers, bars []string) {
	c.t.Helper()
	b, err := os.ReadFile(c.frames)
	if err != nil {
		c.t.Fatal(err)
	}
	for l := range strings.SplitSeq(string(b), "\n") {
		switch {
		case strings.HasPrefix(l, "H "):
			headers = append(headers, l[2:])
		case strings.HasPrefix(l, "S "):
			bars = append(bars, l[2:])
		}
	}
	return headers, bars
}

// writeKnownHosts writes sshelf's known_hosts with lines.
func writeKnownHosts(t *testing.T, cfgDir string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(paths.KnownHostsFile(cfgDir), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// hostForm is what to type into the Add host form. identity is how many
// times to press → on the identity picker, from "(none)".
type hostForm struct {
	label    string
	srv      *sshtest.Server
	user     string
	identity int
	password string
	key      []byte // pasted into "Stored key"
	pass     string // key passphrase
}

// addHost fills in and saves the Add host form, opened with a from the
// host list or the Welcome screen, and waits until the host is saved; it
// is then selected.
func (c *canaryRun) addHost(f hostForm) {
	c.t.Helper()
	off := c.out.Len()
	c.typeKeys("a")
	off = c.waitText(off, "Add host")
	keys := f.label + keyTab + f.srv.Host() + keyTab + strconv.Itoa(f.srv.Port()) + keyTab + f.user + keyTab +
		strings.Repeat(keyRight, f.identity) + keyTab + f.password + keyTab + keyTab
	if f.key != nil {
		keys += paste(string(f.key))
	}
	keys += keyTab + f.pass + keySave
	c.typeKeys(keys)
	c.waitText(off, f.label+": saved")
}

// connectShell presses Enter on the selected host and waits for its
// shell (the n-th one on srv) to open in a tab with the host's label.
// There it types a command and waits for its output inside the frame,
// then exits, waits for the ended tab, closes it with x and waits for the
// host list. answer, when not empty, is typed into the prompt that
// appears first, whose title contains prompt.
func (c *canaryRun) connectShell(label string, srv *sshtest.Server, n int, prompt, answer string) {
	c.t.Helper()
	off := c.out.Len()
	c.typeKeys(keyEnter)
	if answer != "" {
		off = c.waitText(off, prompt)
		c.typeKeys(answer + keyEnter)
	}
	if !srv.WaitFor(10*time.Second, func() bool { return len(srv.RequestsOfType("shell")) == n }) {
		c.t.Fatalf("%s: no shell; output:\n%q", label, c.out.From(0))
	}
	c.tabs++
	off = c.waitText(off, fmt.Sprintf("[%d %s]", c.tabs, label)) // the focused tab
	c.typeKeys("echo in-the-frame" + keyEnter)
	off = c.waitText(off, "in-the-frame")
	c.endSession(off, "exit 0", "exited (0)")
}

// typeUntil types keys until done reports true, for a prompt that may not
// have opened yet: keys typed too early go to the Connecting modal, which
// ignores them. done must stay true once it is true (count with >=): a
// retry typed while the server was still checking the previous answer
// lands in the next prompt and moves the count past the target.
func (c *canaryRun) typeUntil(srv *sshtest.Server, keys string, done func() bool) {
	c.t.Helper()
	for range 10 {
		c.typeKeys(keys)
		if srv.WaitFor(time.Second, done) {
			return
		}
	}
	c.t.Fatalf("the answer never reached the server; output:\n%q", c.out.From(0))
}

// check scans every sink: the terminal output (stdout and stderr share
// the PTY) for every canary the remote did not echo, and every file under
// HOME (the state dir with the debug log and crash reports is in it), the
// config dir (config.json, known_hosts) and TMPDIR (with the chrome of
// every frame) for every canary. sshelf's environment is fixed at exec and
// holds no canary; TestARCH_SEC_001_CanaryScan checks os.Environ() in
// process.
func (c *canaryRun) check() {
	c.t.Helper()
	time.Sleep(50 * time.Millisecond) // let the drain collect the last output
	var screen canaries
	for _, cn := range c.cs {
		if !slices.Contains(c.echoed, cn) {
			screen = append(screen, cn)
		}
	}
	hits := screen.hits("the terminal output", c.out.From(0))
	fileHits, files := c.cs.fileHits(c.t, c.home, c.cfgDir, c.tmp)
	if files == 0 {
		c.t.Error("no file was scanned")
	}
	if st, err := os.Stat(c.frames); err != nil || st.Size() == 0 {
		c.t.Errorf("no frame was recorded: %v", err)
	}
	for _, h := range append(hits, fileHits...) {
		c.t.Error(h)
	}
}

// log returns the debug log.
func (c *canaryRun) log() []byte {
	c.t.Helper()
	b, err := os.ReadFile(paths.LogFile(c.state))
	if err != nil {
		c.t.Fatal(err)
	}
	return b
}

// wantLog fails unless the debug log has every entry in want, so that a
// clean scan is not just an empty log.
func (c *canaryRun) wantLog(want ...string) {
	c.t.Helper()
	log := c.log()
	for _, w := range want {
		if !bytes.Contains(log, []byte(w)) {
			c.t.Errorf("the debug log lacks %s", w)
		}
	}
}

// mainFlows plants canaries through every MVP path that handles a secret
// and drives them end to end:
//
//   - h1-password: password typed in the host form, connect.
//   - kbd-id: an identity created with a password; h2-kbdint uses it and
//     connects with keyboard-interactive (the stored password auto-answers
//     the password round; a typed one-time code answers the next).
//   - h3-storedkey: an encrypted key pasted into the host form with its
//     passphrase, connect with public key.
//   - h4-wrongpw: a wrong stored password, then two wrong typed ones until
//     "authentication failed".
//   - h5-mismatch: a stored password for a host whose key changed: the
//     connection is blocked before authentication.
//   - an import of ~/.ssh/config, and the delete of h1-password with its
//     stored password.
//
// It returns the canaries and the running sshelf, back on the host list.
func mainFlows(t *testing.T, env ...string) *canaryRun {
	t.Helper()
	const (
		pw        = "CANARY-h1-pw-4b8e21"          //nolint:gosec // made-up test secrets
		kbdPw     = "CANARY-kbd-pw-a07c55"         //nolint:gosec
		otp       = "CANARY-otp-913d6f"            //nolint:gosec
		pass      = "CANARY-key-pass-58e0c2"       //nolint:gosec
		wrongPw   = "CANARY-wrong-stored-pw-c3d90" //nolint:gosec
		typedPw   = "CANARY-wrong-typed-pw-17be4"  //nolint:gosec
		blockedPw = "CANARY-blocked-pw-6f2a8d"     //nolint:gosec
	)
	pemKey, pub := newCanaryKey(t, pass)
	cs := canaries{{"h1 password", pw}, {"identity password", kbdPw}, {"one-time code", otp},
		{"key passphrase", pass}, {"wrong stored password", wrongPw}, {"wrong typed password", typedPw},
		{"blocked host password", blockedPw}}
	cs = append(cs, keyCanaries("stored key", pemKey)...)

	srvPW := sshtest.New(t, sshtest.WithPassword("alice", pw))
	srvKbd := sshtest.New(t, sshtest.WithKeyboardInteractive("bob",
		sshtest.Challenge{Questions: []sshtest.Question{{Prompt: "Password: ", Answer: kbdPw}}},
		sshtest.Challenge{Instruction: "Second factor", Questions: []sshtest.Question{{Prompt: "Verification code: ", Answer: otp}}}))
	srvKey := sshtest.New(t, sshtest.WithAuthorizedKey("carol", pub))
	srvBlocked := sshtest.New(t, sshtest.WithPassword("alice", blockedPw))

	c := startCanary(t, cs, func(home, cfgDir string) {
		// srvBlocked's address is known with srvPW's key: a changed key.
		changed := knownhosts.Line([]string{knownhosts.Normalize(srvBlocked.Addr())}, srvPW.HostKeys()[0])
		writeKnownHosts(t, cfgDir, srvPW.KnownHostsLine(), srvKbd.KnownHostsLine(), srvKey.KnownHostsLine(), changed)
		if err := os.Mkdir(filepath.Join(home, ".ssh"), 0o700); err != nil {
			t.Fatal(err)
		}
		cfg := fmt.Sprintf("Host zz-imported\n  HostName 127.0.0.1\n  Port %d\n  User carol\n  IdentityFile ~/.ssh/id_ed25519\n", srvKey.Port())
		if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(cfg), 0o600); err != nil {
			t.Fatal(err)
		}
	}, env...)
	c.waitText(0, "Import from ~/.ssh/config") // the Welcome screen, after the store probe

	c.addHost(hostForm{label: "h1-password", srv: srvPW, user: "alice", password: pw})
	c.connectShell("h1-password", srvPW, 1, "", "")

	off := c.out.Len()
	c.typeKeys("i")
	off = c.waitText(off, "Identities")
	c.typeKeys("a")
	off = c.waitText(off, "Add identity")
	c.typeKeys("kbd-id" + keyTab + "bob" + keyTab + kbdPw + keySave)
	off = c.waitText(off, "kbd-id: saved")
	c.typeKeys(keyEsc)
	c.waitText(off, "h1-password")
	c.addHost(hostForm{label: "h2-kbdint", srv: srvKbd, identity: 1})
	c.connectShell("h2-kbdint", srvKbd, 1, "Prompt from 127.0.0.1", otp)

	c.addHost(hostForm{label: "h3-storedkey", srv: srvKey, user: "carol", key: pemKey, pass: pass})
	c.connectShell("h3-storedkey", srvKey, 1, "", "")

	c.addHost(hostForm{label: "h4-wrongpw", srv: srvPW, user: "alice", password: wrongPw})
	off = c.out.Len()
	failed := func() int {
		n := 0
		for _, a := range srvPW.AuthAttempts() {
			if a.Method == "password" && !a.Success {
				n++
			}
		}
		return n
	}
	c.typeKeys(keyEnter)
	c.waitText(off, "Permission denied, please try again.")
	c.typeUntil(srvPW, typedPw+keyEnter, func() bool { return failed() >= 2 })
	// The second prompt looks like the first, so the renderer may not
	// redraw it: typeUntil retries until the server sees the answer. (This
	// step used to flake: the first prompt's late DoneMsg could close the
	// second prompt; the root model now pops a prompt when it is answered.)
	c.typeUntil(srvPW, typedPw+keyEnter, func() bool { return failed() >= 3 })
	c.waitText(off, "h4-wrongpw: authentication failed")

	c.addHost(hostForm{label: "h5-mismatch", srv: srvBlocked, user: "alice", password: blockedPw})
	off = c.out.Len()
	c.typeKeys(keyEnter)
	c.waitText(off, "h5-mismatch: host key changed") // the status line, under the modal
	off = c.waitText(off, "HOST KEY HAS CHANGED")
	c.typeKeys(keyEsc)
	c.waitText(off, "> h5-mismatch")
	if n := len(srvBlocked.AuthAttempts()); n != 0 {
		t.Errorf("%d auth attempts on the host with a changed key", n)
	}

	off = c.out.Len()
	c.typeKeys("I")
	off = c.waitText(off, "Created 1 host")
	c.typeKeys(keyEnter)
	off = c.waitText(off, "zz-imported")

	// The list is in last-use order (FR-NAV-018): find h1-password, and
	// Esc leaves it selected. Filtering is synchronous and keys are read in
	// order, so Esc acts on the whole query; the delete prompt below proves
	// the selection. Do not wait for the query line itself: the renderer
	// redraws only the cells that changed, so "/ h1-pas" and "sword" can
	// arrive in separate writes with a cursor move between them, and the
	// whole query is then never on the screen in one piece (this flaked).
	c.typeKeys("/h1-password")
	off = c.waitText(off, "esc clear search")
	c.typeKeys(keyEsc)
	off = c.waitText(off, "zz-imported") // the whole list is back
	c.typeKeys("d")
	off = c.waitText(off, "Delete host h1-password?")
	c.typeKeys("y")
	c.waitText(off, "h1-password: deleted")
	return c
}

// TestSEC_CRED_001_CanaryNotInAnyFile saves a password, an identity
// password, a stored key and its passphrase through the forms, uses them,
// imports, deletes, and then byte-searches every file sshelf could have
// written: zero hits. The connections prove the store held the secrets.
// Debug logging is off, so no state dir may appear.
func TestSEC_CRED_001_CanaryNotInAnyFile(t *testing.T) {
	c := mainFlows(t)
	c.quit()
	c.check()
	if _, err := os.Stat(c.state); !os.IsNotExist(err) {
		t.Errorf("state dir created without SSHELF_DEBUG: %v", err)
	}
}

// TestSEC_LOG_001_CanaryHarness runs the SEC-LOG-001 flows with
// SSHELF_DEBUG=1: create an identity, connect (success, wrong password,
// host-key mismatch), store denied and store unavailable. Stdout, stderr,
// the log and every file stay free of canaries.
func TestSEC_LOG_001_CanaryHarness(t *testing.T) {
	t.Run("flows", func(t *testing.T) {
		c := mainFlows(t, "SSHELF_DEBUG=1")
		c.quit()
		c.check()
		c.wantLog("method=password", "method=keyboard-interactive", "method=publickey",
			`msg="connect: authenticated"`, "host=h4-wrongpw")
	})

	// seedPasswordHost writes a config with one host on srv whose hidden
	// identity has a stored password, as a previous run would have.
	seedPasswordHost := func(t *testing.T, label string, srv *sshtest.Server) func(home, cfgDir string) {
		return func(_, cfgDir string) {
			writeKnownHosts(t, cfgDir, srv.KnownHostsLine())
			cfg := `{"version": 1,
  "identities": [{"id": "0192a000-0000-7000-8000-0000000000a1", "name": "` + label + `", "hidden": true, "has_password": true}],
  "hosts": [{"id": "0192a000-0000-7000-8000-0000000000b1", "label": "` + label + `", "hostname": "127.0.0.1",
             "port": ` + strconv.Itoa(srv.Port()) + `, "user": "alice", "identity_id": "0192a000-0000-7000-8000-0000000000a1"}]}`
			if err := os.WriteFile(paths.ConfigFile(cfgDir), []byte(cfg), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("store denied", func(t *testing.T) {
		const pw = "CANARY-denied-pw-0e7d31" //nolint:gosec // a made-up test secret
		srv := sshtest.New(t, sshtest.WithPassword("alice", pw))
		c := startCanary(t, canaries{{"password", pw}}, seedPasswordHost(t, "denied-host", srv),
			"SSHELF_DEBUG=1", "SSHELF_FAULT_STORE=denied")
		off := c.waitText(0, "denied-host alice@127.0.0.1")
		c.typeKeys(keyEnter)
		off = c.waitText(off, "denied-host: Access to")
		if n := len(srv.AuthAttempts()); n > 1 { // at most "none"
			t.Errorf("%d auth attempts after the store denied access", n)
		}
		// Saving a typed password fails the same way and keeps the form.
		c.typeKeys("a")
		off = c.waitText(off, "Add host")
		c.typeKeys("other" + keyTab + "127.0.0.1" + keyTab + keyTab + "alice" + keyTab + keyTab + pw + keySave)
		off = c.waitText(off, "was denied")
		c.typeKeys(keyEsc)
		off = c.waitText(off, "Discard unsaved changes?")
		c.typeKeys("y")
		c.waitText(off, "denied-host alice@127.0.0.1")
		c.quit()
		c.check()
		c.wantLog(`msg="connect: start" host=denied-host`)
	})

	t.Run("store unavailable", func(t *testing.T) {
		const pw = "CANARY-nostore-pw-d4a610" //nolint:gosec // a made-up test secret
		srv := sshtest.New(t, sshtest.WithPassword("alice", pw))
		c := startCanary(t, canaries{{"password", pw}}, seedPasswordHost(t, "nostore-host", srv),
			"SSHELF_DEBUG=1", "SSHELF_FAULT_STORE=unavailable")
		c.waitText(0, "nostore-host alice@127.0.0.1")
		// The password is typed at the prompt and used once.
		c.connectShell("nostore-host", srv, 1, "Password for alice@127.0.0.1", pw)
		c.quit()
		c.check()
		c.wantLog("method=password", `msg="connect: authenticated"`, "credential store: not available")
	})
}

// TestFR_IDENT_004_DebugLogHasNoPassword: after a password login with
// debug logging on, the log has no occurrence of the password.
func TestFR_IDENT_004_DebugLogHasNoPassword(t *testing.T) {
	const pw = "CANARY-ident4-pw-85c3b7" //nolint:gosec // a made-up test secret
	srv := sshtest.New(t, sshtest.WithPassword("alice", pw))
	c := startCanary(t, canaries{{"password", pw}}, func(_, cfgDir string) {
		writeKnownHosts(t, cfgDir, srv.KnownHostsLine())
	}, "SSHELF_DEBUG=1")
	c.waitText(0, "Add host")
	c.addHost(hostForm{label: "pw-host", srv: srv, user: "alice", password: pw})
	c.connectShell("pw-host", srv, 1, "", "")
	c.quit()
	c.wantLog("method=password", `msg="connect: authenticated"`)
	for _, h := range c.cs.hits("the debug log", c.log()) {
		t.Error(h)
	}
	c.check()
}

// TestARCH_SEC_001_CanaryScanAfterPanic: sshelf connects with a stored
// password and then panics in Update while the session is open in its tab
// (on its first output). The crash report, the terminal output, the log
// and every file stay free of the password.
func TestARCH_SEC_001_CanaryScanAfterPanic(t *testing.T) {
	const pw = "CANARY-panic-pw-e19f42" //nolint:gosec // a made-up test secret
	srv := sshtest.New(t, sshtest.WithPassword("alice", pw))
	c := startCanary(t, canaries{{"password", pw}, {"panic value", faultCanary}}, func(_, cfgDir string) {
		writeKnownHosts(t, cfgDir, srv.KnownHostsLine())
	}, "SSHELF_DEBUG=1", "SSHELF_FAULT=session-update", "SSHELF_FAULT_CANARY="+faultCanary)
	c.waitText(0, "Add host")
	c.addHost(hostForm{label: "pw-host", srv: srv, user: "alice", password: pw})
	c.typeKeys(keyEnter)
	if code := c.waitExit(); code != exitCrash {
		t.Fatalf("exit status %d, want %d", code, exitCrash)
	}
	c.checkCrash()
	c.wantLog("method=password", `msg="connect: authenticated"`, "msg=panic")
	c.check()
}
