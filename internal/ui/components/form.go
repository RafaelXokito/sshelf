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

// Form lays out labelled fields one per row, moves focus with tab and
// shift+tab (wrapping), and reports ctrl+s and esc as FormSubmit and
// FormCancel; every other key and paste goes to the focused field. Each
// row can show an error next to its field (FR-HOST-003). The form does not
// validate or save: the screen that owns it does.
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
	focus int
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

// Focused returns the index of the focused field.
func (f *Form) Focused() int { return f.focus }

// SetFocus focuses field i (ignored when out of range).
func (f *Form) SetFocus(i int) {
	if i < 0 || i >= len(f.rows) {
		return
	}
	f.rows[f.focus].field.Blur()
	f.focus = i
	f.rows[i].field.Focus()
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
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(k, keys.Save):
			return FormSubmit, nil
		case key.Matches(k, keys.Cancel):
			return FormCancel, nil
		case key.Matches(k, keys.NextField):
			f.SetFocus((f.focus + 1) % len(f.rows))
			return FormNone, nil
		case key.Matches(k, keys.PrevField):
			f.SetFocus((f.focus - 1 + len(f.rows)) % len(f.rows))
			return FormNone, nil
		}
	}
	return FormNone, f.rows[f.focus].field.Update(msg)
}

// View renders one row per field: a focus marker, the label padded to
// the widest label, the field, and its error if any.
func (f *Form) View() string {
	lw := 0
	for _, r := range f.rows {
		lw = max(lw, lipgloss.Width(r.label))
	}
	var b strings.Builder
	for i, r := range f.rows {
		label := pad(r.label, lw)
		if i == f.focus {
			b.WriteString(formFocusStyle.Render("> " + label))
		} else {
			b.WriteString("  " + formLabelStyle.Render(label))
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
	}
	return b.String()
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

// View implements FormField: "‹ label ›" when focused, else the label.
func (c *Choice) View() string {
	l := c.Selected().Label
	if c.focused {
		return choiceArrowStyle.Render("‹") + " " + l + " " + choiceArrowStyle.Render("›")
	}
	return l
}
