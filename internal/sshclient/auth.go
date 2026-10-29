package sshclient

import (
	"context"
	"errors"
	"slices"

	"golang.org/x/crypto/ssh"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

// Authentication method names (RFC 4252).
const (
	methodPublicKey = "publickey"
	methodPassword  = "password"
)

// ErrNoCredential is returned by CredentialSource.Password when the
// target's identity has no stored password.
var ErrNoCredential = errors.New("no stored credential")

// authPlan returns the methods sshclient offers for t, in order
// (ARCH-CONN-001): the identity's explicit key (stored or file), then the
// password. Methods the server does not offer are skipped.
func authPlan(t domain.Target) []string {
	var plan []string
	if t.HasKey || t.KeyPath != "" {
		plan = append(plan, methodPublicKey)
	}
	if t.HasPassword {
		plan = append(plan, methodPassword)
	}
	return plan
}

// authState drives authentication for one connection attempt. Its
// callbacks run on the handshake goroutine, one at a time, and only after
// the host key has been verified, so credentials are loaded lazily and
// only for a verified host (SEC-SESSION-001, SEC-MEM-009). It keeps no
// credential once a callback has returned.
type authState struct {
	ctx   context.Context
	t     domain.Target
	creds CredentialSource
	plan  []string
	err   error // first fatal error; aborts the handshake
}

func newAuthState(ctx context.Context, t domain.Target, creds CredentialSource) *authState {
	return &authState{ctx: ctx, t: t, creds: creds, plan: authPlan(t)}
}

// fail records err as the reason to abort and returns it.
func (s *authState) fail(err error) error {
	if s.err == nil {
		s.err = err
	}
	return err
}

// next is the ssh.ClientAuthCallback: it picks the next method of the plan
// that the server allows and that has not been tried, or aborts with the
// first fatal error. x/crypto would otherwise carry on with the next
// method after a callback error.
func (s *authState) next(ac *ssh.ClientAuthContext) (ssh.AuthMethod, error) {
	if s.err != nil {
		return nil, s.err
	}
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	for _, m := range s.plan {
		if !slices.Contains(ac.AllowedMethods, m) || slices.Contains(ac.TriedMethods, m) ||
			slices.Contains(ac.PartialSuccessMethods, m) {
			continue
		}
		logAuthAttempt(s.t, m)
		return s.method(m), nil
	}
	return nil, nil // nothing left: x/crypto reports "unable to authenticate"
}

func (s *authState) method(m string) ssh.AuthMethod {
	switch m {
	case methodPublicKey:
		return ssh.PublicKeysCallback(s.signers)
	default:
		return ssh.PasswordCallback(s.password)
	}
}

func (s *authState) signers() ([]ssh.Signer, error) {
	if s.creds == nil {
		return nil, nil
	}
	signers, err := s.creds.Signers(s.ctx, s.t)
	if err != nil {
		return nil, s.fail(err)
	}
	return signers, nil
}

func (s *authState) password() (string, error) {
	if s.creds == nil {
		return "", s.fail(ErrNoCredential)
	}
	pw, err := s.creds.Password(s.ctx, s.t)
	if err != nil {
		return "", s.fail(err)
	}
	return pw, nil
}

// done drops everything the state holds once the handshake is over;
// x/crypto keeps the client config, and so the callbacks, for the
// connection's lifetime.
func (s *authState) done() {
	s.creds = nil
	s.plan = nil
}
