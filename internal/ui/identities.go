package ui

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostedit"
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

// IdentitySaveFailedMsg reports that an identity was not saved or deleted.
type IdentitySaveFailedMsg struct{ Err error }

// IdentityDeletedMsg reports that an identity was deleted.
type IdentityDeletedMsg struct {
	Label   string
	Deleted app.DeletedIdentity
}

// StoreStatusMsg carries the result of the startup probe of the
// credential store (credential-storage §7): nil, or a *app.StoreError when
// there is none.
type StoreStatusMsg struct{ Err error }

// probeStore is the startup probe, run off the Update goroutine
// (ARCH-UI-002).
func probeStore(svc app.Service) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		defer cancel()
		return StoreStatusMsg{Err: svc.SecretsAvailable(ctx)}
	}
}

// probeTimeout bounds the startup probe.
const probeTimeout = 10 * time.Second

// withBanner puts the one-line "no credential store" banner (D-101,
// ARCH-SEC-006) above the host list, the identities and the forms.
func (m Model) withBanner(body []string) []string {
	if m.noStore == nil {
		return body
	}
	switch m.screen.(type) {
	case hostlist.Model, identities.Model, identityedit.Model, hostedit.Model:
	default:
		return body
	}
	banner := bannerStyle.Render("! " + m.noStore.Explain())
	return append([]string{banner, ""}, body...)
}

var bannerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)

// updateIdentities routes the identities view and the identity form
// (design §9.1: HostList → Identities → IdentityEdit). ok is false for
// messages it does not handle.
func (m Model) updateIdentities(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case StoreStatusMsg:
		m.noStore = nil
		if se, ok := errors.AsType[*app.StoreError](msg.Err); ok {
			m.noStore = se
		}
		return m, nil, true

	case hostlist.IdentitiesMsg:
		// i on the host list (FR-NAV-003).
		m, cmd := m.openIdentities()
		return m, cmd, true

	case IdentitiesMsg:
		if v, ok := m.screen.(identities.Model); ok && v.IsLoading() {
			if msg.Err != nil {
				m.status = components.ErrorPrefix + "identities: " + domain.Sanitize(msg.Err.Error())
				m.screen = m.sized(m.prev)
				return m, nil, true
			}
			m.screen = m.sized(identities.New(msg.List))
		}
		return m, nil, true

	case identities.BackMsg:
		if _, ok := m.screen.(identities.Model); ok && m.prev != nil {
			m.status = ""
			m.screen = m.sized(m.prev)
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
		m.screen = m.sized(identities.New(msg.Saved.List).Select(id.ID))
		m.status = domain.Sanitize(strings.Join(append([]string{id.Label + ": saved"}, msg.Saved.Warnings...), "; "))
		return m, nil, true

	case IdentitySaveFailedMsg:
		if f, ok := m.screen.(identityedit.Model); ok {
			m.screen = f.SaveFailed(msg.Err)
		} else {
			m.status = components.ErrorPrefix + "not saved: " + domain.Sanitize(msg.Err.Error())
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
		prev := m.hostsScreen(d.Hosts)
		if old, ok := m.prev.(hostlist.Model); ok {
			if l, ok := prev.(hostlist.Model); ok {
				if h, ok := old.Selected(); ok {
					prev = l.Select(h.ID)
				}
			}
		}
		m.prev = prev
		if _, ok := m.screen.(identities.Model); ok {
			m.screen = m.sized(identities.New(d.List))
		}
		m.status = domain.Sanitize(strings.Join(append([]string{msg.Label + ": deleted"}, d.Warnings...), "; "))
		return m, nil, true

	case identityedit.CancelledMsg:
		if f, ok := m.screen.(identityedit.Model); ok {
			f.Destroy()
			m.modals = nil
			m.screen = m.sized(m.idView)
		}
		return m, nil, true
	}
	return m, nil, false
}

// openIdentities shows the identities view, loading, and reads it.
func (m Model) openIdentities() (Model, tea.Cmd) {
	if _, ok := m.screen.(hostlist.Model); !ok {
		return m, nil
	}
	m.prev = m.screen
	m.status = ""
	m.screen = m.sized(identities.Loading())
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
	f := identityedit.New(info, m.identities)
	if m.noStore != nil {
		f = f.WithoutStore(m.noStore.Store)
	}
	m.screen = m.sized(f)
	return m
}
