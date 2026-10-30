package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
)

// duplicateOf selects the host labelled label and presses c.
func (d *driver) duplicateOf(t *testing.T, label string) {
	t.Helper()
	i := slices.IndexFunc(d.list().Shown(), func(h domain.Host) bool { return h.Label == label })
	if i < 0 {
		t.Fatalf("no host %q", label)
	}
	d.press('g', "g")
	for range i {
		d.press('j', "j")
	}
	d.press('c', "c")
	if !d.inForm() {
		t.Fatalf("c did not open the form:\n%s", d.view())
	}
}

func TestFR_HOST_004_KeyOpensPrefilledForm(t *testing.T) {
	svc := identityService() // zeta has hidden identity i2 with a password; web-prod uses shared i1 "ops"
	d := newDriver(t, svc)
	d.duplicateOf(t, "zeta")
	v := plain(d.view())
	for _, want := range []struct{ line, text string }{
		{"Duplicate host", "Duplicate host zeta"},
		{"Label", "zeta Copy"},
		{"Hostname", "10.0.0.30"},
		{"Identity", "(none)"},
		{"Password", "(set)"},
		{"Passphrase", "(not set)"},
	} {
		if !strings.Contains(lineWith(v, want.line), want.text) {
			t.Errorf("%s line %q, want %q", want.line, lineWith(v, want.line), want.text)
		}
	}
	d.esc()

	// A shared identity stays selected.
	d.duplicateOf(t, "web-prod")
	if v := plain(d.view()); !strings.Contains(lineWith(v, "Label"), "web-prod Copy") || !strings.Contains(lineWith(v, "Identity"), "ops") {
		t.Fatalf("duplicate of web-prod:\n%s", v)
	}
	d.esc()
	if len(svc.saves) != 0 {
		t.Fatalf("opening the forms saved %v", svc.saves)
	}

	// The label is unique case-insensitively.
	svc = identityService()
	svc.hosts = append(svc.hosts, domain.Host{Meta: domain.Meta{ID: "h9"}, Label: "WEB-PROD copy", Hostname: "x"})
	d = newDriver(t, svc)
	d.duplicateOf(t, "web-prod")
	if v := plain(d.view()); !strings.Contains(lineWith(v, "Label"), "web-prod Copy 2") {
		t.Fatalf("label with web-prod Copy taken:\n%s", v)
	}
}

func TestFR_HOST_004_CancelCreatesNothing(t *testing.T) {
	svc := identityService()
	ids := slices.Clone(svc.identities)
	d := newDriver(t, svc)
	for _, cancel := range []struct {
		name string
		do   func()
	}{
		{"esc", d.esc},
		{"ctrl+c", func() { d.key('c', tea.ModCtrl) }},
		{"[ Cancel ]", func() { d.tabTo(hostFormFields + 1); d.space() }},
	} {
		d.duplicateOf(t, "zeta")
		cancel.do()
		if d.inForm() || len(d.root().modals) != 0 || d.quit {
			t.Fatalf("%s did not close the form:\n%s", cancel.name, d.view())
		}
		if len(svc.saves) != 0 || len(svc.copyOf) != 0 || len(svc.hosts) != 3 || !slices.Equal(svc.identities, ids) || len(svc.idSecrets) != 0 {
			t.Fatalf("%s created something: saves %v, hosts %d, identities %+v", cancel.name, svc.saves, len(svc.hosts), svc.identities)
		}
		if h := selected(t, d); h.Label != "zeta" {
			t.Fatalf("%s lost the selection: %s", cancel.name, h.Label)
		}
	}
}

func TestFR_HOST_004_SaveSelectsCopy(t *testing.T) {
	svc := identityService()
	d := newDriver(t, svc)
	d.duplicateOf(t, "zeta")
	d.save()
	if d.inForm() {
		t.Fatalf("still in the form:\n%s", d.view())
	}
	h := selected(t, d)
	if h.Label != "zeta Copy" || h.Hostname != "10.0.0.30" || !slices.Equal(svc.copyOf, []domain.ID{"h3"}) {
		t.Fatalf("selected %+v, copyOf %v", h, svc.copyOf)
	}
	if !strings.Contains(plain(d.view()), "zeta Copy: saved") {
		t.Fatalf("no saved status:\n%s", d.view())
	}
	// The copy has a hidden identity of its own; the source keeps i2.
	i := slices.IndexFunc(svc.identities, func(o domain.Identity) bool { return o.ID == h.IdentityID })
	if h.IdentityID == "" || h.IdentityID == "i2" || i < 0 || !svc.identities[i].Hidden || !svc.identities[i].HasPassword {
		t.Fatalf("copy's identity %q in %+v", h.IdentityID, svc.identities)
	}
	if src := svc.hosts[slices.IndexFunc(svc.hosts, func(o domain.Host) bool { return o.ID == "h3" })]; src.IdentityID != "i2" {
		t.Fatalf("source changed: %+v", src)
	}
}

func TestFR_HOST_004_FooterAndKeymap(t *testing.T) {
	d := newDriver(t, formService())
	d.resize(80, 24)
	foot := plain(d.root().footer())
	for _, want := range []string{"enter connect", "/ search", "a add", "c duplicate", "e edit", "d delete", "q quit"} {
		if !strings.Contains(foot, want) {
			t.Errorf("footer %q lacks %q", foot, want)
		}
	}
	for _, m := range keys.Maps() {
		if m.Name == "host list" && !slices.ContainsFunc(m.Bindings, func(b key.Binding) bool { return slices.Contains(b.Keys(), "c") }) {
			t.Error("c is not in the host list keymap")
		}
	}
	// While searching, c types into the query.
	d.press('/', "/")
	d.press('c', "c")
	if d.inForm() || d.list().Query() != "c" {
		t.Fatalf("c while searching: form %v, query %q", d.inForm(), d.list().Query())
	}
}

func TestFR_HOST_004_DuplicateGolden(t *testing.T) {
	d := newDriver(t, identityService())
	d.duplicateOf(t, "zeta")
	golden(t, "hostedit-duplicate", d.m)
}

func TestFR_HOST_011_StoreErrorShown(t *testing.T) {
	const msg = "could not copy the password: Access to Keychain was denied"
	svc := identityService()
	svc.saveErr = errors.New(msg)
	d := newDriver(t, svc)
	d.duplicateOf(t, "zeta")
	d.save()
	if !d.inForm() || !strings.Contains(plain(d.view()), msg) {
		t.Fatalf("store error not shown in the form:\n%s", d.view())
	}
	if len(svc.hosts) != 3 {
		t.Fatalf("hosts %+v", svc.hosts)
	}
}
