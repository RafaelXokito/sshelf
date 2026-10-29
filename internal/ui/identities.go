package ui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/identities"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/identityedit"
)

// IdentitiesMsg carries the identities view read by app.Service.
type IdentitiesMsg struct {
	List []app.IdentityInfo
	Err  error
}

// IdentitySavedMsg reports that an identity was saved.
type IdentitySavedMsg struct{ Saved app.SavedIdentity }

// IdentitySaveFailedMsg reports that an identity was not saved.
type IdentitySaveFailedMsg struct{ Err error }

// IdentityDeletedMsg reports that an identity was deleted.
type IdentityDeletedMsg struct {
	Label   string
	Deleted app.DeletedIdentity
}

// updateIdentities routes the identities view and the identity form
// (design §9.1: HostList → Identities → IdentityEdit). ok is false for
// messages it does not handle.
func (m Model) updateIdentities(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// i on the host list opens the identities (FR-NAV-003). The host
		// list's own keymap (T-016) can take this over by sending
		// identities.OpenMsg instead.
		if _, ok := m.screen.(hostlist.Model); !ok || len(m.modals) > 0 || !key.Matches(msg, keys.Identities) {
			return m, nil, false
		}
		m, cmd := m.openIdentities()
		return m, cmd, true

	case identities.OpenMsg:
		if _, ok := m.screen.(hostlist.Model); !ok {
			return m, nil, true
		}
		m, cmd := m.openIdentities()
		return m, cmd, true

	case IdentitiesMsg:
		if v, ok := m.screen.(identities.Model); ok && v.IsLoading() {
			if msg.Err != nil {
				m.status = "identities: " + domain.Sanitize(msg.Err.Error())
				m.screen = m.list
				return m, nil, true
			}
			m.screen = identities.New(msg.List)
		}
		return m, nil, true

	case identities.BackMsg:
		if _, ok := m.screen.(identities.Model); ok {
			m.status = ""
			m.screen = m.list
		}
		return m, nil, true

	case identities.AddMsg:
		return m.openIdentityForm(app.IdentityInfo{}), nil, true

	case identities.EditMsg:
		return m.openIdentityForm(msg.Info), nil, true

	case identityedit.SaveMsg:
		svc, in := m.svc, msg.Input
		return m, func() tea.Msg {
			saved, err := svc.SaveIdentity(context.Background(), in)
			if err != nil {
				return IdentitySaveFailedMsg{Err: err}
			}
			return IdentitySavedMsg{Saved: saved}
		}, true

	case IdentitySavedMsg:
		m.identities = msg.Saved.Identities
		if f, ok := m.screen.(identityedit.Model); ok {
			f.Destroy()
		}
		m.modals = nil
		id := msg.Saved.Identity
		m.screen = identities.New(msg.Saved.List).Select(id.ID)
		m.status = domain.Sanitize(strings.Join(append([]string{id.Label + ": saved"}, msg.Saved.Warnings...), "; "))
		return m, nil, true

	case IdentitySaveFailedMsg:
		if f, ok := m.screen.(identityedit.Model); ok {
			m.screen = f.SaveFailed(msg.Err)
		} else {
			m.status = "not saved: " + domain.Sanitize(msg.Err.Error())
		}
		return m, nil, true

	case identities.DeleteMsg:
		svc, id, label := m.svc, msg.ID, msg.Label
		return m, func() tea.Msg {
			deleted, err := svc.DeleteIdentity(context.Background(), id)
			if err != nil {
				return IdentitySaveFailedMsg{Err: err}
			}
			return IdentityDeletedMsg{Label: label, Deleted: deleted}
		}, true

	case IdentityDeletedMsg:
		d := msg.Deleted
		m.identities = d.Identities
		// Hosts that used the identity are unlinked; keep the selection.
		l := hostlist.New(d.Hosts)
		if h, ok := m.list.Selected(); ok {
			l = l.Select(h.ID)
		}
		m.list = l
		if _, ok := m.screen.(identities.Model); ok {
			m.screen = identities.New(d.List)
		}
		m.status = domain.Sanitize(strings.Join(append([]string{msg.Label + ": deleted"}, d.Warnings...), "; "))
		return m, nil, true

	case identityedit.CancelledMsg:
		if f, ok := m.screen.(identityedit.Model); ok {
			f.Destroy()
			m.modals = nil
			m.screen = m.idView
		}
		return m, nil, true
	}
	return m, nil, false
}

// openIdentities shows the identities view, loading, and reads it.
func (m Model) openIdentities() (Model, tea.Cmd) {
	l, ok := m.screen.(hostlist.Model)
	if !ok {
		return m, nil
	}
	m.list = l
	m.status = ""
	m.screen = identities.Loading()
	svc := m.svc
	return m, func() tea.Msg {
		list, err := svc.Identities(context.Background())
		return IdentitiesMsg{List: list, Err: err}
	}
}

// openIdentityForm shows the identity form for info and keeps the view to
// return to.
func (m Model) openIdentityForm(info app.IdentityInfo) Model {
	v, ok := m.screen.(identities.Model)
	if !ok {
		return m
	}
	m.idView = v
	m.status = ""
	m.screen = identityedit.New(info, m.identities)
	return m
}
