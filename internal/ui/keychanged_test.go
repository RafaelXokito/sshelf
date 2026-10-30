package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
)

func mismatchErr() error {
	return fmt.Errorf("ssh: handshake failed: %w", &app.HostKeyMismatchError{
		Address:     "10.0.0.10:22",
		Name:        "10.0.0.10",
		KeyType:     "ssh-ed25519",
		Fingerprint: "SHA256:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU",
		Stale: []app.KnownKey{{
			File:        "/home/u/.config/sshelf/known_hosts",
			Line:        12,
			KeyType:     "ssh-ed25519",
			Fingerprint: "SHA256:uNiVztksCsDhcc0u9e8BujQXVUpKZIDTMczCvj3tD2s",
		}},
	})
}

func TestARCH_CONN_004_BlockGolden(t *testing.T) {
	svc := &fakeService{hosts: testHosts, connectErr: mismatchErr()}
	d := newDriver(t, svc)
	connectSecond(d)
	golden(t, "keychanged", d.m)
	if len(d.root().tabs) != 0 {
		t.Fatal("a session ran after a changed key")
	}
}

func TestFR_KH_002_NoConnectAnyway(t *testing.T) {
	svc := &fakeService{hosts: testHosts, connectErr: mismatchErr()}
	d := newDriver(t, svc)
	connectSecond(d)
	v := d.view()
	for _, want := range []string{
		"HOST KEY HAS CHANGED", "man-in-the-middle",
		"SHA256:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU", "SHA256:uNiVztksCsDhcc0u9e8BujQXVUpKZIDTMczCvj3tD2s",
		"delete line 12 of /home/u/.config/sshelf/known_hosts",
		"ssh-keygen -R 10.0.0.10 -f /home/u/.config/sshelf/known_hosts",
		"web-prod: host key changed; connection blocked",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("block screen lacks %q", want)
		}
	}
	// No key connects: y, a, c and enter only close or do nothing.
	for _, k := range []string{"y", "a", "c"} {
		d.press(rune(k[0]), k)
	}
	if len(svc.connected) != 1 || len(d.root().tabs) != 0 || len(d.root().modals) != 1 {
		t.Fatalf("connects = %d, tabs = %d, modals = %d", len(svc.connected), len(d.root().tabs), len(d.root().modals))
	}
	d.press(tea.KeyEscape, "")
	if len(d.root().modals) != 0 || len(svc.connected) != 1 {
		t.Fatalf("esc: modals = %d, connects = %d", len(d.root().modals), len(svc.connected))
	}
}

func TestFR_KH_003_RevokedStatusNoTrustAction(t *testing.T) {
	err := fmt.Errorf("ssh: handshake failed: %w", &app.HostKeyRevokedError{
		Address: "10.0.0.10:22", KeyType: "ssh-ed25519",
		Entry: app.KnownKey{File: "/home/u/.ssh/known_hosts", Line: 3},
	})
	d := newDriver(t, &fakeService{hosts: testHosts, connectErr: err})
	connectSecond(d)
	v := d.view()
	if !strings.Contains(v, "web-prod: host key revoked (/home/u/.ssh/known_hosts line 3)") {
		t.Fatalf("view:\n%s", v)
	}
	if len(d.root().modals) != 0 || strings.Contains(strings.ToLower(v), "trust") {
		t.Fatalf("a trust action is offered for a revoked key:\n%s", v)
	}
}
