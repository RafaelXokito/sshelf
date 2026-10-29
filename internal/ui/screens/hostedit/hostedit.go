package hostedit

import (
	"errors"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/confirm"
)

// FieldWidth is the width of the text fields, in columns.
const FieldWidth = 28

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// The rows of the form, in order.
const (
	rowLabel = iota
	rowHostname
	rowPort
	rowUser
	rowIdentity
	rowPassword
	rowKeyPath
	rowKey
	rowPassphrase
)

// MsgCredsNeedNone is shown on the identity row when credentials are typed
// below while a shared identity is chosen.
const MsgCredsNeedNone = "Choose (none) to use the credentials below"

// maxKeyPaste is how many runes the stored-key field takes: well over the
// 2800-byte limit, so an oversized key gets the "reference the key file"
// message instead of being cut (SEC-CRED-003).
const maxKeyPaste = 16 << 10

var rowOf = map[domain.Field]int{
	domain.FieldLabel:    rowLabel,
	domain.FieldHostname: rowHostname,
	domain.FieldPort:     rowPort,
	domain.FieldIdentity: rowIdentity,
	domain.FieldKeyPath:  rowKeyPath,
}

// SaveMsg asks the root model to save Host, with the credentials typed in
// the form, through app.Service. Host.ID is "" for a new host. The
// secrets in Creds print as [REDACTED]; the service destroys them.
type SaveMsg struct {
	Host  domain.Host
	Creds app.HostCredentials
}

// CancelledMsg tells the root model the form closed without saving.
type CancelledMsg struct{}

// Model is the host form (FR-HOST-001): label, hostname, port, username,
// an identity picker and, for a host without a shared identity, its own
// password, key file, stored key and key passphrase (FR-HOST-010). Those
// are saved as the host's hidden identity, so a first-time user never has
// to create an identity; secret fields show only their status
// (FR-IDENT-004). Ctrl+S validates (FR-HOST-003) and, when every
// field is valid, sends SaveMsg; errors are shown next to their fields.
// Esc closes the form, after a confirm when there are unsaved changes.
type Model struct {
	host  domain.Host // as opened; ID "" when adding
	data  domain.Data // the other hosts and the identities, for validation
	form  *components.Form
	in    [rowIdentity]*components.TextInput
	ident *components.Choice

	keyPath                   *components.TextInput
	password, key, passphrase *components.SecretField

	saving bool   // a SaveMsg is out; keys other than esc wait
	err    string // why the last save failed, when not a field error
}

// New returns the form for h (the zero Host to add one), validated against
// hosts and offering identities. Hidden identities are never offered: h's
// own hidden identity shows as "(none)" with its credentials below.
func New(h domain.Host, hosts []domain.Host, identities []domain.Identity) Model {
	m := Model{host: h, data: domain.Data{Hosts: hosts, Identities: identities}, form: components.NewForm()}
	m.form.FieldWidth = FieldWidth
	for i := range m.in {
		m.in[i] = components.NewTextInput()
		m.in[i].Width = FieldWidth
	}
	m.in[rowLabel].CharLimit = domain.MaxLabelLen * 2 // longer labels get an error, not truncation
	m.in[rowLabel].SetValue(h.Label)
	m.in[rowHostname].CharLimit = 255
	m.in[rowHostname].Placeholder = "name, IPv4 or IPv6"
	m.in[rowHostname].SetValue(h.Hostname)
	m.in[rowPort].CharLimit = 5
	m.in[rowPort].Placeholder = strconv.Itoa(domain.DefaultPort)
	if h.Port != 0 {
		m.in[rowPort].SetValue(strconv.Itoa(h.Port))
	}
	m.in[rowUser].CharLimit = 255
	m.in[rowUser].Placeholder = "optional"
	m.in[rowUser].SetValue(h.User)

	var hidden domain.Identity
	opts := []components.Option{{Label: "(none)"}}
	for _, id := range identities {
		if id.Hidden {
			if id.ID == h.IdentityID {
				hidden = id
			}
			continue
		}
		opts = append(opts, components.Option{Label: domain.Sanitize(id.Label), Value: string(id.ID)})
	}
	m.ident = components.NewChoice(opts...)
	m.ident.Select(string(h.IdentityID)) // a dangling or hidden ID shows, and saves, as "(none)"

	status := func(has bool) string {
		if has {
			return "set"
		}
		return "not set"
	}
	secret := func(has bool) *components.SecretField {
		f := components.NewSecretField(status(has))
		f.Input().Width = FieldWidth
		return f
	}
	m.password = secret(hidden.HasPassword)
	m.keyPath = components.NewTextInput()
	m.keyPath.Width = FieldWidth
	m.keyPath.CharLimit = 4096
	m.keyPath.Placeholder = "optional"
	m.keyPath.SetValue(hidden.KeyPath)
	m.key = secret(hidden.HasKey)
	m.key.Input().Newlines = true
	m.key.Input().CharLimit = maxKeyPaste
	m.passphrase = secret(hidden.HasPassphrase)

	m.form.Add("Label", m.in[rowLabel]).
		Add("Hostname", m.in[rowHostname]).
		Add("Port", m.in[rowPort]).
		Add("Username", m.in[rowUser]).
		Add("Identity", m.ident).
		Add("Password", m.password).
		Add("Key file", m.keyPath).
		Add("Stored key", m.key).
		Add("Passphrase", m.passphrase)
	m.form.MarkClean()
	return m
}

// WithoutStore returns the form with its secret fields disabled,
// explaining that store is not available (ARCH-SEC-006, D-101). A key
// file and the username still save.
func (m Model) WithoutStore(store string) Model {
	reason := "disabled: no " + store
	m.password.Disable(reason)
	m.key.Disable(reason)
	m.passphrase.Disable(reason)
	return m
}

// Cancel implements components.FormScreen: Ctrl+C closes the form without
// asking (FR-NAV-003).
func (m Model) Cancel() tea.Msg { return CancelledMsg{} }

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

// submit validates the form and sends SaveMsg when it is valid.
func (m Model) submit() (components.Screen, tea.Cmd) {
	m.form.ClearErrors()
	m.err = ""
	h := m.host
	h.Label = strings.TrimSpace(m.in[rowLabel].Value())
	h.Hostname = strings.TrimSpace(m.in[rowHostname].Value())
	h.User = strings.TrimSpace(m.in[rowUser].Value())
	h.IdentityID = domain.ID(m.ident.Value())
	port, portErr := domain.ParsePort(m.in[rowPort].Value())
	h.Port = port

	if err := domain.ValidateHost(h, m.data); err != nil {
		m.showErrors(err)
	}
	if portErr != nil {
		m.showErrors(portErr)
	}
	creds := m.creds()
	if h.IdentityID != "" && (strings.TrimSpace(creds.KeyPath) != "" || m.password.Changed() || m.key.Changed() || m.passphrase.Changed()) {
		m.form.SetError(rowIdentity, MsgCredsNeedNone)
	}
	if m.form.FocusFirstError() {
		return m, nil
	}
	m.saving = true
	c := app.HostCredentials{
		KeyPath:         creds.KeyPath,
		Password:        app.NewSecret(m.password.Bytes()),
		Key:             app.NewSecret(m.key.Bytes()),
		Passphrase:      app.NewSecret(m.passphrase.Bytes()),
		ClearPassword:   m.password.Cleared(),
		ClearKey:        m.key.Cleared(),
		ClearPassphrase: m.passphrase.Cleared(),
	}
	return m, func() tea.Msg { return SaveMsg{Host: h, Creds: c} }
}

// creds returns the non-secret part of the credentials typed.
func (m Model) creds() app.HostCredentials {
	return app.HostCredentials{KeyPath: strings.TrimSpace(m.keyPath.Value())}
}

// Destroy zeroes what was typed into the secret fields; the root model
// calls it when it drops the form.
func (m Model) Destroy() {
	m.password.Reset()
	m.key.Reset()
	m.passphrase.Reset()
}

func (m Model) showErrors(err error) {
	if ve, ok := errors.AsType[*domain.ValidationError](err); ok {
		for f, text := range ve.Fields {
			if row, ok := rowOf[f]; ok {
				m.form.SetError(row, text)
			}
		}
		// A key problem's message is too long for the row: it goes
		// under the form (see SaveFailed).
		if _, ok := ve.Fields[domain.FieldKey]; ok {
			m.form.SetError(rowKey, "see below")
		}
	}
}

// SaveFailed returns the form after the save it sent failed: field
// errors go next to their fields, anything else shows under the form. The
// form stays open with what was typed.
func (m Model) SaveFailed(err error) Model {
	m.saving = false
	if ve, ok := errors.AsType[*domain.ValidationError](err); ok {
		m.showErrors(err)
		m.err = domain.Sanitize(ve.Fields[domain.FieldKey])
		m.form.FocusFirstError()
		return m
	}
	m.err = domain.Sanitize(components.ErrorText(err))
	return m
}

// View implements components.Screen.
func (m Model) View() string {
	title := "Add host"
	if m.host.ID != "" {
		title = "Edit host " + domain.Sanitize(m.host.Label)
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
	help := components.Help(keys.NextField, keys.PrevField, keys.Save, keys.Cancel)
	switch m.form.Focused() {
	case rowIdentity:
		help = components.Help(keys.ChoicePrev, keys.ChoiceNext, keys.NextField, keys.Save, keys.Cancel)
	case rowPassword, rowKey, rowPassphrase:
		help = components.Help(keys.NextField, keys.Save, keys.Cancel, keys.ClearSecret)
	}
	b.WriteString(help + "\n")
	return b.String()
}
