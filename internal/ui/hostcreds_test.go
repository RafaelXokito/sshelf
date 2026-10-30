package ui

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostedit"
)

// SaveHostWithCredentials mimics app.App.SaveHostWithCredentials in
// memory: typed credentials with no identity chosen become a hidden
// identity; choosing a shared one drops it.
func (f *fakeService) SaveHostWithCredentials(ctx context.Context, h domain.Host, c app.HostCredentials) (app.SavedHost, error) {
	defer c.Destroy()
	f.checkGoroutine("SaveHostWithCredentials")
	f.mu.Lock()
	var hidden *domain.Identity
	if i := slices.IndexFunc(f.hosts, func(o domain.Host) bool { return o.ID == h.ID && h.ID != "" }); i >= 0 {
		if j := slices.IndexFunc(f.identities, func(o domain.Identity) bool { return o.ID == f.hosts[i].IdentityID && o.Hidden }); j >= 0 {
			id := f.identities[j]
			hidden = &id
		}
	}
	typed := c.KeyPath != "" || c.Password.Len() > 0 || c.Key.Len() > 0 || c.Passphrase.Len() > 0
	if c.CopyOf != "" {
		// A duplicate (FR-HOST-011): the source's hidden identity is
		// copied, flags and all, under a new ID.
		f.copyOf = append(f.copyOf, c.CopyOf)
		if i := slices.IndexFunc(f.hosts, func(o domain.Host) bool { return o.ID == c.CopyOf }); i >= 0 && h.IdentityID == "" {
			if j := slices.IndexFunc(f.identities, func(o domain.Identity) bool { return o.ID == f.hosts[i].IdentityID && o.Hidden }); j >= 0 {
				id := f.identities[j]
				id.ID, id.Label = domain.NewID(), h.Label
				f.identities = append(slices.Clone(f.identities), id)
				h.IdentityID = id.ID
			}
		}
	}
	switch {
	case h.IdentityID != "" && hidden != nil && h.IdentityID != hidden.ID:
		f.identities = slices.DeleteFunc(slices.Clone(f.identities), func(o domain.Identity) bool { return o.ID == hidden.ID })
	case (h.IdentityID == "" || hidden != nil && h.IdentityID == hidden.ID) && (hidden != nil || typed):
		id := domain.Identity{Meta: domain.Meta{ID: domain.NewID()}, Label: h.Label, Hidden: true}
		if hidden != nil {
			id = *hidden
		}
		if f.idSecrets == nil {
			f.idSecrets = map[string][]byte{}
		}
		if c.Password.Len() > 0 {
			id.HasPassword = true
			f.idSecrets[string(id.ID)+"/password"] = bytes.Clone(c.Password.Bytes())
		}
		if c.ClearPassword && c.Password.Len() == 0 {
			id.HasPassword = false
		}
		if c.KeyPath != "" {
			id.KeyPath = c.KeyPath
		}
		f.identities = slices.DeleteFunc(slices.Clone(f.identities), func(o domain.Identity) bool { return o.ID == id.ID })
		f.identities = append(f.identities, id)
		h.IdentityID = id.ID
	}
	f.mu.Unlock()
	saved, err := f.SaveHost(ctx, h)
	f.mu.Lock()
	saved.Identities = slices.Clone(f.identities)
	f.mu.Unlock()
	return saved, err
}

// tabTo moves the host form's focus forward n fields.
func (d *driver) tabTo(n int) {
	for range n {
		d.tab()
	}
}

func TestFR_HOST_010_FirstTimeUserPasswordInForm(t *testing.T) {
	const canary = "canary-host-pw-1d2c"
	svc := &fakeService{} // first run: Welcome
	d := newDriver(t, svc)
	d.press('a', "a")
	d.typeText("web")
	d.tab()
	d.typeText("10.0.0.10")
	d.tab()
	d.tab()
	d.typeText("deploy")
	d.tabTo(2) // past the identity picker, which stays "(none)"
	d.typeText(canary)
	v := ansiSeq.ReplaceAllString(d.view(), "")
	if strings.Contains(v, canary) || !strings.Contains(lineWith(v, "Password"), "••••••••") {
		t.Fatalf("view:\n%s", v)
	}
	golden(t, "hostedit-credentials", d.m)
	d.save()
	if d.inForm() {
		t.Fatalf("not saved:\n%s", d.view())
	}
	if len(svc.identities) != 1 || !svc.identities[0].Hidden || !svc.identities[0].HasPassword {
		t.Fatalf("identities = %+v", svc.identities)
	}
	if got := string(svc.idSecrets[string(svc.identities[0].ID)+"/password"]); got != canary {
		t.Fatal("the password did not reach the service")
	}
	h := selected(t, d)
	if h.IdentityID != svc.identities[0].ID || h.User != "deploy" {
		t.Fatalf("host %+v", h)
	}
	// Connect without ever opening the identities view.
	d.press(tea.KeyEnter, "")
	if len(svc.connected) != 1 || svc.connected[0] != h.ID {
		t.Fatalf("connected %v", svc.connected)
	}
}

func TestFR_HOST_010_HiddenNotListedOrOffered(t *testing.T) {
	svc := identityService() // i2 is zeta's hidden identity
	d := newDriver(t, svc)
	d.press('i', "i")
	if v := d.view(); strings.Contains(v, "zeta") {
		t.Fatalf("hidden identity listed:\n%s", v)
	}
	d.esc()
	m := hostedit.New(svc.hosts[2], svc.hosts, svc.identities)
	v := ansiSeq.ReplaceAllString(m.View(), "")
	if !strings.Contains(lineWith(v, "Identity"), "(none)") || !strings.Contains(lineWith(v, "Password"), "(set)") {
		t.Fatalf("form of a host with a hidden identity:\n%s", v)
	}
}

func TestFR_HOST_010_CredentialsNeedNoIdentity(t *testing.T) {
	svc := identityService()
	d := newDriver(t, svc)
	d.press('a', "a")
	d.typeText("api")
	d.tab()
	d.typeText("h")
	d.tabTo(3)
	d.key(tea.KeyRight, 0) // a shared identity
	d.tab()
	d.typeText("pw")
	d.save()
	v := ansiSeq.ReplaceAllString(d.view(), "")
	if !d.inForm() || !strings.Contains(lineWith(v, "Identity"), hostedit.MsgCredsNeedNone) {
		t.Fatalf("view:\n%s", v)
	}
}
