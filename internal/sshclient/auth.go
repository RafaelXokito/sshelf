package sshclient

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"golang.org/x/crypto/ssh"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

// Authentication method names (RFC 4252).
const (
	methodPublicKey           = "publickey"
	methodPassword            = "password"
	methodKeyboardInteractive = "keyboard-interactive"
)

// MaxAuthFailures is how many rejected passwords or keyboard-interactive
// answers end the attempt with ErrAuthFailed (FR-CONN-003).
const MaxAuthFailures = 3

// Authentication errors.
var (
	// ErrNoCredential is returned by CredentialSource.Password when the
	// target's identity has no stored password.
	ErrNoCredential = errors.New("no stored credential")
	// ErrCredentialMissing means the identity has the secret but the
	// credential store does not contain it: "not set on this device"
	// (SEC-CRED-007, D-108). The user is asked for it instead.
	ErrCredentialMissing = errors.New("not set on this device")
	// ErrAuthCancelled means the user cancelled an authentication prompt
	// (ARCH-CONN-002).
	ErrAuthCancelled = errors.New("authentication cancelled")
	// ErrAuthFailed means the server rejected every credential, or the
	// user failed MaxAuthFailures times (FR-CONN-003, FR-CONN-006).
	ErrAuthFailed = errors.New("authentication failed")
)

// PromptKind says what an AuthPrompt asks for.
type PromptKind int

// The prompt kinds.
const (
	// PromptPassword asks for the user's password on the host.
	PromptPassword PromptKind = iota + 1
	// PromptPassphrase asks for the passphrase of an encrypted key.
	PromptPassphrase
	// PromptKeyboardInteractive shows the server's keyboard-interactive
	// questions.
	PromptKeyboardInteractive
)

// Question is one field of an AuthPrompt.
type Question struct {
	Text string // sanitised
	Echo bool   // the answer may be shown as it is typed
}

// AuthPrompt is a question for the user during authentication. Every
// server-supplied string in it has been through domain.Sanitize
// (SEC-SESSION-003).
type AuthPrompt struct {
	Kind        PromptKind
	Label       string // the host's label
	User        string // the user name sent to the server
	Host        string // the host name as dialled
	Key         string // PromptPassphrase: the key's file path or description
	Name        string // keyboard-interactive name, from the server
	Instruction string // keyboard-interactive instruction, from the server
	Questions   []Question
	Note        string // e.g. "password not set on this device"; never from the server
}

// passwordQuestion matches a keyboard-interactive prompt that asks for the
// password (ARCH-CONN-002).
var passwordQuestion = regexp.MustCompile(`(?i)password`)

// authPlan returns the methods sshclient offers for t, in order
// (ARCH-CONN-001): the identity's explicit key (stored or file), then
// password, then keyboard-interactive. The last two use the stored
// password or ask the user (FR-CONN-003). Methods the server does not
// offer are skipped.
func authPlan(t domain.Target) []string {
	var plan []string
	if t.HasKey || t.KeyPath != "" {
		plan = append(plan, methodPublicKey)
	}
	return append(plan, methodPassword, methodKeyboardInteractive)
}

func interactive(m string) bool { return m == methodPassword || m == methodKeyboardInteractive }

// authState drives authentication for one connection attempt. Its
// callbacks run on the handshake goroutine, one at a time, and only after
// the host key has been verified, so credentials are loaded lazily and
// only for a verified host (SEC-SESSION-001, SEC-MEM-009). It keeps no
// credential once a callback has returned.
type authState struct {
	ctx         context.Context
	t           domain.Target
	creds       CredentialSource
	p           Prompter
	plan        []string
	storedTried bool  // the stored password was fetched (it is offered once)
	failures    int   // rejected password and keyboard-interactive attempts
	err         error // first fatal error; aborts the handshake
}

func newAuthState(ctx context.Context, t domain.Target, creds CredentialSource, p Prompter) *authState {
	if p == nil {
		p = RejectUnknownHosts{}
	}
	return &authState{ctx: ctx, t: t, creds: creds, p: p, plan: authPlan(t)}
}

// fail records err as the reason to abort and returns it.
func (s *authState) fail(err error) error {
	if s.err == nil {
		s.err = err
	}
	return err
}

// next is the ssh.ClientAuthCallback. It aborts with the first fatal
// error (x/crypto would otherwise carry on with the next method), then
// picks the first method of the plan that the server allows and that has
// not been tried. When none is left it retries password or
// keyboard-interactive, until MaxAuthFailures rejections.
func (s *authState) next(ac *ssh.ClientAuthContext) (ssh.AuthMethod, error) {
	if s.err != nil {
		return nil, s.err
	}
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	s.failures = 0
	for _, m := range ac.TriedMethods {
		if interactive(m) {
			s.failures++
		}
	}
	if s.failures >= MaxAuthFailures {
		return nil, fmt.Errorf("%w after %d attempts", ErrAuthFailed, s.failures)
	}
	allowed := func(m string) bool {
		return slices.Contains(ac.AllowedMethods, m) && !slices.Contains(ac.PartialSuccessMethods, m)
	}
	for _, m := range s.plan {
		if allowed(m) && !slices.Contains(ac.TriedMethods, m) {
			logAuthAttempt(s.t, m)
			return s.method(m), nil
		}
	}
	for _, m := range s.plan {
		if interactive(m) && allowed(m) {
			logAuthAttempt(s.t, m)
			return s.method(m), nil
		}
	}
	return nil, nil // nothing left: x/crypto reports "unable to authenticate"
}

func (s *authState) method(m string) ssh.AuthMethod {
	switch m {
	case methodPublicKey:
		return ssh.PublicKeysCallback(s.signers)
	case methodPassword:
		return ssh.PasswordCallback(s.password)
	default:
		return ssh.KeyboardInteractive(s.challenge)
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

// stored returns the stored password the first time it is called in a
// connection attempt (ok true), and ok false afterwards or when there is
// none; note then says why the user is asked.
func (s *authState) stored() (pw string, ok bool, note string, err error) {
	if s.storedTried {
		if s.failures > 0 {
			return "", false, "Permission denied, please try again.", nil
		}
		return "", false, "", nil
	}
	s.storedTried = true
	if s.creds == nil {
		return "", false, "", nil
	}
	pw, err = s.creds.Password(s.ctx, s.t)
	switch {
	case err == nil:
		return pw, true, "", nil
	case errors.Is(err, ErrNoCredential):
		return "", false, "", nil
	case errors.Is(err, ErrCredentialMissing):
		return "", false, "The stored password is not set on this device.", nil
	default:
		return "", false, "", s.fail(err)
	}
}

// password is the password method: the stored password once, then the
// user's answer (FR-CONN-003).
func (s *authState) password() (string, error) {
	pw, ok, note, err := s.stored()
	if err != nil || ok {
		return pw, err
	}
	answers, err := s.ask(AuthPrompt{
		Kind:      PromptPassword,
		Questions: []Question{{Text: "Password:"}},
		Note:      note,
	})
	if err != nil {
		return "", err
	}
	return answers[0], nil
}

// challenge is the keyboard-interactive bridge (FR-CONN-008,
// SEC-SESSION-002): a round with exactly one no-echo question matching
// /password/i gets the stored password, once; every other round goes to
// the user, labelled as coming from the server, with its text sanitised.
func (s *authState) challenge(name, instruction string, questions []string, echos []bool) ([]string, error) {
	if len(questions) == 0 {
		return nil, nil // an empty round needs no answer
	}
	var note string
	if len(questions) == 1 && !echos[0] && passwordQuestion.MatchString(questions[0]) {
		pw, ok, n, err := s.stored()
		if err != nil || ok {
			return []string{pw}, err
		}
		note = n
	}
	q := AuthPrompt{
		Kind:        PromptKeyboardInteractive,
		Name:        domain.Sanitize(name),
		Instruction: domain.Sanitize(instruction),
		Note:        note,
	}
	for i, text := range questions {
		q.Questions = append(q.Questions, Question{Text: domain.Sanitize(text), Echo: i < len(echos) && echos[i]})
	}
	return s.ask(q)
}

// ask shows q and returns exactly one answer per question. A cancel or
// any other error aborts the attempt.
func (s *authState) ask(q AuthPrompt) ([]string, error) {
	q.Label, q.User, q.Host = s.t.Label, s.t.User, s.t.Hostname
	answers, err := s.p.Ask(s.ctx, q)
	if err != nil {
		if ctxErr := s.ctx.Err(); ctxErr != nil {
			err = ctxErr
		}
		return nil, s.fail(err)
	}
	if len(answers) != len(q.Questions) {
		return nil, s.fail(fmt.Errorf("prompt returned %d answers for %d questions", len(answers), len(q.Questions)))
	}
	return answers, nil
}

// done drops everything the state holds once the handshake is over;
// x/crypto keeps the client config, and so the callbacks, for the
// connection's lifetime.
func (s *authState) done() {
	s.creds = nil
	s.p = nil
	s.plan = nil
}
