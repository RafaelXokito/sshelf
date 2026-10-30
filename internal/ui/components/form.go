package components

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/RafaelXokito/sshelf/internal/ui/keys"
)

var (
	formLabelStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	formFocusStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	formErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	helpStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	choiceArrowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
)

// ErrorPrefix starts every error message, so errors do not rely on colour
// alone (NFR-UX-003).
const ErrorPrefix = "✗ "

// FormScreen is a screen that holds a form. Ctrl+C cancels it instead of
// quitting the app (FR-NAV-003): the root model closes any modals and
// handles the message Cancel returns as if the screen had sent it.
type FormScreen interface {
	Screen
	Cancel() tea.Msg
}

// ShowModalMsg asks the root model to push Modal onto the modal stack,
// for example a confirm modal from a form.
type ShowModalMsg struct{ Modal Modal }

// FormField is one field of a Form. *TextInput and *Choice implement it.
type FormField interface {
	// Update handles a key press or paste while the field has focus.
	Update(msg tea.Msg) tea.Cmd
	// View renders the field without its label.
	View() string
	Focus()
	Blur()
	// Value is compared with the value at the last MarkClean to tell
	// whether the form has unsaved changes.
	Value() string
}

// FormEvent is what a key did to a Form.
type FormEvent int

const (
	// FormNone means the key was handled inside the form, or ignored.
	FormNone FormEvent = iota
	// FormSubmit means ctrl+s: the screen validates and saves.
	FormSubmit
	// FormCancel means esc: the screen closes the form, after asking
	// when Dirty reports unsaved changes.
	FormCancel
)

type formRow struct {
	label string
	field FormField
	err   string
}

// Button names one of the buttons every Form shows after its fields
// (FR-NAV-015).
type Button int

const (
	// ButtonNone means no button: a field has focus, or a point is not on
	// a button.
	ButtonNone Button = iota
	// ButtonSave is "[ Save ]"; pressing it is FormSubmit, like ctrl+s.
	ButtonSave
	// ButtonCancel is "[ Cancel ]"; pressing it is FormCancel, like esc.
	ButtonCancel
)

// Button labels, as rendered.
const (
	SaveButton   = "[ Save ]"
	CancelButton = "[ Cancel ]"
)

// FocusGlyph marks the focused field or button, so focus does not rely on
// colour alone (NFR-UX-003).
const FocusGlyph = "> "

// numButtons is the number of buttons after the fields; in the focus
// order they follow the last field, Save first.
const numButtons = 2

// Rect is a rectangle of cells in a Form's View: X and Y are the column
// and line of its top-left cell, counted from 0 at the top-left of the
// View; W and H are its width and height in cells.
type Rect struct{ X, Y, W, H int }

// Contains reports whether the cell at column x, line y is inside r.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// FieldLayout is where one field's row sits in a Form's View.
type FieldLayout struct {
	// Label covers the focus marker and the padded label.
	Label Rect
	// Value covers the field's view, at least FieldWidth columns wide;
	// the error after it is not included.
	Value Rect
}

// FormLayout is where a Form's View puts its fields and buttons, for
// mouse hit-testing (FR-NAV-015). It matches the View rendered in the
// same state.
type FormLayout struct {
	Fields []FieldLayout // in the order the fields were added
	Save   Rect          // "[ Save ]", without its focus marker
	Cancel Rect          // "[ Cancel ]", without its focus marker
	Height int           // lines in the View
}

// Form lays out labelled fields one per row, followed by a row with the
// [ Save ] and [ Cancel ] buttons (FR-NAV-015). Tab and shift+tab move
// focus through the fields and then the buttons, wrapping; enter or space
// on a focused button reports FormSubmit or FormCancel, the same as ctrl+s
// and esc, which work anywhere. Every other key and paste goes to the
// focused field, and does nothing on a button. Each row can show an error
// next to its field (FR-HOST-003). The form does not validate or save: the
// screen that owns it does.
//
// Use it by pointer, built with NewForm and Add:
//
//	f := components.NewForm()
//	f.FieldWidth = 32
//	label := components.NewTextInput()
//	f.Add("Label", label).Add("Port", port)
//	f.MarkClean()
type Form struct {
	// FieldWidth is the column width reserved for each field, so errors
	// line up after it; 0 means errors follow the field directly.
	FieldWidth int

	rows  []formRow
	focus int // a field index; len(rows) is Save, len(rows)+1 Cancel
	clean []string
}

// NewForm returns an empty form.
func NewForm() *Form { return &Form{} }

// Add appends a field with its label and returns f. The first field added
// gets focus.
func (f *Form) Add(label string, field FormField) *Form {
	f.rows = append(f.rows, formRow{label: label, field: field})
	if len(f.rows) == 1 {
		field.Focus()
	} else {
		field.Blur()
	}
	return f
}

// Len returns the number of fields.
func (f *Form) Len() int { return len(f.rows) }

// Focused returns the index of the focused field, or -1 when a button has
// focus.
func (f *Form) Focused() int {
	if f.focus >= len(f.rows) {
		return -1
	}
	return f.focus
}

// FocusedButton returns the focused button, or ButtonNone when a field
// has focus.
func (f *Form) FocusedButton() Button {
	if len(f.rows) == 0 {
		return ButtonNone
	}
	switch f.focus - len(f.rows) {
	case 0:
		return ButtonSave
	case 1:
		return ButtonCancel
	}
	return ButtonNone
}

// SetFocus focuses field i (ignored when out of range).
func (f *Form) SetFocus(i int) {
	if i < 0 || i >= len(f.rows) {
		return
	}
	f.moveFocus(i)
}

// FocusButton focuses button b (ignored for ButtonNone or an empty form).
func (f *Form) FocusButton(b Button) {
	if len(f.rows) == 0 {
		return
	}
	switch b {
	case ButtonSave:
		f.moveFocus(len(f.rows))
	case ButtonCancel:
		f.moveFocus(len(f.rows) + 1)
	}
}

// Press returns the event of pressing button b: FormSubmit for Save,
// FormCancel for Cancel, FormNone otherwise. Focus does not move.
func (f *Form) Press(b Button) FormEvent {
	switch b {
	case ButtonSave:
		return FormSubmit
	case ButtonCancel:
		return FormCancel
	}
	return FormNone
}

// moveFocus moves focus to position i of the focus order: the fields,
// then Save and Cancel.
func (f *Form) moveFocus(i int) {
	if f.focus < len(f.rows) {
		f.rows[f.focus].field.Blur()
	}
	f.focus = i
	if i < len(f.rows) {
		f.rows[i].field.Focus()
	}
}

// SetError shows msg next to field i; "" clears it.
func (f *Form) SetError(i int, msg string) {
	if i >= 0 && i < len(f.rows) {
		f.rows[i].err = msg
	}
}

// Error returns the error shown next to field i.
func (f *Form) Error(i int) string {
	if i < 0 || i >= len(f.rows) {
		return ""
	}
	return f.rows[i].err
}

// ClearErrors removes every field error.
func (f *Form) ClearErrors() {
	for i := range f.rows {
		f.rows[i].err = ""
	}
}

// FocusFirstError focuses the first field with an error and reports
// whether there was one.
func (f *Form) FocusFirstError() bool {
	for i, r := range f.rows {
		if r.err != "" {
			f.SetFocus(i)
			return true
		}
	}
	return false
}

// MarkClean records the current values as saved; Dirty compares with them.
func (f *Form) MarkClean() {
	f.clean = f.clean[:0]
	for _, r := range f.rows {
		f.clean = append(f.clean, r.field.Value())
	}
}

// Dirty reports whether any value differs from the last MarkClean.
func (f *Form) Dirty() bool {
	for i, r := range f.rows {
		if i >= len(f.clean) || r.field.Value() != f.clean[i] {
			return true
		}
	}
	return false
}

// Update handles a message for the form; see Form.
func (f *Form) Update(msg tea.Msg) (FormEvent, tea.Cmd) {
	if len(f.rows) == 0 {
		return FormNone, nil
	}
	n := len(f.rows) + numButtons
	k, isKey := msg.(tea.KeyPressMsg)
	if isKey {
		switch {
		case key.Matches(k, keys.Save):
			return FormSubmit, nil
		case key.Matches(k, keys.Cancel):
			return FormCancel, nil
		case key.Matches(k, keys.NextField):
			f.moveFocus((f.focus + 1) % n)
			return FormNone, nil
		case key.Matches(k, keys.PrevField):
			f.moveFocus((f.focus - 1 + n) % n)
			return FormNone, nil
		}
	}
	b := f.FocusedButton()
	if b == ButtonNone {
		return FormNone, f.rows[f.focus].field.Update(msg)
	}
	if isKey && key.Matches(k, keys.PressButton) {
		return f.Press(b), nil
	}
	return FormNone, nil
}

// View renders one row per field: a focus marker, the label padded to
// the widest label, the field, and its error if any. A blank line and the
// button row follow, with the buttons starting in the field column.
func (f *Form) View() string {
	v, _ := f.render()
	return v
}

// Layout returns where View puts each field and button in the form's
// current state.
func (f *Form) Layout() FormLayout {
	_, l := f.render()
	return l
}

// HitTest returns what is at column x, line y of View: a field index (or
// -1) and a button (or ButtonNone); at most one is set. A point on a
// field's error, a focus marker of a button or a blank cell hits nothing.
func (f *Form) HitTest(x, y int) (field int, button Button) {
	l := f.Layout()
	for i, fl := range l.Fields {
		if fl.Label.Contains(x, y) || fl.Value.Contains(x, y) {
			return i, ButtonNone
		}
	}
	switch {
	case l.Save.Contains(x, y):
		return -1, ButtonSave
	case l.Cancel.Contains(x, y):
		return -1, ButtonCancel
	}
	return -1, ButtonNone
}

// clickable is a FormField that reacts to the click that focused it:
// x is the column in the field's View, or -1 for a click on the label,
// and wasFocused says whether the field had focus before the click.
type clickable interface {
	click(x int, wasFocused bool)
}

// Click handles a left press at column x, line y of View
// (FR-NAV-015): on a field's label or value it focuses the field (a text
// field with its cursor at the end; on a focused choice, ‹ and › change
// the option); on a button it focuses and presses it, returning
// FormSubmit or FormCancel exactly like ctrl+s and esc. Anywhere else it
// does nothing.
func (f *Form) Click(x, y int) FormEvent {
	field, b := f.HitTest(x, y)
	if b != ButtonNone {
		f.FocusButton(b)
		return f.Press(b)
	}
	if field < 0 {
		return FormNone
	}
	vx := -1
	if v := f.Layout().Fields[field].Value; v.Contains(x, y) {
		vx = x - v.X
	}
	was := f.focus == field
	f.SetFocus(field)
	if c, ok := f.rows[field].field.(clickable); ok {
		c.click(vx, was)
	}
	return FormNone
}

// render builds the View and its layout together, so that they agree.
func (f *Form) render() (string, FormLayout) {
	lw := 0
	for _, r := range f.rows {
		lw = max(lw, lipgloss.Width(r.label))
	}
	marker := len(FocusGlyph)
	blank := strings.Repeat(" ", marker)
	labelW := marker + lw
	valueX := labelW + 2
	l := FormLayout{Fields: make([]FieldLayout, 0, len(f.rows))}
	var b strings.Builder
	y := 0
	for i, r := range f.rows {
		label := pad(r.label, lw)
		if i == f.focus {
			b.WriteString(formFocusStyle.Render(FocusGlyph + label))
		} else {
			b.WriteString(blank + formLabelStyle.Render(label))
		}
		b.WriteString("  ")
		v := r.field.View()
		if r.err != "" {
			b.WriteString(pad(v, f.FieldWidth))
			b.WriteString("  " + formErrorStyle.Render(ErrorPrefix+r.err))
		} else {
			b.WriteString(v)
		}
		b.WriteString("\n")
		h := strings.Count(v, "\n") + 1
		l.Fields = append(l.Fields, FieldLayout{
			Label: Rect{X: 0, Y: y, W: labelW, H: h},
			Value: Rect{X: valueX, Y: y, W: max(f.FieldWidth, lipgloss.Width(v)), H: h},
		})
		y += h
	}

	b.WriteString("\n")
	y++
	b.WriteString(strings.Repeat(" ", valueX-marker))
	x := valueX
	focused := f.FocusedButton()
	for i, btn := range []struct {
		which Button
		text  string
		rect  *Rect
	}{{ButtonSave, SaveButton, &l.Save}, {ButtonCancel, CancelButton, &l.Cancel}} {
		if i > 0 {
			b.WriteString(" ")
			x += 1 + marker
		}
		if btn.which == focused {
			b.WriteString(formFocusStyle.Render(FocusGlyph + btn.text))
		} else {
			b.WriteString(blank + btn.text)
		}
		w := lipgloss.Width(btn.text)
		*btn.rect = Rect{X: x, Y: y, W: w, H: 1}
		x += w
	}
	b.WriteString("\n")
	l.Height = y + 1
	return b.String(), l
}

// pad right-pads s with spaces to w columns.
func pad(s string, w int) string {
	if n := lipgloss.Width(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

// Help renders a one-line key hint, "key desc • key desc", from bindings.
func Help(bindings ...key.Binding) string {
	parts := make([]string, 0, len(bindings))
	for _, kb := range bindings {
		h := kb.Help()
		parts = append(parts, h.Key+" "+h.Desc)
	}
	return helpStyle.Render(strings.Join(parts, " • "))
}

// Option is one choice of a Choice field.
type Option struct {
	Label string // shown; the caller sanitises it
	Value string // returned by Value
}

// Choice is a form field that picks one of a fixed list of options with
// ← and → (or space), wrapping around. Use it by pointer.
type Choice struct {
	options []Option
	sel     int
	focused bool
}

// NewChoice returns a choice with the first option selected.
func NewChoice(options ...Option) *Choice { return &Choice{options: options} }

// Select selects the first option with value v and reports whether there
// was one.
func (c *Choice) Select(v string) bool {
	for i, o := range c.options {
		if o.Value == v {
			c.sel = i
			return true
		}
	}
	return false
}

// Selected returns the selected option (the zero Option when there are
// none).
func (c *Choice) Selected() Option {
	if c.sel >= len(c.options) {
		return Option{}
	}
	return c.options[c.sel]
}

// Value returns the selected option's value.
func (c *Choice) Value() string { return c.Selected().Value }

// Focus implements FormField.
func (c *Choice) Focus() { c.focused = true }

// Blur implements FormField.
func (c *Choice) Blur() { c.focused = false }

// Update implements FormField.
func (c *Choice) Update(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok || !c.focused || len(c.options) == 0 {
		return nil
	}
	switch {
	case key.Matches(k, keys.ChoicePrev):
		c.sel = (c.sel - 1 + len(c.options)) % len(c.options)
	case key.Matches(k, keys.ChoiceNext):
		c.sel = (c.sel + 1) % len(c.options)
	}
	return nil
}

// click selects the previous option on a click on ‹ and the next one on
// a click on ›; the arrows only show while the choice has focus.
func (c *Choice) click(x int, wasFocused bool) {
	if !wasFocused || len(c.options) == 0 || x < 0 {
		return
	}
	switch x {
	case 0:
		c.sel = (c.sel - 1 + len(c.options)) % len(c.options)
	case lipgloss.Width(c.View()) - 1:
		c.sel = (c.sel + 1) % len(c.options)
	}
}

// View implements FormField: "‹ label ›" when focused, else the label.
func (c *Choice) View() string {
	l := c.Selected().Label
	if c.focused {
		return choiceArrowStyle.Render("‹") + " " + l + " " + choiceArrowStyle.Render("›")
	}
	return l
}
