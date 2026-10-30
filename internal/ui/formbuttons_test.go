package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/confirm"
)

// Fields in the host form (hostedit) and the identity form (identityedit).
const (
	hostFormFields     = 9
	identityFormFields = 6
)

func (d *driver) space() { d.send(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}) }
func (d *driver) enter() { d.key(tea.KeyEnter, 0) }

func TestFR_NAV_015_ButtonsRendered(t *testing.T) {
	d := newDriver(t, formService())
	d.press('a', "a")
	v := plain(d.view())
	row := lineWith(v, components.SaveButton)
	if !strings.Contains(row, components.CancelButton) || strings.Contains(row, components.FocusGlyph) {
		t.Fatalf("button row %q:\n%s", row, v)
	}
	// The buttons follow the last field, after a blank line.
	lines := strings.Split(v, "\n")
	i := slices.Index(lines, row)
	if !strings.Contains(lines[i-2], "Passphrase") || strings.TrimSpace(lines[i-1]) != "" {
		t.Fatalf("buttons not after the last field:\n%s", v)
	}
	d.tabTo(hostFormFields) // Save
	if row := lineWith(plain(d.view()), components.SaveButton); !strings.Contains(row, components.FocusGlyph+components.SaveButton) {
		t.Fatalf("focused Save has no glyph: %q", row)
	}
	golden(t, "hostedit-buttons", d.m)
	d.esc()

	d = newDriver(t, identityService())
	d.press('i', "i")
	d.press('e', "e")
	d.tabTo(identityFormFields + 1) // Cancel
	if row := lineWith(plain(d.view()), components.CancelButton); !strings.Contains(row, components.FocusGlyph+components.CancelButton) {
		t.Fatalf("focused Cancel has no glyph: %q", row)
	}
	golden(t, "identityedit-buttons", d.m)
}

// TestFR_NAV_015_HostFormButtons checks that the host form's buttons act
// exactly like ctrl+s and esc, including validation and the unsaved-changes
// confirmation.
func TestFR_NAV_015_HostFormButtons(t *testing.T) {
	svc := formService()
	d := newDriver(t, svc)

	// [ Cancel ] on a clean form closes it at once.
	d.press('a', "a")
	d.tabTo(hostFormFields + 1)
	d.space()
	if d.inForm() || len(d.root().modals) != 0 {
		t.Fatalf("space on Cancel of a clean form:\n%s", d.view())
	}

	// [ Save ] on an invalid form shows errors and focuses the first one.
	d.press('a', "a")
	d.tabTo(hostFormFields)
	d.enter()
	if !d.inForm() || len(svc.saves) != 0 || !strings.Contains(plain(d.view()), components.ErrorPrefix+domain.MsgLabelRequired) {
		t.Fatalf("enter on Save of an empty form:\n%s", d.view())
	}
	if !strings.Contains(plain(d.view()), "> Label") {
		t.Fatalf("the first error is not focused:\n%s", d.view())
	}

	// [ Cancel ] with unsaved changes asks first, like esc.
	d.typeText("api")
	d.tabTo(hostFormFields + 1)
	d.enter()
	if len(d.root().modals) != 1 {
		t.Fatalf("no confirm from Cancel:\n%s", d.view())
	}
	if _, ok := d.root().modals[0].(confirm.Model); !ok {
		t.Fatalf("modal %T", d.root().modals[0])
	}
	d.press('n', "n")
	if !d.inForm() {
		t.Fatal("n on the confirm closed the form")
	}

	// [ Save ] on a valid form saves, like ctrl+s.
	d.tabTo(1) // wraps to Label
	d.tab()
	d.typeText("10.0.0.40")
	d.tabTo(hostFormFields - 1) // Save
	d.space()
	if d.inForm() || len(svc.saves) != 1 || svc.saves[0].Label != "api" || svc.saves[0].Hostname != "10.0.0.40" {
		t.Fatalf("space on Save: saves %+v\n%s", svc.saves, d.view())
	}
}

func TestFR_NAV_015_IdentityFormButtons(t *testing.T) {
	svc := identityService()
	d := newDriver(t, svc)
	d.press('i', "i")
	d.press('e', "e") // ops
	d.clear()
	d.typeText("ops2")
	d.tabTo(identityFormFields + 1) // Cancel
	d.space()
	if _, ok := d.root().modals[0].(confirm.Model); !ok || !d.inIdentityForm() {
		t.Fatalf("no confirm from Cancel:\n%s", d.view())
	}
	d.press('n', "n")
	d.key(tea.KeyTab, tea.ModShift) // Save
	d.enter()
	if d.inIdentityForm() || !slices.ContainsFunc(svc.identities, func(i domain.Identity) bool { return i.Label == "ops2" }) {
		t.Fatalf("enter on Save did not save:\n%s", d.view())
	}
}
