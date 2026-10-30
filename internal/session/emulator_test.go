package session

import (
	"bytes"
	"fmt"
	"image/color"
	"io"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// replies collects what a screen sends to the remote, as the reply pump
// of a session would.
type replies struct {
	mu   sync.Mutex
	b    bytes.Buffer
	done chan struct{}
}

func (r *replies) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.b.Write(p)
}

// drainScreen returns a screen of cols×rows whose replies are collected.
func drainScreen(t testing.TB, cols, rows int) (*screen, *replies) {
	t.Helper()
	s := newScreen(cols, rows, 0)
	r := &replies{done: make(chan struct{})}
	go func() { _, _ = io.Copy(r, s); close(r.done) }()
	t.Cleanup(func() { s.closeReplies(); <-r.done })
	return s, r
}

// marker ends every exchange, so take knows when the bytes before it are
// complete. It is a byte sequence no key encodes to.
const marker = "\x00<end>\x00"

// take returns everything sent to the remote since the last take.
func (r *replies) take(t testing.TB, s *screen) []byte {
	t.Helper()
	s.sendBytes([]byte(marker))
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		b := r.b.Bytes()
		if i := bytes.Index(b, []byte(marker)); i >= 0 {
			out := bytes.Clone(b[:i])
			r.b.Next(i + len(marker))
			r.mu.Unlock()
			return out
		}
		r.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("marker not received")
		}
		time.Sleep(time.Millisecond)
	}
}

func key(code rune, mod uv.KeyMod) uv.KeyPressEvent {
	return uv.KeyPressEvent{Code: code, Mod: mod}
}

func TestARCH_SESS_017_KeyTable(t *testing.T) {
	const (
		shift = uv.ModShift
		alt   = uv.ModAlt
		ctrl  = uv.ModCtrl
	)
	type row struct {
		name   string
		k      uv.KeyPressEvent
		normal string
		app    string // "" = same as normal
	}
	rows := []row{
		{"text", uv.KeyPressEvent{Code: 'a', Text: "a"}, "a", ""},
		{"non-ASCII text", uv.KeyPressEvent{Code: 'é', Text: "é"}, "é", ""},
		{"shifted text", uv.KeyPressEvent{Code: 'a', Text: "A", Mod: shift}, "A", ""},
		{"enter", key(uv.KeyEnter, 0), "\r", ""},
		{"tab", key(uv.KeyTab, 0), "\t", ""},
		{"backspace", key(uv.KeyBackspace, 0), "\x7f", ""},
		{"esc", key(uv.KeyEscape, 0), "\x1b", ""},
		{"shift+tab", key(uv.KeyTab, shift), "\x1b[Z", ""},
		{"ctrl+a", key('a', ctrl), "\x01", ""},
		{"ctrl+z", key('z', ctrl), "\x1a", ""},
		{"ctrl+[", key('[', ctrl), "\x1b", ""},
		{"ctrl+\\", key('\\', ctrl), "\x1c", ""},
		{"ctrl+]", key(']', ctrl), "\x1d", ""},
		{"ctrl+^", key('^', ctrl), "\x1e", ""},
		{"ctrl+_", key('_', ctrl), "\x1f", ""},
		{"ctrl+space", key(uv.KeySpace, ctrl), "\x00", ""},
		{"alt+a", uv.KeyPressEvent{Code: 'a', Text: "a", Mod: alt}, "\x1ba", ""},
		{"alt+enter", key(uv.KeyEnter, alt), "\x1b\r", ""},
		{"up", key(uv.KeyUp, 0), "\x1b[A", "\x1bOA"},
		{"down", key(uv.KeyDown, 0), "\x1b[B", "\x1bOB"},
		{"right", key(uv.KeyRight, 0), "\x1b[C", "\x1bOC"},
		{"left", key(uv.KeyLeft, 0), "\x1b[D", "\x1bOD"},
		{"home", key(uv.KeyHome, 0), "\x1b[H", "\x1bOH"},
		{"end", key(uv.KeyEnd, 0), "\x1b[F", "\x1bOF"},
		{"insert", key(uv.KeyInsert, 0), "\x1b[2~", ""},
		{"delete", key(uv.KeyDelete, 0), "\x1b[3~", ""},
		{"pgup", key(uv.KeyPgUp, 0), "\x1b[5~", ""},
		{"pgdn", key(uv.KeyPgDown, 0), "\x1b[6~", ""},
		{"f1", key(uv.KeyF1, 0), "\x1bOP", ""},
		{"f4", key(uv.KeyF4, 0), "\x1bOS", ""},
		{"f5", key(uv.KeyF5, 0), "\x1b[15~", ""},
		{"f12", key(uv.KeyF12, 0), "\x1b[24~", ""},
		// The shim (ARCH-SESS-017).
		{"ctrl+up", key(uv.KeyUp, ctrl), "\x1b[1;5A", ""},
		{"shift+left", key(uv.KeyLeft, shift), "\x1b[1;2D", ""},
		{"alt+shift+right", key(uv.KeyRight, alt|shift), "\x1b[1;4C", ""},
		{"alt+down", key(uv.KeyDown, alt), "\x1b[1;3B", ""},
		{"ctrl+shift+home", key(uv.KeyHome, ctrl|shift), "\x1b[1;6H", ""},
		{"shift+end", key(uv.KeyEnd, shift), "\x1b[1;2F", ""},
		{"ctrl+f5", key(uv.KeyF5, ctrl), "\x1b[15;5~", ""},
		{"shift+f12", key(uv.KeyF12, shift), "\x1b[24;2~", ""},
		{"ctrl+delete", key(uv.KeyDelete, ctrl), "\x1b[3;5~", ""},
		{"shift+insert", key(uv.KeyInsert, shift), "\x1b[2;2~", ""},
		{"alt+pgup", key(uv.KeyPgUp, alt), "\x1b[5;3~", ""},
		{"ctrl+alt+shift+pgdn", key(uv.KeyPgDown, ctrl|alt|shift), "\x1b[6;8~", ""},
		{"shift+f1", key(uv.KeyF1, shift), "\x1b[1;2P", ""},
		{"ctrl+f4", key(uv.KeyF4, ctrl), "\x1b[1;5S", ""},
	}
	for _, mode := range []string{"normal", "application"} {
		s, r := drainScreen(t, 80, 24)
		if mode == "application" {
			s.write([]byte("\x1b[?1h"))
		}
		for _, tc := range rows {
			want := tc.normal
			if mode == "application" && tc.app != "" {
				want = tc.app
			}
			s.sendKey(tc.k)
			if got := string(r.take(t, s)); got != want {
				t.Errorf("%s cursor mode, %s: sent %q, want %q", mode, tc.name, got, want)
			}
		}
	}
}

// TestARCH_SESS_017_NoKeyEncodesToNothing sends every key that has an
// xterm encoding with every combination of Shift, Alt and Ctrl.
func TestARCH_SESS_017_NoKeyEncodesToNothing(t *testing.T) {
	s, r := drainScreen(t, 80, 24)
	codes := []rune{
		uv.KeyUp, uv.KeyDown, uv.KeyRight, uv.KeyLeft, uv.KeyHome, uv.KeyEnd,
		uv.KeyInsert, uv.KeyDelete, uv.KeyPgUp, uv.KeyPgDown,
		uv.KeyEnter, uv.KeyTab, uv.KeyBackspace, uv.KeyEscape, uv.KeySpace,
	}
	for f := uv.KeyF1; f <= uv.KeyF12; f++ {
		codes = append(codes, f)
	}
	for c := 'a'; c <= 'z'; c++ {
		codes = append(codes, c)
	}
	codes = append(codes, []rune("0123456789`-=[]\\;',./")...)
	for _, appCursor := range []bool{false, true} {
		if appCursor {
			s.write([]byte("\x1b[?1h"))
		}
		for _, c := range codes {
			for m := uv.KeyMod(0); m <= uv.ModShift|uv.ModAlt|uv.ModCtrl; m++ {
				if m&^(uv.ModShift|uv.ModAlt|uv.ModCtrl) != 0 {
					continue
				}
				k := uv.KeyPressEvent{Code: c, Mod: m}
				if c < uv.KeyExtended && c > ' ' && m&uv.ModCtrl == 0 {
					k.Text = string(c) // Bubble Tea sets the text of printable keys
				}
				s.sendKey(k)
				if got := r.take(t, s); len(got) == 0 {
					t.Errorf("application cursor %v: %v encodes to nothing", appCursor, uv.Key(k))
				}
			}
		}
	}
}

func TestARCH_SESS_016_EmulatorAnswersDA1AndCPR(t *testing.T) {
	s, r := drainScreen(t, 80, 24)
	s.write([]byte("\x1b[c"))
	if got := string(r.take(t, s)); got != "\x1b[?62;1;6;22c" {
		t.Errorf("DA1 answer %q", got)
	}
	s.write([]byte("hello\r\n\x1b[3;7H\x1b[6n"))
	if got := string(r.take(t, s)); got != "\x1b[3;7R" {
		t.Errorf("CPR answer %q, want ESC[3;7R", got)
	}
	s.setDefaultColors(color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff}, color.RGBA{R: 0xfd, G: 0xf6, B: 0xe3, A: 0xff})
	s.write([]byte("\x1b]11;?\x1b\\\x1b]10;?\x07"))
	got := string(r.take(t, s))
	if !strings.Contains(got, "rgb:fdfd/f6f6/e3e3") || !strings.Contains(got, "rgb:1111/2222/3333") {
		t.Errorf("OSC 10/11 answers %q", got)
	}
}

// visible matches what may appear in a rendered line besides text: SGR
// sequences only.
var sgrOnly = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

// checkRendered fails if a rendered line holds anything but text and SGR.
func checkRendered(t testing.TB, lines []string) {
	t.Helper()
	for i, l := range lines {
		rest := sgrOnly.ReplaceAllString(l, "")
		for _, r := range rest {
			if r < 0x20 || (r >= 0x7f && r < 0xa0) {
				t.Fatalf("line %d holds control character %U: %q", i, r, l)
			}
		}
		for _, bad := range []string{"\x1b]", "\x1bP", "\x1b_", "\x1b^", "\x1bX"} {
			if strings.Contains(l, bad) {
				t.Fatalf("line %d holds %q: %q", i, bad, l)
			}
		}
	}
}

func TestARCH_SESS_011_NoLinksRendered(t *testing.T) {
	s, _ := drainScreen(t, 40, 5)
	s.write([]byte("\x1b]8;;https://evil.example/\x1b\\click\x1b]8;;\x1b\\ \x1b[1mbold\x1b[m"))
	snap := s.snapshot()
	checkRendered(t, snap.Lines)
	if !strings.Contains(snap.Lines[0], "click") || strings.Contains(snap.Lines[0], "evil") {
		t.Fatalf("line 0 = %q", snap.Lines[0])
	}
	for _, h := range sshtest.HostileCorpus {
		s.write([]byte(h))
	}
	checkRendered(t, s.snapshot().Lines)
	lines, _ := s.scrollback(0, 100)
	checkRendered(t, lines)
}

func FuzzSEC_SESSION_005_RenderOnlySGR(f *testing.F) {
	for _, h := range sshtest.HostileCorpus {
		f.Add([]byte(h))
	}
	f.Add([]byte("日本語ab👍x\x1b[31mred\x1b[0m\r\n\x1b[?1049h\x1b[2J"))
	f.Fuzz(func(t *testing.T, data []byte) {
		s, _ := drainScreen(t, 10, 4)
		s.write(data)
		snap := s.snapshot()
		checkRendered(t, snap.Lines)
		if len(snap.Lines) != 4 {
			t.Fatalf("%d lines", len(snap.Lines))
		}
		lines, _ := s.scrollback(0, 50)
		checkRendered(t, lines)
	})
}

// plainLine is a rendered line without styles and trailing spaces.
func plainLine(l string) string { return strings.TrimRight(sgrOnly.ReplaceAllString(l, ""), " ") }

func TestARCH_TEST_002_WideCharacterAtMargin(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		// ADR-013 limitation 2, the measured cases at width 10.
		{"12345678日x", []string{"12345678日", "x"}},
		{"123456789日x", []string{"123456789", "日x"}},
		{"日本語ab👍x", []string{"日本語ab👍", "x"}},
		{"日本語abc👍x", []string{"日本語abc", "👍x"}},
		{"1234567890x", []string{"1234567890", "x"}},
	} {
		for _, chunk := range []int{1, 3, 64} {
			s, _ := drainScreen(t, 10, 3)
			in := []byte(tc.in)
			for i := 0; i < len(in); i += chunk {
				s.write(in[i:min(i+chunk, len(in))])
			}
			snap := s.snapshot()
			for i, w := range tc.want {
				if got := plainLine(snap.Lines[i]); got != w {
					t.Errorf("%q in %d-byte chunks: line %d = %q, want %q", tc.in, chunk, i, got, w)
				}
			}
		}
	}
}

func TestNFR_SIZE_003_ScrollbackCapped(t *testing.T) {
	s, _ := drainScreen(t, 20, 5)
	for i := range 2100 {
		s.write(fmt.Appendf(nil, "line %d\r\n", i))
	}
	if n := s.scrollbackLen(); n != DefaultScrollback {
		t.Fatalf("scrollback holds %d lines, want %d", n, DefaultScrollback)
	}
	lines, total := s.scrollback(0, 1)
	if total != DefaultScrollback+5 || plainLine(lines[0]) != "line 96" {
		t.Fatalf("oldest line %q of %d", lines, total)
	}
	// Lines 0 to 95 were dropped: line top of the view is line top+96.
	if n := s.droppedLines(); n != 96 {
		t.Fatalf("dropped %d lines, want 96", n)
	}
	// A remote clearing the scrollback (ED 3) drops every line kept.
	s.write([]byte("\x1b[3J"))
	if n, k := s.droppedLines(), s.scrollbackLen(); n != 96+DefaultScrollback || k != 0 {
		t.Fatalf("after ED 3: dropped %d, kept %d", n, k)
	}
}
