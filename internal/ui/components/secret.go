package components

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/ui/keys"
)

// SecretField is a form field for a stored secret: a password, a pasted
// private key or a key passphrase (FR-IDENT-004, SEC-LOG-005). The stored
// value is never loaded into it. While it is empty it shows the secret's
// status, such as "(set)" or "(not set)"; what the user types or pastes
// replaces the secret on save and shows only as the fixed-length Mask. An
// empty field leaves the stored secret unchanged, and ClearSecret
// (ctrl+x) marks a stored one to be deleted on save (SEC-CRED-004). There
// is no reveal and no copy.
//
// Use it by pointer; NewSecretField returns one.
type SecretField struct {
	in       *TextInput
	status   string
	canClear bool // something is stored
	clear    bool
	disabled string // why the field takes no input; "" when enabled
}

// Disable makes the field read-only and show reason instead of a status,
// for when no credential store is available (ARCH-SEC-006). Anything
// typed is zeroed.
func (f *SecretField) Disable(reason string) {
	f.in.Reset()
	f.clear = false
	f.disabled = reason
}

// Disabled reports whether the field is read-only.
func (f *SecretField) Disabled() bool { return f.disabled != "" }

// NewSecretField returns an empty field showing status, such as "set",
// "not set" or "not set on this device". Any status but "not set" can be
// cleared.
func NewSecretField(status string) *SecretField {
	in := NewTextInput()
	in.Echo = EchoMasked
	f := &SecretField{in: in}
	f.SetStatus(status)
	return f
}

// SetStatus changes the status shown while the field is empty.
func (f *SecretField) SetStatus(status string) {
	f.status = status
	f.canClear = status != "not set"
	f.clear = f.clear && f.canClear
	f.placeholder()
}

func (f *SecretField) placeholder() {
	if f.clear {
		f.in.Placeholder = "(will be cleared)"
		return
	}
	f.in.Placeholder = "(" + f.status + ")"
}

// Cleared reports whether the stored secret is to be deleted.
func (f *SecretField) Cleared() bool { return f.clear }

// Input returns the masked input, to set its Width, CharLimit or
// Newlines.
func (f *SecretField) Input() *TextInput { return f.in }

// Changed reports whether the user typed or pasted a new value.
func (f *SecretField) Changed() bool { return f.in.Len() > 0 }

// Bytes returns a copy of the new value, which the caller owns and must
// clear or hand on; nil when unchanged.
func (f *SecretField) Bytes() []byte {
	if !f.Changed() {
		return nil
	}
	return f.in.Bytes()
}

// Reset zeroes and empties the new value.
func (f *SecretField) Reset() { f.in.Reset() }

// Value implements FormField. It never returns the secret, only whether
// there is a new one, so Form.Dirty keeps no copy of it.
func (f *SecretField) Value() string {
	switch {
	case f.Changed():
		return "changed"
	case f.clear:
		return "cleared"
	}
	return ""
}

// Focus implements FormField.
func (f *SecretField) Focus() { f.in.Focus() }

// Blur implements FormField.
func (f *SecretField) Blur() { f.in.Blur() }

// Update implements FormField. ClearSecret toggles clearing; typing a new
// value undoes it.
func (f *SecretField) Update(msg tea.Msg) tea.Cmd {
	if f.disabled != "" {
		return nil
	}
	if k, ok := msg.(tea.KeyPressMsg); ok && key.Matches(k, keys.ClearSecret) {
		if f.in.Focused() && f.canClear {
			f.in.Reset()
			f.clear = !f.clear
			f.placeholder()
		}
		return nil
	}
	cmd := f.in.Update(msg)
	if f.clear && f.Changed() {
		f.clear = false
		f.placeholder()
	}
	return cmd
}

// click puts the cursor at the end of what was typed; a disabled field
// is only focused (FR-NAV-015).
func (f *SecretField) click(int, bool) { f.in.End() }

// View implements FormField: the status while empty, else Mask.
func (f *SecretField) View() string {
	if f.disabled != "" {
		v := placeholderStyle.Render("(" + f.disabled + ")")
		if f.in.Focused() {
			return "_" + v
		}
		return v
	}
	return f.in.View()
}
