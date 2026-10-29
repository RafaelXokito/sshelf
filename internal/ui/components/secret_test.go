package components

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSecretFieldNeverShowsOrReturnsTheSecret(t *testing.T) {
	const canary = "canary-123456789"
	f := NewSecretField("set")
	f.Focus()
	if v := f.View(); !strings.Contains(v, "(set)") {
		t.Fatalf("view %q", v)
	}
	f.Update(tea.PasteMsg{Content: canary})
	if v := f.View(); strings.Contains(v, canary) || !strings.Contains(v, Mask) {
		t.Fatalf("view %q", v)
	}
	if f.Value() == canary || !f.Changed() || string(f.Bytes()) != canary {
		t.Fatal("Value leaks the secret, or Bytes lost it")
	}
	f.Reset()
	if f.Changed() || f.Bytes() != nil || f.Value() != "" {
		t.Fatal("Reset left a value")
	}
}

func TestSecretFieldClear(t *testing.T) {
	ctrlX := tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl}
	f := NewSecretField("not set on this device")
	f.Focus()
	f.Update(ctrlX)
	if !f.Cleared() || f.Value() != "cleared" || !strings.Contains(f.View(), "(will be cleared)") {
		t.Fatalf("not cleared: %q", f.View())
	}
	f.Update(tea.KeyPressMsg{Code: 'a', Text: "a"}) // typing undoes it
	if f.Cleared() || !f.Changed() {
		t.Fatal("typing did not undo the clear")
	}
	unset := NewSecretField("not set")
	unset.Focus()
	unset.Update(ctrlX)
	if unset.Cleared() {
		t.Fatal("an unset secret was marked to clear")
	}
}

func TestTextInputNewlinesAndBytes(t *testing.T) {
	in := NewTextInput()
	in.Focus()
	in.Update(tea.PasteMsg{Content: "a\r\nb\tc"})
	if in.Value() != "abc" {
		t.Fatalf("value %q", in.Value())
	}
	in.Reset()
	in.Newlines = true
	in.Update(tea.PasteMsg{Content: "a\r\nb€"})
	if b := in.Bytes(); string(b) != "a\nb€" {
		t.Fatalf("bytes %q", b)
	}
}
