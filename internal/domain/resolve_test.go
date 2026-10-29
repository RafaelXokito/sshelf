package domain

import (
	"testing"
	"time"
)

func TestResolveMinimal(t *testing.T) {
	t.Setenv("USER", "local")
	ids := []Identity{
		{Meta: Meta{ID: "id-1"}, Label: "ops", Username: "deploy", KeyPath: "~/.ssh/id_ed25519"},
		{Meta: Meta{ID: "id-2"}, Label: "other", Username: "other", KeyPath: "/other"},
		{Meta: Meta{ID: "id-3"}, Label: "stored", HasKey: true, HasPassword: true, HasPassphrase: true},
	}
	three := 3 * time.Second
	for _, tc := range []struct {
		name string
		host Host
		set  Settings
		want Target
	}{
		{
			name: "defaults",
			host: Host{Meta: Meta{ID: "h"}, Label: "web", Hostname: "10.0.0.1"},
			want: Target{HostID: "h", Label: "web", Hostname: "10.0.0.1", Port: 22, User: "local", ConnectTimeout: 10 * time.Second},
		},
		{
			name: "identity user and key",
			host: Host{Hostname: "h", Port: 2222, IdentityID: "id-1"},
			set:  Settings{ConnectTimeout: 3 * time.Second},
			want: Target{Hostname: "h", Port: 2222, User: "deploy", IdentityID: "id-1", IdentityLabel: "ops", KeyPath: "~/.ssh/id_ed25519", ConnectTimeout: 3 * time.Second},
		},
		{
			name: "host user wins",
			host: Host{Hostname: "h", User: "root", IdentityID: "id-2"},
			want: Target{Hostname: "h", Port: 22, User: "root", IdentityID: "id-2", IdentityLabel: "other", KeyPath: "/other", ConnectTimeout: 10 * time.Second},
		},
		{
			name: "unknown identity",
			host: Host{Hostname: "h", IdentityID: "gone"},
			want: Target{Hostname: "h", Port: 22, User: "local", IdentityID: "gone", ConnectTimeout: 10 * time.Second},
		},
		{
			name: "stored secrets flags and host timeout",
			host: Host{Hostname: "h", IdentityID: "id-3", Options: HostOptions{ConnectTimeout: &three}},
			set:  Settings{ConnectTimeout: time.Minute},
			want: Target{Hostname: "h", Port: 22, User: "local", IdentityID: "id-3", IdentityLabel: "stored",
				HasKey: true, HasPassword: true, HasPassphrase: true, ConnectTimeout: 3 * time.Second},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Resolve(tc.host, nil, ids, tc.set); got != tc.want {
				t.Fatalf("Resolve() =\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
}

// TestFR_IDENT_002_UsernameOverride: identity ops (user deploy) on host A,
// which has no user, resolves to deploy; host B, with user root, resolves
// to root with the same identity's key.
func TestFR_IDENT_002_UsernameOverride(t *testing.T) {
	t.Setenv("USER", "local")
	ops := Identity{Meta: Meta{ID: "ops"}, Label: "ops", Username: "deploy", HasKey: true}
	a := Resolve(Host{Hostname: "a", IdentityID: "ops"}, nil, []Identity{ops}, Settings{})
	b := Resolve(Host{Hostname: "b", User: "root", IdentityID: "ops"}, nil, []Identity{ops}, Settings{})
	if a.User != "deploy" || !a.HasKey {
		t.Fatalf("host A: user %q, key %v; want deploy with the identity's key", a.User, a.HasKey)
	}
	if b.User != "root" || !b.HasKey || b.IdentityID != "ops" {
		t.Fatalf("host B: user %q, key %v; want root with the identity's key", b.User, b.HasKey)
	}
	// No host user and no identity user: $USER.
	ops.Username = ""
	if c := Resolve(Host{Hostname: "c", IdentityID: "ops"}, nil, []Identity{ops}, Settings{}); c.User != "local" {
		t.Fatalf("host C: user %q, want $USER", c.User)
	}
}
