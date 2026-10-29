package domain

import (
	"testing"
	"time"
)

func TestResolveMinimal(t *testing.T) {
	ids := []Identity{
		{Meta: Meta{ID: "id-1"}, Username: "deploy", KeyPath: "~/.ssh/id_ed25519"},
		{Meta: Meta{ID: "id-2"}, Username: "other", KeyPath: "/other"},
	}
	for _, tc := range []struct {
		name string
		host Host
		set  Settings
		want Target
	}{
		{
			name: "defaults",
			host: Host{Meta: Meta{ID: "h"}, Label: "web", Hostname: "10.0.0.1"},
			want: Target{HostID: "h", Label: "web", Hostname: "10.0.0.1", Port: 22, ConnectTimeout: 10 * time.Second},
		},
		{
			name: "identity user and key",
			host: Host{Hostname: "h", Port: 2222, IdentityID: "id-1"},
			set:  Settings{ConnectTimeout: 3 * time.Second},
			want: Target{Hostname: "h", Port: 2222, User: "deploy", IdentityID: "id-1", KeyPath: "~/.ssh/id_ed25519", ConnectTimeout: 3 * time.Second},
		},
		{
			name: "host user wins",
			host: Host{Hostname: "h", User: "root", IdentityID: "id-2"},
			want: Target{Hostname: "h", Port: 22, User: "root", IdentityID: "id-2", KeyPath: "/other", ConnectTimeout: 10 * time.Second},
		},
		{
			name: "unknown identity",
			host: Host{Hostname: "h", IdentityID: "gone"},
			want: Target{Hostname: "h", Port: 22, IdentityID: "gone", ConnectTimeout: 10 * time.Second},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Resolve(tc.host, nil, ids, tc.set); got != tc.want {
				t.Fatalf("Resolve() =\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
}
