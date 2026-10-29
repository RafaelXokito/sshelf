package components

import (
	tea "charm.land/bubbletea/v2"
)

// SecretField is a form field for a stored secret: a password, a pasted
// private key or a key passphrase (FR-IDENT-004, SEC-LOG-005). The stored
// value is never loaded into it. While it is empty it shows the secret's
// status, such as "(set)" or "(not set)"; what the user types or pastes
// replaces the secret on save and shows only as the fixed-length Mask. An
// empty field leaves the stored secret unchanged. There is no reveal and
// no copy.
//
// Use it by pointer; NewSecretField returns one.
type SecretField struct {
	in     *TextInput
	status string
}

// NewSecretField returns an empty field showing status, such as "set",
// "not set" or "not set on this device".
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
	f.in.Placeholder = "(" + status + ")"
}

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
	if f.Changed() {
		return "changed"
	}
	return ""
}

// Focus implements FormField.
func (f *SecretField) Focus() { f.in.Focus() }

// Blur implements FormField.
func (f *SecretField) Blur() { f.in.Blur() }

// Update implements FormField.
func (f *SecretField) Update(msg tea.Msg) tea.Cmd { return f.in.Update(msg) }

// View implements FormField: the status while empty, else Mask.
func (f *SecretField) View() string { return f.in.View() }
