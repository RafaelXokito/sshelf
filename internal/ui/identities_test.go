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
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/identities"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/identityedit"
)

// fakeIdentities is the identity half of fakeService. It keeps a copy of
// every secret it is given, so tests can check what reached the service.
type fakeIdentities struct {
	idMissing map[domain.ID]bool // passwords "not set on this device"
	idSaveErr error
	idSaves   []app.IdentityInput // metadata only; secrets are destroyed
	idSecrets map[string][]byte   // "<id>/<field>" → value

	idDeleteWarnings []string // returned by DeleteIdentity
}

func (f *fakeService) Identities(context.Context) ([]app.IdentityInfo, error) {
	f.checkGoroutine("Identities")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.identityList(), nil
}

// identityList mimics app.App.Identities. f.mu must be held.
func (f *fakeService) identityList() []app.IdentityInfo {
	status := func(has bool) app.SecretStatus {
		if has {
			return app.SecretSet
		}
		return app.SecretNotSet
	}
	var out []app.IdentityInfo
	for _, id := range f.identities {
		if id.Hidden {
			continue
		}
		info := app.IdentityInfo{Identity: id, Password: status(id.HasPassword), Key: status(id.HasKey), Passphrase: status(id.HasPassphrase)}
		if id.HasPassword && f.idMissing[id.ID] {
			info.Password = app.SecretNotOnDevice
		}
		for _, h := range f.hosts {
			if h.IdentityID == id.ID {
				info.LinkedHosts++
			}
		}
		out = append(out, info)
	}
	return out
}

// SaveIdentity mimics app.App.SaveIdentity in memory.
func (f *fakeService) SaveIdentity(_ context.Context, in app.IdentityInput) (app.SavedIdentity, error) {
	f.checkGoroutine("SaveIdentity")
	defer in.Destroy()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.idSecrets == nil {
		f.idSecrets = map[string][]byte{}
	}
	meta := in
	meta.Password, meta.Key, meta.Passphrase = app.Secret{}, app.Secret{}, app.Secret{}
	f.idSaves = append(f.idSaves, meta)
	if f.idSaveErr != nil {
		return app.SavedIdentity{}, f.idSaveErr
	}
	ids := slices.Clone(f.identities)
	var id domain.Identity
	i := slices.IndexFunc(ids, func(o domain.Identity) bool { return o.ID == in.ID && in.ID != "" })
	if i >= 0 {
		id = ids[i]
	} else {
		id.ID = domain.NewID()
	}
	id.Label, id.Username, id.KeyPath = strings.TrimSpace(in.Name), strings.TrimSpace(in.Username), strings.TrimSpace(in.KeyPath)
	if err := domain.ValidateIdentity(id, domain.Data{Identities: ids}); err != nil {
		return app.SavedIdentity{}, err
	}
	for field, s := range map[string]app.Secret{"password": in.Password, "private-key": in.Key, "key-passphrase": in.Passphrase} {
		if s.Len() == 0 {
			continue
		}
		f.idSecrets[string(id.ID)+"/"+field] = bytes.Clone(s.Bytes())
		switch field {
		case "password":
			id.HasPassword = true
		case "private-key":
			id.HasKey = true
		default:
			id.HasPassphrase = true
		}
	}
	if i >= 0 {
		ids[i] = id
	} else {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b domain.Identity) int {
		return strings.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label))
	})
	f.identities = ids
	return app.SavedIdentity{Identity: id, Identities: slices.Clone(ids), List: f.identityList()}, nil
}

var identityHosts = []domain.Host{
	{Meta: domain.Meta{ID: "h1"}, Label: "db-staging", Hostname: "10.0.1.20", IdentityID: "i1"},
	{Meta: domain.Meta{ID: "h2"}, Label: "web-prod", Hostname: "10.0.0.10", IdentityID: "i1"},
	{Meta: domain.Meta{ID: "h3"}, Label: "zeta", Hostname: "10.0.0.30", IdentityID: "i2"},
}

func identityService() *fakeService {
	return &fakeService{
		hosts: slices.Clone(identityHosts),
		identities: []domain.Identity{
			{Meta: domain.Meta{ID: "i2"}, Label: "zeta", Hidden: true, HasPassword: true},
			{Meta: domain.Meta{ID: "i1"}, Label: "ops", Username: "deploy", HasPassword: true, KeyPath: "~/.ssh/id_ed25519"},
			{Meta: domain.Meta{ID: "i4"}, Label: "synced", HasPassword: true},
		},
		fakeIdentities: fakeIdentities{idMissing: map[domain.ID]bool{"i4": true}},
		session:        &stubSession{},
	}
}

func (d *driver) onIdentities() (identities.Model, bool) {
	v, ok := d.root().screen.(identities.Model)
	return v, ok
}

func (d *driver) inIdentityForm() bool {
	_, ok := d.root().screen.(identityedit.Model)
	return ok
}

func TestFR_NAV_003_IOpensIdentities(t *testing.T) {
	d := newDriver(t, identityService())
	d.press('i', "i")
	v, ok := d.onIdentities()
	if !ok {
		t.Fatalf("i did not open the identities:\n%s", d.view())
	}
	view := d.view()
	if !strings.Contains(view, "Identities") || !strings.Contains(view, "> ops") || !strings.Contains(view, "2 hosts") {
		t.Fatalf("view:\n%s", view)
	}
	// The hidden per-host identity is not listed (FR-HOST-010).
	if len(v.List()) != 2 || strings.Contains(view, "zeta ") {
		t.Fatalf("listed %+v", v.List())
	}
	golden(t, "identities", d.m)
	d.esc()
	if _, ok := d.root().screen.(hostlist.Model); !ok {
		t.Fatalf("esc did not return to the host list:\n%s", d.view())
	}
	// The message the host list's keymap can send works too.
	d.send(hostlist.IdentitiesMsg{})
	if _, ok := d.onIdentities(); !ok {
		t.Fatal("IdentitiesMsg did not open the identities")
	}
}

func TestFR_IDENT_004_FormShowsSetNotSet(t *testing.T) {
	d := newDriver(t, identityService())
	d.press('i', "i")
	d.press('e', "e") // ops
	if !d.inIdentityForm() {
		t.Fatalf("e did not open the form:\n%s", d.view())
	}
	v := ansiSeq.ReplaceAllString(d.view(), "")
	if l := lineWith(v, "Password"); !strings.Contains(l, "(set)") {
		t.Fatalf("password line %q", l)
	}
	if l := lineWith(v, "Passphrase"); !strings.Contains(l, "(not set)") {
		t.Fatalf("passphrase line %q", l)
	}
	if !strings.Contains(lineWith(v, "Key file"), "~/.ssh/id_ed25519") {
		t.Fatalf("key file not shown:\n%s", v)
	}
	golden(t, "identityedit-edit", d.m)
	d.esc()

	d.press('j', "j") // synced: its password is not on this device
	d.press('e', "e")
	if l := lineWith(ansiSeq.ReplaceAllString(d.view(), ""), "Password"); !strings.Contains(l, "(not set on this device)") {
		t.Fatalf("password line %q", l)
	}
}

func TestSEC_LOG_005_GoldenHasNoCanaryOrBullets(t *testing.T) {
	const canary = "canary-pw-3b9d0" // not as long as the mask
	svc := identityService()
	d := newDriver(t, svc)
	d.press('i', "i")
	d.press('a', "a")
	d.typeText("db")
	d.tab()
	d.typeText("postgres")
	d.tab()
	d.typeText(canary)
	d.tab()
	d.tab()
	d.tab()
	d.send(tea.PasteMsg{Content: canary})
	v := ansiSeq.ReplaceAllString(d.view(), "")
	if strings.Contains(v, canary) || strings.Contains(v, strings.Repeat("•", len(canary))) {
		t.Fatalf("the view shows the secret or its length:\n%s", v)
	}
	if !strings.Contains(lineWith(v, "Password"), components.Mask) {
		t.Fatalf("no mask:\n%s", v)
	}
	golden(t, "identityedit-secret", d.m)
	d.save()
	if d.inIdentityForm() {
		t.Fatalf("not saved:\n%s", d.view())
	}
	saved := svc.identities[slices.IndexFunc(svc.identities, func(i domain.Identity) bool { return i.Label == "db" })]
	if got := string(svc.idSecrets[string(saved.ID)+"/password"]); got != canary {
		t.Fatal("the typed password did not reach the service")
	}
	if got := string(svc.idSecrets[string(saved.ID)+"/key-passphrase"]); got != canary {
		t.Fatal("the pasted passphrase did not reach the service")
	}
	v = d.view()
	if strings.Contains(v, canary) || !strings.Contains(v, "db: saved") || !strings.Contains(v, "> db") {
		t.Fatalf("after save:\n%s", v)
	}
}

func TestFR_CRED_001_NoMasterPasswordScreen(t *testing.T) {
	svc := identityService()
	d := newDriver(t, svc)
	var views []string
	step := func(f func()) { f(); views = append(views, strings.ToLower(d.view())) }
	step(func() { d.press('i', "i") })
	step(func() { d.press('a', "a") })
	step(func() { d.typeText("new") })
	step(func() { d.tab(); d.tab(); d.typeText("pw") })
	step(func() { d.save() })
	step(func() { d.esc() })
	for _, v := range views {
		for _, bad := range []string{"master password", "unlock", "vault"} {
			if strings.Contains(v, bad) {
				t.Fatalf("a view mentions %q:\n%s", bad, v)
			}
		}
	}
	if len(svc.idSaves) != 1 || svc.idSaves[0].Name != "new" {
		t.Fatalf("saves = %+v", svc.idSaves)
	}
	if _, ok := d.root().screen.(hostlist.Model); !ok {
		t.Fatalf("not back on the host list:\n%s", d.view())
	}
}

func TestFR_IDENT_001_FormNameErrors(t *testing.T) {
	svc := identityService()
	d := newDriver(t, svc)
	d.press('i', "i")
	d.press('a', "a")
	d.typeText("OPS")
	d.save()
	v := ansiSeq.ReplaceAllString(d.view(), "")
	if !d.inIdentityForm() || !strings.Contains(lineWith(v, "Name already in use"), "Name") || len(svc.idSaves) != 0 {
		t.Fatalf("view:\n%s", v)
	}
	// A hidden identity's name is free.
	d.clear()
	d.typeText("zeta")
	d.save()
	if d.inIdentityForm() {
		t.Fatalf("not saved:\n%s", d.view())
	}
	// Esc with changes asks; ctrl+c cancels without asking or quitting.
	d.press('a', "a")
	d.typeText("x")
	d.key('c', tea.ModCtrl)
	if d.quit || d.inIdentityForm() {
		t.Fatalf("ctrl+c: quit %v, form %v", d.quit, d.inIdentityForm())
	}
	if _, ok := d.onIdentities(); !ok {
		t.Fatal("ctrl+c did not return to the identities")
	}
}

func TestFR_IDENT_001_SaveFailureKeepsForm(t *testing.T) {
	svc := identityService()
	svc.idSaveErr = &app.StoreError{Store: "Secret Service", Err: app.ErrStoreDenied}
	d := newDriver(t, svc)
	d.press('i', "i")
	d.press('a', "a")
	d.typeText("db")
	d.save()
	if v := d.view(); !d.inIdentityForm() || !strings.Contains(v, "Access to Secret Service was denied") {
		t.Fatalf("view:\n%s", v)
	}
	svc.idSaveErr = nil
	d.save()
	if d.inIdentityForm() || len(svc.idSaves) != 2 {
		t.Fatalf("retry: %d saves", len(svc.idSaves))
	}
}
