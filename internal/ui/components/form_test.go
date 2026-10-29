package components

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestFormFocusDirtyAndEvents(t *testing.T) {
	a, b := NewTextInput(), NewTextInput()
	c := NewChoice(Option{Label: "none"}, Option{Label: "one", Value: "1"})
	f := NewForm().Add("A", a).Add("B", b).Add("C", c)
	f.MarkClean()
	if !a.Focused() || b.Focused() || f.Dirty() {
		t.Fatal("initial focus or dirty state wrong")
	}
	shiftTab := tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	if ev, _ := f.Update(shiftTab); ev != FormNone || f.Focused() != 2 {
		t.Fatalf("shift+tab from the first field: focus %d", f.Focused())
	}
	f.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if c.Value() != "1" || !f.Dirty() {
		t.Fatalf("choice %q, dirty %v", c.Value(), f.Dirty())
	}
	f.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if f.Dirty() {
		t.Fatal("dirty after restoring the value")
	}
	f.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // wraps to A
	f.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if a.Value() != "x" || b.Value() != "" {
		t.Fatalf("typing went to the wrong field: %q %q", a.Value(), b.Value())
	}
	if ev, _ := f.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}); ev != FormSubmit {
		t.Fatal("ctrl+s is not FormSubmit")
	}
	if ev, _ := f.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); ev != FormCancel {
		t.Fatal("esc is not FormCancel")
	}
	f.FieldWidth = 10
	f.SetError(1, "bad")
	if !f.FocusFirstError() || f.Focused() != 1 {
		t.Fatal("FocusFirstError")
	}
	if l := strings.Split(plain(f.View()), "\n")[1]; l != "> B  _           ✗ bad" {
		t.Fatalf("error row %q", l)
	}
	f.ClearErrors()
	if f.Error(1) != "" {
		t.Fatal("ClearErrors")
	}
}
