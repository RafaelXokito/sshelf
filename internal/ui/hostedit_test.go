package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/confirm"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostedit"
)

var testIdentities = []domain.Identity{
	{Meta: domain.Meta{ID: "i1"}, Label: "deploy"},
	{Meta: domain.Meta{ID: "i2"}, Label: "per-host secret", Hidden: true},
}

func formService() *fakeService {
	return &fakeService{hosts: slices.Clone(testHosts), identities: testIdentities, session: &stubSession{}}
}

func (d *driver) typeText(s string) {
	for _, r := range s {
		d.press(r, string(r))
	}
}

func (d *driver) key(code rune, mod tea.KeyMod) { d.send(tea.KeyPressMsg{Code: code, Mod: mod}) }

func (d *driver) save()  { d.key('s', tea.ModCtrl) }
func (d *driver) tab()   { d.key(tea.KeyTab, 0) }
func (d *driver) esc()   { d.key(tea.KeyEscape, 0) }
func (d *driver) clear() { d.key('u', tea.ModCtrl) } // the cursor is at the end
func (d *driver) inForm() bool {
	_, ok := d.root().screen.(hostedit.Model)
	return ok
}

// lineWith returns the view's line containing s.
func lineWith(view, s string) string {
	for l := range strings.SplitSeq(view, "\n") {
		if strings.Contains(l, s) {
			return l
		}
	}
	return ""
}

func TestFR_HOST_001_AddSelectsHost(t *testing.T) {
	svc := formService()
	d := newDriver(t, svc)
	d.press('a', "a")
	if !d.inForm() || !strings.Contains(d.view(), "Add host") {
		t.Fatalf("a did not open the form:\n%s", d.view())
	}
	d.typeText("Api Gateway") // spaces allowed (D-105)
	d.tab()
	d.typeText("10.0.0.40")
	d.save()
	if d.inForm() {
		t.Fatalf("form still open after ctrl+s:\n%s", d.view())
	}
	if len(svc.saves) != 1 || svc.saves[0].ID != "" || svc.saves[0].Port != 0 {
		t.Fatalf("saves = %+v", svc.saves)
	}
	h := selected(t, d)
	if h.Label != "Api Gateway" || h.Hostname != "10.0.0.40" || h.ID == "" {
		t.Fatalf("selected %+v", h)
	}
	v := d.view()
	if !strings.Contains(v, "> Api Gateway") || !strings.Contains(v, "Api Gateway: saved") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestFR_HOST_001_PortChangeShownAndUsed(t *testing.T) {
	svc := formService()
	d := newDriver(t, svc)
	d.press('j', "j") // web-prod, port 22 by default
	d.press('e', "e")
	if !strings.Contains(d.view(), "Edit host web-prod") {
		t.Fatalf("e did not open the form:\n%s", d.view())
	}
	d.tab()
	d.tab()
	d.typeText("2222")
	d.save()
	if len(svc.saves) != 1 || svc.saves[0].ID != "h2" || svc.saves[0].Port != 2222 {
		t.Fatalf("saves = %+v", svc.saves)
	}
	if v := d.view(); !strings.Contains(v, "> web-prod") || !strings.Contains(v, "ubuntu@10.0.0.10:2222") {
		t.Fatalf("list does not show :2222 on the selected host:\n%s", v)
	}
	d.press(tea.KeyEnter, "")
	if len(svc.connected) != 1 || svc.connected[0] != "h2" {
		t.Fatalf("connected %v", svc.connected)
	}
	if i := slices.IndexFunc(svc.hosts, func(x domain.Host) bool { return x.ID == "h2" }); svc.hosts[i].Port != 2222 {
		t.Fatal("the service does not hold port 2222 for the next connection")
	}
}

func TestFR_HOST_001_EscDiscards(t *testing.T) {
	svc := formService()
	d := newDriver(t, svc)

	// Nothing typed: Esc closes at once.
	d.press('a', "a")
	d.esc()
	if d.inForm() || len(d.root().modals) != 0 {
		t.Fatalf("esc on a clean form:\n%s", d.view())
	}

	// Unsaved changes: Esc asks, and the default (enter) keeps editing.
	d.press('j', "j")
	d.press('e', "e")
	d.typeText("-x")
	d.esc()
	if _, ok := d.root().modals[0].(confirm.Model); !ok || !strings.Contains(d.view(), "Discard unsaved changes?") {
		t.Fatalf("no confirm:\n%s", d.view())
	}
	golden(t, "hostedit-discard", d.view())
	d.press(tea.KeyEnter, "")
	if !d.inForm() || len(d.root().modals) != 0 || !strings.Contains(d.view(), "web-prod-x") {
		t.Fatalf("enter did not keep editing:\n%s", d.view())
	}
	d.esc()
	d.press('y', "y")
	if d.inForm() || len(svc.saves) != 0 {
		t.Fatalf("y did not discard; saves = %v", svc.saves)
	}
	if h := selected(t, d); h.ID != "h2" || h.Label != "web-prod" {
		t.Fatalf("selected %+v after discard", h)
	}
}

func TestFR_NAV_003_CtrlCInFormCancels(t *testing.T) {
	svc := formService()
	d := newDriver(t, svc)
	d.press('a', "a")
	d.typeText("new")
	d.key('c', tea.ModCtrl)
	if d.quit || d.inForm() {
		t.Fatalf("ctrl+c in the form: quit = %v, form open = %v", d.quit, d.inForm())
	}
	// Also with the discard confirm open.
	d.press('a', "a")
	d.typeText("new")
	d.esc()
	d.key('c', tea.ModCtrl)
	if d.quit || d.inForm() || len(d.root().modals) != 0 || len(svc.saves) != 0 {
		t.Fatalf("ctrl+c over the confirm: quit = %v, form = %v, modals = %d", d.quit, d.inForm(), len(d.root().modals))
	}
	d.key('c', tea.ModCtrl)
	if !d.quit {
		t.Fatal("ctrl+c on the list did not quit")
	}
}

func TestFR_HOST_003_ErrorsNextToFields(t *testing.T) {
	svc := formService()
	d := newDriver(t, svc)
	d.press('a', "a")
	d.typeText("Web-Prod")
	d.tab()
	d.typeText("my host")
	d.tab()
	d.typeText("abc")
	d.save()
	if !d.inForm() || len(svc.saves) != 0 {
		t.Fatalf("invalid form saved: %v", svc.saves)
	}
	v := ansiSeq.ReplaceAllString(d.view(), "")
	for field, msg := range map[string]string{
		"Label": "Label already in use", "Hostname": "Hostname cannot contain spaces", "Port": "Port must be 1 to 65535",
	} {
		if l := lineWith(v, msg); !strings.Contains(l, field) {
			t.Errorf("%q not on the %s line:\n%s", msg, field, v)
		}
	}
	golden(t, "hostedit-errors", v)

	// The first invalid field has focus. Fixing the fields saves; 65535
	// is a valid port.
	if !strings.Contains(lineWith(v, "Label already in use"), "> Label") {
		t.Fatalf("focus is not on the label:\n%s", v)
	}
	d.clear()
	d.typeText("Web Prod EU")
	d.tab()
	d.clear()
	d.typeText("2001:db8::1")
	d.tab()
	d.clear()
	d.typeText("65535")
	d.save()
	if d.inForm() || len(svc.saves) != 1 {
		t.Fatalf("valid form not saved:\n%s", d.view())
	}
	if h := selected(t, d); h.Label != "Web Prod EU" || h.Hostname != "2001:db8::1" || h.Port != 65535 {
		t.Fatalf("saved %+v", h)
	}
}

func TestFR_HOST_003_ServiceRejectionShownOnField(t *testing.T) {
	svc := formService()
	svc.saveErr = &domain.ValidationError{Fields: map[domain.Field]string{domain.FieldLabel: domain.MsgLabelInUse}}
	d := newDriver(t, svc)
	d.press('a', "a")
	d.typeText("racer")
	d.tab()
	d.typeText("h")
	d.save()
	if !d.inForm() || !strings.Contains(lineWith(d.view(), "Label already in use"), "Label") {
		t.Fatalf("view:\n%s", d.view())
	}
}

func TestFR_HOST_001_SaveFailureKeepsForm(t *testing.T) {
	svc := formService()
	svc.saveErr = errors.New("could not save config.json: permission denied")
	d := newDriver(t, svc)
	d.press('a', "a")
	d.typeText("api")
	d.tab()
	d.typeText("h")
	d.save()
	v := d.view()
	if !d.inForm() || !strings.Contains(v, "permission denied") || !strings.Contains(v, "api") {
		t.Fatalf("view:\n%s", v)
	}
	// The form is usable again: a second ctrl+s retries.
	svc.saveErr = nil
	d.save()
	if d.inForm() || len(svc.saves) != 2 {
		t.Fatalf("retry: saves = %d", len(svc.saves))
	}
}

func TestFR_HOST_001_IdentityPicker(t *testing.T) {
	svc := formService()
	d := newDriver(t, svc)
	d.press('a', "a")
	d.typeText("api")
	d.tab()
	d.typeText("10.0.0.40")
	d.tab()
	d.tab()
	d.typeText("root")
	d.tab()
	golden(t, "hostedit-add", d.view())
	if strings.Contains(d.view(), "per-host secret") {
		t.Fatal("a hidden identity is offered")
	}
	d.key(tea.KeyRight, 0)
	if !strings.Contains(ansiSeq.ReplaceAllString(d.view(), ""), "‹ deploy ›") {
		t.Fatalf("right did not pick deploy:\n%s", d.view())
	}
	d.save()
	if len(svc.saves) != 1 || svc.saves[0].IdentityID != "i1" || svc.saves[0].User != "root" {
		t.Fatalf("saves = %+v", svc.saves)
	}
}

func TestHostEditGoldenEdit(t *testing.T) {
	m := hostedit.New(domain.Host{
		Meta: domain.Meta{ID: "h1"}, Label: "db-staging", Hostname: "10.0.1.20", Port: 2222, User: "admin", IdentityID: "i2",
	}, testHosts, testIdentities)
	// A host's own hidden identity is offered and selected.
	golden(t, "hostedit-edit", m.View())
}
