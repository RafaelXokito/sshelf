package sshclient

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// pwCreds hands out one signer (or none) and a password.
type pwCreds struct {
	signer   ssh.Signer
	password string
}

func (c pwCreds) Signers(context.Context, domain.Target) ([]ssh.Signer, error) {
	if c.signer == nil {
		return nil, nil
	}
	return []ssh.Signer{c.signer}, nil
}

func (c pwCreds) Password(context.Context, domain.Target) (string, error) {
	if c.password == "" {
		return "", ErrNoCredential
	}
	return c.password, nil
}

// trusting returns a Config whose known_hosts trusts srv.
func trusting(t *testing.T, srv *sshtest.Server) Config {
	t.Helper()
	kh := filepath.Join(t.TempDir(), "known_hosts")
	write(t, kh, srv.KnownHostsLine())
	return Config{KnownHostsFiles: []string{kh}}
}

func TestARCH_CONN_001_AuthMethodTable(t *testing.T) {
	for _, tc := range []struct {
		name string
		t    domain.Target
		want []string
	}{
		{"no identity", domain.Target{}, []string{"password", "keyboard-interactive"}},
		{"key file", domain.Target{KeyPath: "~/.ssh/id"}, []string{"publickey", "password", "keyboard-interactive"}},
		{"stored key", domain.Target{HasKey: true, HasPassphrase: true}, []string{"publickey", "password", "keyboard-interactive"}},
		{"password", domain.Target{HasPassword: true}, []string{"password", "keyboard-interactive"}},
		{"key and password", domain.Target{HasKey: true, HasPassword: true}, []string{"publickey", "password", "keyboard-interactive"}},
	} {
		if got := authPlan(tc.t); !slices.Equal(got, tc.want) {
			t.Errorf("%s: plan = %v, want %v", tc.name, got, tc.want)
		}
	}

	// The server sees the key before the password.
	signer, err := sshtest.GenerateSigner(sshtest.Ed25519)
	if err != nil {
		t.Fatal(err)
	}
	other, err := sshtest.GenerateSigner(sshtest.Ed25519)
	if err != nil {
		t.Fatal(err)
	}
	srv := sshtest.New(t, sshtest.WithAuthorizedKey("alice", other.PublicKey()), sshtest.WithPassword("alice", "pw"))
	target := domain.Target{Hostname: srv.Host(), Port: srv.Port(), User: "alice", HasKey: true, HasPassword: true}
	c, err := trusting(t, srv).Connect(context.Background(), target, pwCreds{signer: signer, password: "pw"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	var got []string
	for _, a := range srv.AuthAttempts() {
		got = append(got, a.Method)
	}
	if !slices.Equal(got, []string{"none", "publickey", "password"}) {
		t.Fatalf("server saw %v, want none, publickey, password", got)
	}
}
