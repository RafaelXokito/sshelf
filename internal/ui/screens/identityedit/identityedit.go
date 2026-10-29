package identityedit

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/confirm"
)

// FieldWidth is the width of the fields, in columns.
const FieldWidth = 28

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// The rows of the form, in order.
const (
	rowName = iota
	rowUser
	rowPassword
	rowKeyPath
	rowPassphrase
)

// SaveMsg asks the root model to save Input through app.Service. Its
// secrets print as [REDACTED]; the service destroys them.
type SaveMsg struct{ Input app.IdentityInput }

// CancelledMsg tells the root model the form closed without saving.
type CancelledMsg struct{}

// Model is the identity form (FR-IDENT-001): name, username, password,
// key file and key passphrase. Secret fields never show a stored value,
// only its status (FR-IDENT-004); typing replaces it on save, and leaving
// one empty keeps it. Ctrl+S validates and sends SaveMsg; Esc closes,
// after a confirm when there are unsaved changes.
type Model struct {
	info       app.IdentityInfo // as opened; ID "" when adding
	identities []domain.Identity
	form       *components.Form
	name, user *components.TextInput
	keyPath    *components.TextInput
	password   *components.SecretField
	passphrase *components.SecretField

	saving bool
	err    string // why the last save failed, when not a field error
}

// New returns the form for info (the zero IdentityInfo to add one),
// checking the name against identities.
func New(info app.IdentityInfo, identities []domain.Identity) Model {
	id := info.Identity
	m := Model{info: info, identities: identities, form: components.NewForm()}
	m.form.FieldWidth = FieldWidth
	text := func(v, placeholder string) *components.TextInput {
		in := components.NewTextInput()
		in.Width = FieldWidth
		in.CharLimit = 255
		in.Placeholder = placeholder
		in.SetValue(v)
		return in
	}
	secret := func(s app.SecretStatus) *components.SecretField {
		f := components.NewSecretField(s.String())
		f.Input().Width = FieldWidth
		return f
	}
	m.name = text(id.Label, "")
	m.name.CharLimit = domain.MaxLabelLen * 2 // longer names get an error, not truncation
	m.user = text(id.Username, "optional")
	m.keyPath = text(id.KeyPath, "optional")
	m.keyPath.CharLimit = 4096
	m.password = secret(info.Password)
	m.passphrase = secret(info.Passphrase)

	m.form.Add("Name", m.name).
		Add("Username", m.user).
		Add("Password", m.password).
		Add("Key file", m.keyPath).
		Add("Passphrase", m.passphrase)
	m.form.MarkClean()
	return m
}

// Cancel implements components.FormScreen: Ctrl+C closes the form without
// asking (FR-NAV-003).
func (m Model) Cancel() tea.Msg { return CancelledMsg{} }

// Destroy zeroes what was typed into the secret fields; the root model
// calls it when it drops the form.
func (m Model) Destroy() {
	m.password.Reset()
	m.passphrase.Reset()
}

// Update implements components.Screen.
func (m Model) Update(msg tea.Msg) (components.Screen, tea.Cmd) {
	switch msg.(type) {
	case tea.KeyPressMsg, tea.PasteMsg:
	default:
		return m, nil
	}
	ev, cmd := m.form.Update(msg)
	switch ev {
	case components.FormCancel:
		if m.form.Dirty() {
			return m, func() tea.Msg {
				return components.ShowModalMsg{Modal: confirm.New("Discard unsaved changes?", CancelledMsg{})}
			}
		}
		return m, func() tea.Msg { return CancelledMsg{} }
	case components.FormSubmit:
		if m.saving {
			return m, nil
		}
		return m.submit()
	}
	return m, cmd
}

// submit validates the name and sends SaveMsg.
func (m Model) submit() (components.Screen, tea.Cmd) {
	m.form.ClearErrors()
	m.err = ""
	id := m.info.Identity
	id.Label = strings.TrimSpace(m.name.Value())
	if err := domain.ValidateIdentity(id, domain.Data{Identities: m.identities}); err != nil {
		m.showErrors(err)
	}
	if m.form.FocusFirstError() {
		return m, nil
	}
	m.saving = true
	in := app.IdentityInput{
		ID:         id.ID,
		Name:       id.Label,
		Username:   m.user.Value(),
		KeyPath:    m.keyPath.Value(),
		Password:   app.NewSecret(m.password.Bytes()),
		Passphrase: app.NewSecret(m.passphrase.Bytes()),
	}
	return m, func() tea.Msg { return SaveMsg{Input: in} }
}

func (m Model) showErrors(err error) {
	if ve, ok := errors.AsType[*domain.ValidationError](err); ok {
		if text, ok := ve.Fields[domain.FieldName]; ok {
			m.form.SetError(rowName, text)
		}
	}
}

// SaveFailed returns the form after the save it sent failed: a name error
// goes next to the name, anything else shows under the form. What was
// typed stays, so the user can retry.
func (m Model) SaveFailed(err error) Model {
	m.saving = false
	if _, ok := errors.AsType[*domain.ValidationError](err); ok {
		m.showErrors(err)
		m.form.FocusFirstError()
		return m
	}
	m.err = domain.Sanitize(err.Error())
	return m
}

// View implements components.Screen.
func (m Model) View() string {
	title := "Add identity"
	if m.info.Identity.ID != "" {
		title = "Edit identity " + domain.Sanitize(m.info.Identity.Label)
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render(title) + "\n\n")
	b.WriteString(m.form.View())
	b.WriteString("\n")
	switch {
	case m.saving:
		b.WriteString(dimStyle.Render("Saving…") + "\n")
	case m.err != "":
		b.WriteString(errStyle.Render(m.err) + "\n")
	}
	b.WriteString(components.Help(keys.NextField, keys.PrevField, keys.Save, keys.Cancel) + "\n")
	return b.String()
}
