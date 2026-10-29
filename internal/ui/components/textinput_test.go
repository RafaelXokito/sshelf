package components

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;:?]*[ -/]*[@-~]`)

func plain(s string) string { return ansiSeq.ReplaceAllString(s, "") }

func typeInto(t *TextInput, s string) {
	for _, r := range s {
		t.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func pressKey(t *TextInput, code rune, mod tea.KeyMod) {
	t.Update(tea.KeyPressMsg{Code: code, Mod: mod})
}

func focused() *TextInput {
	t := NewTextInput()
	t.Focus()
	return t
}

func TestTextInputTypingAndCursor(t *testing.T) {
	in := focused()
	typeInto(in, "helo")
	pressKey(in, tea.KeyLeft, 0)
	typeInto(in, "l")
	if in.Value() != "hello" || in.Cursor() != 4 {
		t.Fatalf("value %q cursor %d", in.Value(), in.Cursor())
	}
	pressKey(in, tea.KeyHome, 0)
	typeInto(in, ">")
	pressKey(in, tea.KeyEnd, 0)
	typeInto(in, "!")
	if in.Value() != ">hello!" {
		t.Fatalf("value %q", in.Value())
	}
	pressKey(in, tea.KeyBackspace, 0)
	pressKey(in, 'a', tea.ModCtrl) // home
	pressKey(in, tea.KeyDelete, 0)
	if in.Value() != "hello" || in.Cursor() != 0 {
		t.Fatalf("value %q cursor %d", in.Value(), in.Cursor())
	}
	pressKey(in, tea.KeyLeft, 0) // clamped
	pressKey(in, tea.KeyRight, 0)
	pressKey(in, tea.KeyRight, 0)
	pressKey(in, 'k', tea.ModCtrl)
	if in.Value() != "he" {
		t.Fatalf("ctrl+k: %q", in.Value())
	}
	pressKey(in, tea.KeyLeft, 0)
	pressKey(in, 'u', tea.ModCtrl)
	if in.Value() != "e" || in.Cursor() != 0 {
		t.Fatalf("ctrl+u: %q cursor %d", in.Value(), in.Cursor())
	}
}

func TestTextInputPasteSanitisedAndLimited(t *testing.T) {
	in := focused()
	in.CharLimit = 8
	in.Update(tea.PasteMsg{Content: "a\x1b[31mb\tc\nd\x07efghijk"})
	if in.Value() != "abcdefgh" {
		t.Fatalf("value %q", in.Value())
	}
	typeInto(in, "z")
	if in.Len() != 8 {
		t.Fatalf("limit exceeded: %q", in.Value())
	}
}

func TestTextInputBlurredIgnoresKeys(t *testing.T) {
	in := NewTextInput()
	typeInto(in, "x")
	in.Update(tea.PasteMsg{Content: "y"})
	if in.Value() != "" {
		t.Fatalf("blurred input took %q", in.Value())
	}
	in.SetValue("abc")
	if in.View() != "abc" {
		t.Fatalf("blurred view %q", in.View())
	}
}

func TestTextInputMaskedHidesValueAndLength(t *testing.T) {
	const secret = "canary-secret-value" //nolint:gosec // test data
	for _, n := range []int{1, len(secret)} {
		in := focused()
		in.Echo = EchoMasked
		in.SetValue(secret[:n])
		v := plain(in.View())
		if strings.Contains(v, secret[:n]) || v != Mask+"_" {
			t.Fatalf("masked view %q for %d runes", v, n)
		}
	}
	in := focused()
	in.Echo = EchoMasked
	in.Placeholder = "not set"
	if v := plain(in.View()); v != "_not set" {
		t.Fatalf("empty masked view %q", v)
	}
}

func TestTextInputEchoNoneShowsNothing(t *testing.T) {
	in := focused()
	in.Echo = EchoNone
	in.Placeholder = "ignored"
	typeInto(in, "secret")
	if v := plain(in.View()); v != "_" {
		t.Fatalf("view %q", v)
	}
	in.Blur()
	if v := in.View(); v != "" {
		t.Fatalf("blurred view %q", v)
	}
}

func TestTextInputPlaceholder(t *testing.T) {
	in := NewTextInput()
	in.Placeholder = "22"
	if v := plain(in.View()); v != "22" {
		t.Fatalf("view %q", v)
	}
	in.Focus()
	if v := plain(in.View()); v != "_22" {
		t.Fatalf("focused view %q", v)
	}
}

func TestTextInputWideRunesFitWidth(t *testing.T) {
	in := focused()
	in.Width = 7
	in.SetValue("日本語テキストabc")
	for i := 0; i <= in.Len(); i++ {
		in.SetCursor(i)
		v := plain(in.View())
		if w := lipgloss.Width(v); w > in.Width {
			t.Fatalf("cursor %d: view %q is %d columns wide, limit %d", i, v, w, in.Width)
		}
		if strings.ContainsRune(v, '�') {
			t.Fatalf("cursor %d: view %q has a broken rune", i, v)
		}
	}
	in.End()
	if v := plain(in.View()); v != "トabc_" {
		t.Fatalf("end view %q", v)
	}
	in.Home()
	if v := plain(in.View()); !strings.HasPrefix(v, "日本語") {
		t.Fatalf("home view %q", v)
	}
	in.Blur()
	if v := in.View(); lipgloss.Width(v) > in.Width || !strings.HasPrefix(v, "日本語") {
		t.Fatalf("blurred view %q", v)
	}
}

func TestTextInputZeroesBuffers(t *testing.T) {
	in := focused()
	typeInto(in, "secret")
	old := in.value[:cap(in.value)]
	in.Insert(strings.Repeat("x", cap(in.value))) // forces a new buffer
	for _, r := range old {
		if r != 0 && r != 'x' {
			t.Fatalf("old buffer not zeroed: %q", string(old))
		}
	}
	buf := in.value[:cap(in.value)]
	in.Home()
	pressKey(in, tea.KeyDelete, 0)
	in.Reset()
	for _, r := range buf {
		if r != 0 {
			t.Fatalf("buffer not zeroed after Reset: %q", string(buf))
		}
	}
}
