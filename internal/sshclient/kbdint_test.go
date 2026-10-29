package sshclient

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// Made-up test secrets; no test prints them.
const (
	kbdPassword = "canary-kbd-password" //nolint:gosec // a made-up test secret
	kbdOTP      = "123456"
)

// scriptedPrompter trusts no host key and answers auth prompts with
// answer, recording every prompt.
type scriptedPrompter struct {
	answer func(AuthPrompt) ([]string, error)
	mu     sync.Mutex
	asks   []AuthPrompt
}

func (p *scriptedPrompter) HostKey(context.Context, HostKeyPrompt) (bool, error) { return false, nil }

func (p *scriptedPrompter) Ask(_ context.Context, q AuthPrompt) ([]string, error) {
	p.mu.Lock()
	p.asks = append(p.asks, q)
	p.mu.Unlock()
	if p.answer == nil {
		return nil, ErrAuthCancelled
	}
	return p.answer(q)
}

func (p *scriptedPrompter) prompts() []AuthPrompt {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.asks)
}

// answers returns a prompt answer function that answers each question
// by matching its text against the keys of byText.
func answers(byText map[string]string) func(AuthPrompt) ([]string, error) {
	return func(q AuthPrompt) ([]string, error) {
		var out []string
		for _, qq := range q.Questions {
			a := "wrong"
			for k, v := range byText {
				if strings.Contains(qq.Text, k) {
					a = v
				}
			}
			out = append(out, a)
		}
		return out, nil
	}
}

// kbdConnect connects as alice to a server with the given options, with a
// stored password (or none) and prompter p.
func kbdConnect(t *testing.T, password string, p Prompter, opts ...sshtest.Option) (*sshtest.Server, error) {
	t.Helper()
	srv := sshtest.New(t, opts...)
	target := domain.Target{Label: "bastion", Hostname: srv.Host(), Port: srv.Port(), User: "alice", HasPassword: password != ""}
	c, err := trusting(t, srv).Connect(context.Background(), target, pwCreds{password: password}, p)
	if c != nil {
		_ = c.Close()
	}
	return srv, err
}

var passwordRound = sshtest.Challenge{Questions: []sshtest.Question{{Prompt: "Password: ", Answer: kbdPassword}}}

func otpRound(echo bool) sshtest.Challenge {
	return sshtest.Challenge{
		Instruction: "Two-factor authentication",
		Questions:   []sshtest.Question{{Prompt: "Verification code: ", Echo: echo, Answer: kbdOTP}},
	}
}

func TestFR_CONN_008_KbdIntPasswordAutoAnswered(t *testing.T) {
	p := &scriptedPrompter{}
	srv, err := kbdConnect(t, kbdPassword, p, sshtest.WithKeyboardInteractive("alice", passwordRound))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(p.prompts()); n != 0 {
		t.Fatalf("%d prompts shown, want none", n)
	}
	if got := srv.AuthAttempts(); got[len(got)-1].Method != "keyboard-interactive" || !got[len(got)-1].Success {
		t.Fatalf("auth attempts = %+v", got)
	}
}

func TestFR_CONN_008_OTPShownPasswordFilled(t *testing.T) {
	p := &scriptedPrompter{answer: answers(map[string]string{"code": kbdOTP})}
	_, err := kbdConnect(t, kbdPassword, p, sshtest.WithKeyboardInteractive("alice", passwordRound, otpRound(true)))
	if err != nil {
		t.Fatal(err)
	}
	asks := p.prompts()
	if len(asks) != 1 || len(asks[0].Questions) != 1 || !strings.Contains(asks[0].Questions[0].Text, "Verification code") {
		t.Fatalf("prompts = %+v, want only the OTP prompt", asks)
	}
	q := asks[0]
	if q.Kind != PromptKeyboardInteractive || q.Instruction != "Two-factor authentication" || q.User != "alice" || q.Label != "bastion" {
		t.Fatalf("prompt = %+v", q)
	}
}

func TestFR_CONN_008_EchoOnlyWhenServerSets(t *testing.T) {
	for _, echo := range []bool{false, true} {
		p := &scriptedPrompter{answer: answers(map[string]string{"code": kbdOTP})}
		if _, err := kbdConnect(t, kbdPassword, p, sshtest.WithKeyboardInteractive("alice", passwordRound, otpRound(echo))); err != nil {
			t.Fatal(err)
		}
		if asks := p.prompts(); len(asks) != 1 || asks[0].Questions[0].Echo != echo {
			t.Fatalf("server echo %v: prompts = %+v", echo, asks)
		}
	}
}

func TestFR_CONN_008_NoStoredPasswordPrompts(t *testing.T) {
	p := &scriptedPrompter{answer: answers(map[string]string{"Password": kbdPassword})}
	if _, err := kbdConnect(t, "", p, sshtest.WithKeyboardInteractive("alice", passwordRound)); err != nil {
		t.Fatal(err)
	}
	asks := p.prompts()
	if len(asks) != 1 || asks[0].Questions[0].Text != "Password: " || asks[0].Questions[0].Echo {
		t.Fatalf("prompts = %+v, want the password prompt, not echoed", asks)
	}
}

func TestSEC_SESSION_002_TwoPromptsNoAutoAnswer(t *testing.T) {
	both := sshtest.Challenge{Questions: []sshtest.Question{
		{Prompt: "Password: ", Answer: kbdPassword},
		{Prompt: "Verification code: ", Answer: kbdOTP},
	}}
	p := &scriptedPrompter{answer: answers(map[string]string{"Password": kbdPassword, "code": kbdOTP})}
	if _, err := kbdConnect(t, kbdPassword, p, sshtest.WithKeyboardInteractive("alice", both)); err != nil {
		t.Fatal(err)
	}
	if asks := p.prompts(); len(asks) != 1 || len(asks[0].Questions) != 2 {
		t.Fatalf("prompts = %+v, want both questions shown to the user", asks)
	}
}

func TestSEC_SESSION_002_EchoedPasswordNotAutoAnswered(t *testing.T) {
	echoed := sshtest.Challenge{Questions: []sshtest.Question{{Prompt: "Password: ", Echo: true, Answer: kbdPassword}}}
	p := &scriptedPrompter{answer: answers(map[string]string{"Password": kbdPassword})}
	if _, err := kbdConnect(t, kbdPassword, p, sshtest.WithKeyboardInteractive("alice", echoed)); err != nil {
		t.Fatal(err)
	}
	if n := len(p.prompts()); n != 1 {
		t.Fatalf("%d prompts, want the echoed password prompt shown", n)
	}
}

func TestSEC_SESSION_002_OSC52Escaped(t *testing.T) {
	evil := sshtest.Challenge{
		Instruction: "Hello\x1b]52;c;ZWNobyBwd25lZA==\x07 there\x1b[2J",
		Questions:   []sshtest.Question{{Prompt: "\x1b[31mCode\x1b[0m: ", Echo: true, Answer: kbdOTP}},
	}
	p := &scriptedPrompter{answer: answers(map[string]string{"Code": kbdOTP})}
	if _, err := kbdConnect(t, "", p, sshtest.WithKeyboardInteractive("alice", evil)); err != nil {
		t.Fatal(err)
	}
	asks := p.prompts()
	if len(asks) != 1 {
		t.Fatalf("prompts = %+v", asks)
	}
	q := asks[0]
	for _, s := range []string{q.Instruction, q.Questions[0].Text, q.Name} {
		if strings.ContainsAny(s, "\x1b\x07") || strings.Contains(s, "]52;") {
			t.Fatalf("server text not sanitised: %q", s)
		}
	}
	if q.Instruction != "Hello there" || q.Questions[0].Text != "Code: " {
		t.Fatalf("prompt = %q / %q", q.Instruction, q.Questions[0].Text)
	}
}

func TestFR_CONN_003_ThreeFailures(t *testing.T) {
	p := &scriptedPrompter{answer: answers(nil)} // always wrong
	srv, err := kbdConnect(t, "", p, sshtest.WithPassword("alice", kbdPassword))
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("err = %v, want ErrAuthFailed", err)
	}
	asks := p.prompts()
	if len(asks) != MaxAuthFailures {
		t.Fatalf("%d prompts, want %d", len(asks), MaxAuthFailures)
	}
	if asks[0].Kind != PromptPassword || asks[0].Note != "" || asks[1].Note == "" {
		t.Fatalf("prompts = %+v", asks)
	}
	var pw int
	for _, a := range srv.AuthAttempts() {
		if a.Method == "password" {
			pw++
		}
	}
	if pw != MaxAuthFailures {
		t.Fatalf("server saw %d password attempts, want %d", pw, MaxAuthFailures)
	}
}

func TestFR_CONN_003_WrongStoredPasswordThenPrompt(t *testing.T) {
	p := &scriptedPrompter{answer: answers(map[string]string{"Password": kbdPassword})}
	srv := sshtest.New(t, sshtest.WithPassword("alice", kbdPassword))
	target := domain.Target{Hostname: srv.Host(), Port: srv.Port(), User: "alice", HasPassword: true}
	c, err := trusting(t, srv).Connect(context.Background(), target, pwCreds{password: "stale"}, p)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if asks := p.prompts(); len(asks) != 1 || asks[0].Note == "" {
		t.Fatalf("prompts = %+v, want one retry prompt", asks)
	}
}

func TestARCH_CONN_002_CancelIsErrAuthCancelled(t *testing.T) {
	for name, opt := range map[string]sshtest.Option{
		"keyboard-interactive": sshtest.WithKeyboardInteractive("alice", passwordRound, otpRound(false)),
		"password":             sshtest.WithPassword("alice", kbdPassword),
	} {
		p := &scriptedPrompter{} // cancels
		srv, err := kbdConnect(t, "", p, opt)
		if !errors.Is(err, ErrAuthCancelled) {
			t.Fatalf("%s: err = %v, want ErrAuthCancelled", name, err)
		}
		if n := len(p.prompts()); n != 1 {
			t.Fatalf("%s: %d prompts after a cancel, want 1", name, n)
		}
		for _, a := range srv.AuthAttempts() {
			if a.Success {
				t.Fatalf("%s: %+v", name, a)
			}
		}
	}
}
