package session

import (
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
)

// The remote's mouse modes decide what SendMouse forwards and how it is
// encoded (FR-TAB-011, ADR-013).

func TestFR_TAB_011_MotionOnlyWhenRequested(t *testing.T) {
	press := uv.MouseClickEvent{X: 2, Y: 1, Button: uv.MouseLeft}
	release := uv.MouseReleaseEvent{X: 2, Y: 1, Button: uv.MouseLeft}
	wheel := uv.MouseWheelEvent{X: 2, Y: 1, Button: uv.MouseWheelDown}
	drag := uv.MouseMotionEvent{X: 3, Y: 1, Button: uv.MouseLeft}
	hover := uv.MouseMotionEvent{X: 3, Y: 1}
	type want struct{ press, release, wheel, drag, hover string }
	for _, tc := range []struct {
		name  string
		modes string // what the remote sent
		mode  MouseMode
		want  want
	}{
		{"no mode", "", MouseNone, want{}},
		{"X10 ?9", "\x1b[?9h", MousePress, want{press: "\x1b[M #\"", wheel: "\x1b[Ma#\""}},
		{"normal ?1000", "\x1b[?1000h", MousePress, want{press: "\x1b[M #\"", release: "\x1b[M##\"", wheel: "\x1b[Ma#\""}},
		{"normal ?1000, SGR", "\x1b[?1000h\x1b[?1006h", MousePress,
			want{press: "\x1b[<0;3;2M", release: "\x1b[<0;3;2m", wheel: "\x1b[<65;3;2M"}},
		{"button ?1002, SGR", "\x1b[?1002h\x1b[?1006h", MouseDrag,
			want{press: "\x1b[<0;3;2M", release: "\x1b[<0;3;2m", wheel: "\x1b[<65;3;2M", drag: "\x1b[<32;4;2M"}},
		{"any ?1003, SGR", "\x1b[?1003h\x1b[?1006h", MouseAny,
			want{press: "\x1b[<0;3;2M", release: "\x1b[<0;3;2m", wheel: "\x1b[<65;3;2M", drag: "\x1b[<32;4;2M", hover: "\x1b[<35;4;2M"}},
		{"?1000 turned off again", "\x1b[?1000h\x1b[?1006h\x1b[?1000l", MouseNone, want{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, r := drainScreen(t, 80, 24)
			s.write([]byte(tc.modes))
			if got := s.snapshot().Mouse; got != tc.mode {
				t.Fatalf("Snapshot().Mouse = %v, want %v", got, tc.mode)
			}
			for _, ev := range []struct {
				name string
				m    uv.MouseEvent
				want string
			}{
				{"press", press, tc.want.press},
				{"release", release, tc.want.release},
				{"wheel", wheel, tc.want.wheel},
				{"drag", drag, tc.want.drag},
				{"hover", hover, tc.want.hover},
			} {
				s.sendMouse(ev.m)
				if got := string(r.take(t, s)); got != ev.want {
					t.Errorf("%s sent %q, want %q", ev.name, got, ev.want)
				}
			}
		})
	}
}

// TestFR_TAB_011_SessionSGRThroughEchoHex: after the remote sends
// ?1000h ?1006h, a left press at cell 10, 4 of the session reaches the
// remote as ESC [ < 0 ; 11 ; 5 M (echo-hex prints what it receives).
func TestFR_TAB_011_SessionSGRThroughEchoHex(t *testing.T) {
	srv := newServer(t)
	s := open(t, srv, Options{})
	if err := s.SendBytes([]byte("modes 1000,1006\r")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for s.Snapshot().Mouse != MousePress {
		if time.Now().After(deadline) {
			t.Fatalf("Snapshot().Mouse = %v after modes 1000,1006", s.Snapshot().Mouse)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if err := s.SendBytes([]byte("echo-hex\r")); err != nil {
		t.Fatal(err)
	}
	waitReceived(t, srv, 1, "echo-hex\r")
	if err := s.SendMouse(uv.MouseClickEvent{X: 10, Y: 4, Button: uv.MouseLeft}); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, s, "1b 5b 3c 30 3b 31 31 3b 35 4d") // ESC [ < 0 ; 1 1 ; 5 M
}
