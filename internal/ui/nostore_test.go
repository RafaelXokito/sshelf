package ui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
)

// SecretsAvailable returns the fake's store error (nil: available).
func (f *fakeService) SecretsAvailable(context.Context) error {
	f.checkGoroutine("SecretsAvailable")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.storeErr
}

// noSecretService is the probe result on Linux without a Secret Service.
var noSecretService = &app.StoreError{Store: "Secret Service", Err: app.ErrStoreUnavailable}

const banner = "! No Secret Service found: install or start GNOME Keyring or KWallet"

func TestARCH_SEC_006_DisabledFieldGolden(t *testing.T) {
	svc := identityService()
	svc.storeErr = noSecretService
	d := newDriver(t, svc)
	if v := d.view(); !strings.Contains(v, banner) {
		t.Fatalf("no banner on the host list:\n%s", v)
	}
	golden(t, "nostore-hostlist", d.m)
	d.press('i', "i")
	if v := d.view(); !strings.Contains(v, banner) {
		t.Fatalf("no banner on the identities:\n%s", v)
	}
	d.press('e', "e")
	v := ansiSeq.ReplaceAllString(d.view(), "")
	for _, row := range []string{"Password", "Stored key", "Passphrase"} {
		if !strings.Contains(lineWith(v, row), "(disabled: no Secret Service)") {
			t.Fatalf("%s is not disabled:\n%s", row, v)
		}
	}
	if !strings.Contains(v, banner) {
		t.Fatalf("no banner in the identity editor:\n%s", v)
	}
	golden(t, "nostore-identityedit", d.m)
	// Typing into a disabled field does nothing; ctrl+o is off too.
	d.tabTo(2)
	d.typeText("secret")
	d.tab()
	d.key('o', tea.ModCtrl)
	d.save()
	if len(svc.idSaves) != 1 || svc.idSaves[0].ImportKeyFile || svc.idSaves[0].Password.Len() != 0 {
		t.Fatalf("saves = %+v", svc.idSaves)
	}
	if _, ok := d.onIdentities(); !ok {
		t.Fatalf("the key-file identity was not saved:\n%s", d.view())
	}
	// The host form too.
	d.esc()
	d.press('a', "a")
	if l := lineWith(ansiSeq.ReplaceAllString(d.view(), ""), "Password"); !strings.Contains(l, "(disabled: no Secret Service)") {
		t.Fatalf("host form password %q", l)
	}
}

func TestFR_CRED_002_SaveRefusedMessageInForm(t *testing.T) {
	svc := identityService()
	svc.idSaveErr = noSecretService // the store went away after startup
	d := newDriver(t, svc)
	d.press('i', "i")
	d.press('a', "a")
	d.typeText("db")
	d.tabTo(2)
	d.typeText("pw")
	d.save()
	v := d.view()
	if !d.inIdentityForm() || !strings.Contains(v, "No Secret Service found: install or start GNOME Keyring or KWallet") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestFR_CRED_002_StoreAvailableNoBanner(t *testing.T) {
	d := newDriver(t, identityService())
	if strings.Contains(d.view(), "Secret Service") {
		t.Fatalf("banner with a working store:\n%s", d.view())
	}
	if _, ok := d.root().screen.(hostlist.Model); !ok {
		t.Fatal("not on the host list")
	}
}
