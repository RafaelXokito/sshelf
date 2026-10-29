package ui

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/confirm"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
)

// DeleteIdentity mimics app.App.DeleteIdentity in memory.
func (f *fakeService) DeleteIdentity(_ context.Context, id domain.ID) (app.DeletedIdentity, error) {
	f.checkGoroutine("DeleteIdentity")
	f.mu.Lock()
	defer f.mu.Unlock()
	i := slices.IndexFunc(f.identities, func(o domain.Identity) bool { return o.ID == id })
	if i < 0 {
		return app.DeletedIdentity{}, app.ErrIdentityNotFound
	}
	f.identities = slices.Delete(slices.Clone(f.identities), i, i+1)
	f.hosts = slices.Clone(f.hosts)
	for j := range f.hosts {
		if f.hosts[j].IdentityID == id {
			f.hosts[j].IdentityID = ""
		}
	}
	return app.DeletedIdentity{
		Hosts:      slices.Clone(f.hosts),
		Identities: slices.Clone(f.identities),
		List:       f.identityList(),
		Warnings:   f.idDeleteWarnings,
	}, nil
}

func TestFR_IDENT_001_DeleteStatesLinkedCount(t *testing.T) {
	svc := identityService()
	d := newDriver(t, svc)
	d.press('j', "j") // web-prod, which uses ops
	d.press('i', "i")
	d.press('d', "d")
	if _, ok := d.root().modals[0].(confirm.Model); !ok {
		t.Fatalf("no confirm:\n%s", d.view())
	}
	v := d.view()
	if !strings.Contains(v, "Delete identity ops and its secrets? 2 linked hosts will be unlinked.") {
		t.Fatalf("view:\n%s", v)
	}
	golden(t, "identity-delete", d.m)
	d.press(tea.KeyEnter, "") // the default is No
	if len(svc.identities) != 3 || len(d.root().modals) != 0 {
		t.Fatalf("enter deleted: %+v", svc.identities)
	}
	svc.idDeleteWarnings = []string{`could not delete "sshelf" / "i1/password" from Keychain (access denied); remove it with the OS tools`}
	d.press('d', "d")
	d.press('y', "y")
	v = d.view()
	if slices.ContainsFunc(svc.identities, func(i domain.Identity) bool { return i.ID == "i1" }) {
		t.Fatal("y did not delete")
	}
	if !strings.Contains(v, "ops: deleted") || !strings.Contains(v, `"sshelf" / "i1/password"`) || strings.Contains(v, "> ops") {
		t.Fatalf("view:\n%s", v)
	}
	// Back on the host list, the hosts are unlinked and the selection kept.
	d.esc()
	l, ok := d.root().screen.(hostlist.Model)
	if !ok {
		t.Fatalf("not on the host list:\n%s", d.view())
	}
	for _, h := range l.Hosts() {
		if h.IdentityID == "i1" {
			t.Fatalf("%s still uses ops", h.Label)
		}
	}
	if h := selected(t, d); h.ID != "h2" {
		t.Fatalf("selected %s", h.ID)
	}
}

func TestSEC_CRED_004_ClearInForm(t *testing.T) {
	svc := identityService()
	d := newDriver(t, svc)
	d.press('i', "i")
	d.press('e', "e") // ops: password set
	d.tab()
	d.tab()
	d.key('x', tea.ModCtrl)
	if l := lineWith(ansiSeq.ReplaceAllString(d.view(), ""), "Password"); !strings.Contains(l, "(will be cleared)") {
		t.Fatalf("password line %q", l)
	}
	// ctrl+x again keeps it; on an unset field it does nothing.
	d.key('x', tea.ModCtrl)
	if l := lineWith(ansiSeq.ReplaceAllString(d.view(), ""), "Password"); !strings.Contains(l, "(set)") {
		t.Fatalf("password line %q", l)
	}
	d.key('x', tea.ModCtrl)
	d.tab()
	d.tab()
	d.key('x', tea.ModCtrl)
	if l := lineWith(ansiSeq.ReplaceAllString(d.view(), ""), "Passphrase"); !strings.Contains(l, "(not set)") {
		t.Fatalf("passphrase line %q", l)
	}
	d.save()
	if len(svc.idSaves) != 1 || !svc.idSaves[0].ClearPassword || svc.idSaves[0].ClearPassphrase {
		t.Fatalf("saves = %+v", svc.idSaves)
	}
}
