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
	f.Update(shiftTab) // Cancel
	f.Update(shiftTab) // Save
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
	f.SetFocus(0)
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

// newButtonForm returns a form of a text input, a choice and a second
// text input, with a reserved field width.
func newButtonForm() (*Form, *TextInput, *Choice, *TextInput) {
	a, b := NewTextInput(), NewTextInput()
	c := NewChoice(Option{Label: "none"}, Option{Label: "one", Value: "1"})
	f := NewForm().Add("Name", a).Add("Kind", c).Add("Port", b)
	f.FieldWidth = 12
	f.MarkClean()
	return f, a, c, b
}

type formPos struct {
	field  int
	button Button
}

func TestFR_NAV_015_TabReachesButtons(t *testing.T) {
	f, a, c, b := newButtonForm()
	tab := tea.KeyPressMsg{Code: tea.KeyTab}
	shiftTab := tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	for i, want := range []formPos{{1, ButtonNone}, {2, ButtonNone}, {-1, ButtonSave}, {-1, ButtonCancel}, {0, ButtonNone}} {
		f.Update(tab)
		if got := (formPos{f.Focused(), f.FocusedButton()}); got != want {
			t.Fatalf("tab %d: focus %+v, want %+v", i+1, got, want)
		}
	}
	for i, want := range []formPos{{-1, ButtonCancel}, {-1, ButtonSave}, {2, ButtonNone}} {
		f.Update(shiftTab)
		if got := (formPos{f.Focused(), f.FocusedButton()}); got != want {
			t.Fatalf("shift+tab %d: focus %+v, want %+v", i+1, got, want)
		}
	}
	// On a button no field has focus, and the button carries the focus
	// glyph, not colour alone (NFR-UX-003).
	f.Update(tab)
	if a.Focused() || b.Focused() || c.focused {
		t.Fatal("a field kept focus while a button has it")
	}
	lines := strings.Split(plain(f.View()), "\n")
	if got := strings.TrimRight(lines[4], " "); got != "      > [ Save ]   [ Cancel ]" {
		t.Fatalf("button row %q", got)
	}
	if strings.Contains(strings.Join(lines[:3], "\n"), FocusGlyph) {
		t.Fatalf("a field row shows the focus glyph:\n%s", plain(f.View()))
	}
	// Typing and pasting on a button change nothing.
	f.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	f.Update(tea.PasteMsg{Content: "pasted"})
	if f.Dirty() {
		t.Fatal("typing on a button changed a field")
	}
	f.FocusButton(ButtonCancel)
	if f.FocusedButton() != ButtonCancel || f.Focused() != -1 {
		t.Fatal("FocusButton(ButtonCancel)")
	}
	if got := strings.TrimRight(strings.Split(plain(f.View()), "\n")[4], " "); got != "        [ Save ] > [ Cancel ]" {
		t.Fatalf("button row %q", got)
	}
	f.SetFocus(1)
	if f.FocusedButton() != ButtonNone || !c.focused {
		t.Fatal("SetFocus from a button")
	}
}

func TestFR_NAV_015_EnterOnSaveSubmits(t *testing.T) {
	f, _, _, _ := newButtonForm()
	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyEnter}, {Code: tea.KeySpace, Text: " "}} {
		f.FocusButton(ButtonSave)
		if ev, _ := f.Update(k); ev != FormSubmit || f.FocusedButton() != ButtonSave {
			t.Errorf("%s on Save: event %v, focus %v", k, ev, f.FocusedButton())
		}
	}
	// esc still cancels with Save focused.
	if ev, _ := f.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); ev != FormCancel {
		t.Fatal("esc on Save is not FormCancel")
	}
	if f.Press(ButtonSave) != FormSubmit || f.Press(ButtonNone) != FormNone {
		t.Fatal("Press")
	}
}

func TestFR_NAV_015_SpaceOnCancelCancels(t *testing.T) {
	f, a, c, _ := newButtonForm()
	for _, k := range []tea.KeyPressMsg{{Code: tea.KeySpace, Text: " "}, {Code: tea.KeyEnter}} {
		f.FocusButton(ButtonCancel)
		if ev, _ := f.Update(k); ev != FormCancel || f.FocusedButton() != ButtonCancel {
			t.Errorf("%s on Cancel: event %v, focus %v", k, ev, f.FocusedButton())
		}
	}
	// ctrl+s still submits with Cancel focused.
	if ev, _ := f.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}); ev != FormSubmit {
		t.Fatal("ctrl+s on Cancel is not FormSubmit")
	}
	// On fields, space goes to the field as before.
	f.SetFocus(0)
	if ev, _ := f.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}); ev != FormNone || a.Value() != " " {
		t.Fatalf("space on a text field: event %v, value %q", ev, a.Value())
	}
	f.SetFocus(1)
	if ev, _ := f.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}); ev != FormNone || c.Value() != "1" {
		t.Fatalf("space on a choice: event %v, value %q", ev, c.Value())
	}
}

// cellsAt returns w columns of view's line y from column x.
func cellsAt(view string, x, y, w int) string {
	lines := strings.Split(plain(view), "\n")
	if y >= len(lines) {
		return ""
	}
	r := []rune(lines[y])
	if x >= len(r) {
		return ""
	}
	return string(r[x:min(x+w, len(r))])
}

func TestFR_NAV_015_LayoutMatchesView(t *testing.T) {
	f, a, _, _ := newButtonForm()
	a.SetValue("web")
	f.SetError(2, "bad port")
	for _, focus := range []func(){
		func() { f.SetFocus(0) },
		func() { f.SetFocus(1) },
		func() { f.FocusButton(ButtonSave) },
		func() { f.FocusButton(ButtonCancel) },
	} {
		focus()
		v, l := f.View(), f.Layout()
		if got := strings.Count(v, "\n"); got != l.Height {
			t.Fatalf("view has %d lines, layout says %d", got, l.Height)
		}
		for i, label := range []string{"Name", "Kind", "Port"} {
			fl := l.Fields[i]
			if got := cellsAt(v, fl.Label.X, fl.Label.Y, fl.Label.W); strings.TrimSpace(strings.TrimPrefix(got, FocusGlyph)) != label {
				t.Errorf("field %d label rect holds %q", i, got)
			}
			if got, want := cellsAt(v, fl.Value.X, fl.Value.Y, fl.Value.W), plain(f.rows[i].field.View()); !strings.HasPrefix(got, want) {
				t.Errorf("field %d value rect holds %q, want %q", i, got, want)
			}
			if fl.Value.X != fl.Label.X+fl.Label.W+2 || fl.Value.W != 12 || fl.Label.Y != i {
				t.Errorf("field %d rects %+v", i, fl)
			}
			if fx, b := f.HitTest(fl.Value.X+fl.Value.W-1, fl.Value.Y); fx != i || b != ButtonNone {
				t.Errorf("hit on field %d value: %d, %d", i, fx, b)
			}
			if fx, b := f.HitTest(0, fl.Label.Y); fx != i || b != ButtonNone {
				t.Errorf("hit on field %d label: %d, %d", i, fx, b)
			}
		}
		if got := cellsAt(v, l.Save.X, l.Save.Y, l.Save.W); got != SaveButton {
			t.Errorf("save rect holds %q", got)
		}
		if got := cellsAt(v, l.Cancel.X, l.Cancel.Y, l.Cancel.W); got != CancelButton {
			t.Errorf("cancel rect holds %q", got)
		}
		errX := l.Fields[2].Value.X + l.Fields[2].Value.W + 3
		for _, tc := range []struct {
			x, y int
			want formPos
		}{
			{l.Save.X, l.Save.Y, formPos{-1, ButtonSave}},
			{l.Save.X + l.Save.W - 1, l.Save.Y, formPos{-1, ButtonSave}},
			{l.Cancel.X, l.Cancel.Y, formPos{-1, ButtonCancel}},
			{l.Cancel.X + l.Cancel.W - 1, l.Cancel.Y, formPos{-1, ButtonCancel}},
			{l.Save.X - 1, l.Save.Y, formPos{-1, ButtonNone}},              // focus marker
			{l.Save.X + l.Save.W, l.Save.Y, formPos{-1, ButtonNone}},       // gap
			{l.Cancel.X + l.Cancel.W, l.Cancel.Y, formPos{-1, ButtonNone}}, // after
			{0, l.Save.Y - 1, formPos{-1, ButtonNone}},                     // blank line
			{errX, l.Fields[2].Value.Y, formPos{-1, ButtonNone}},           // the error
		} {
			if fx, b := f.HitTest(tc.x, tc.y); (formPos{fx, b}) != tc.want {
				t.Errorf("HitTest(%d, %d) = %d, %d; want %+v", tc.x, tc.y, fx, b, tc.want)
			}
		}
	}
}
