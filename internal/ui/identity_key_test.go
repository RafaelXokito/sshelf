package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/identities"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/identityedit"
)

const pastedKey = "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\nBBBB\n-----END OPENSSH PRIVATE KEY-----\n"

func TestFR_IDENT_003_PasteKeyInForm(t *testing.T) {
	svc := identityService()
	d := newDriver(t, svc)
	d.press('i', "i")
	d.press('a', "a")
	d.typeText("deploy-key")
	for range 4 {
		d.tab()
	}
	d.send(tea.PasteMsg{Content: strings.ReplaceAll(pastedKey, "\n", "\r\n")})
	v := ansiSeq.ReplaceAllString(d.view(), "")
	if strings.Contains(v, "BEGIN") || strings.Contains(v, "AAAA") {
		t.Fatalf("the pasted key is shown:\n%s", v)
	}
	d.save()
	if d.inIdentityForm() {
		t.Fatalf("not saved:\n%s", d.view())
	}
	var id domain.ID
	for _, i := range svc.identities {
		if i.Label == "deploy-key" {
			id = i.ID
		}
	}
	if got := string(svc.idSecrets[string(id)+"/private-key"]); got != pastedKey {
		t.Fatalf("stored key %q, want the paste with its newlines", got)
	}
}

func TestFR_IDENT_003_StoreKeyFileAction(t *testing.T) {
	svc := identityService()
	d := newDriver(t, svc)
	d.press('i', "i")
	d.press('e', "e")       // ops, with a key file
	d.key('o', tea.ModCtrl) // not on the key file row: ignored
	for range 3 {
		d.tab()
	}
	d.key('o', tea.ModCtrl)
	if !strings.Contains(d.view(), "stored in the credential store on save") {
		t.Fatalf("view:\n%s", d.view())
	}
	d.save()
	if len(svc.idSaves) != 1 || !svc.idSaves[0].ImportKeyFile || svc.idSaves[0].KeyPath != "~/.ssh/id_ed25519" {
		t.Fatalf("saves = %+v", svc.idSaves)
	}
}

func TestFR_IDENT_003_KeyErrorsShown(t *testing.T) {
	svc := identityService()
	svc.idSaveErr = &domain.ValidationError{Fields: map[domain.Field]string{domain.FieldKey: app.MsgKeyTooLarge}}
	d := newDriver(t, svc)
	d.press('i', "i")
	d.press('a', "a")
	d.typeText("big")
	d.save()
	v := ansiSeq.ReplaceAllString(d.view(), "")
	if !d.inIdentityForm() || !strings.Contains(v, "reference the key file instead") || !strings.Contains(lineWith(v, "Stored key"), "see below") {
		t.Fatalf("view:\n%s", v)
	}
	if !strings.Contains(lineWith(v, "Stored key"), "> Stored key") {
		t.Fatalf("focus not on the stored key:\n%s", v)
	}
}

func TestFR_IDENT_003_ViewShowsTypeAndFingerprint(t *testing.T) {
	info := app.IdentityInfo{
		Identity: domain.Identity{Meta: domain.Meta{ID: "i1"}, Label: "ops", HasKey: true},
		Key:      app.SecretSet, KeyType: "ED25519", KeyFingerprint: "SHA256:abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG",
	}
	if d := identities.Details(info); !strings.Contains(d, "ED25519 SHA256:abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG") {
		t.Fatalf("details:\n%s", d)
	}
	if v := identityedit.New(info, nil).View(); !strings.Contains(v, "Stored key: ED25519 SHA256:abc") {
		t.Fatalf("form:\n%s", v)
	}
}
