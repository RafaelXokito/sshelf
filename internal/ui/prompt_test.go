package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/prompt"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
)

// promptModel returns the root model in a connection attempt to web-prod
// with the prompt p open, and p's reply channel.
func promptModel(t *testing.T, p app.AuthPromptMsg) (tea.Model, chan app.AuthReply) {
	t.Helper()
	var m tea.Model = New(&fakeService{hosts: testHosts})
	m, _ = m.Update(LoadedMsg{Hosts: testHosts})
	m, _ = m.Update(hostlist.ConnectRequestMsg{HostID: "h2", Label: "web-prod"}) // the connect cmd is not run
	reply := make(chan app.AuthReply, 1)
	p.Label, p.User, p.Host, p.Reply = "web-prod", "ubuntu", "10.0.0.10", reply
	m, _ = m.Update(p)
	if n := len(m.(Model).modals); n != 2 {
		t.Fatalf("%d modals, want Connecting and the prompt", n)
	}
	return m, reply
}

func typeText(m tea.Model, s string) tea.Model {
	for _, r := range s {
		m, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

var passwordPrompt = app.AuthPromptMsg{Kind: app.PromptPassword, Questions: []app.Question{{Text: "Password:"}}}

func TestFR_CONN_003_PromptShowsUserHostAndKind(t *testing.T) {
	m, _ := promptModel(t, passwordPrompt)
	if v := m.View().Content; !strings.Contains(v, "Password for ubuntu@10.0.0.10") {
		t.Fatalf("password prompt:\n%s", v)
	}
	m, _ = promptModel(t, app.AuthPromptMsg{
		Kind: app.PromptPassphrase, Key: "/home/u/.ssh/id_ed25519", Questions: []app.Question{{Text: "Passphrase:"}},
		Note: "The stored passphrase is not set on this device.",
	})
	v := m.View().Content
	for _, want := range []string{"Passphrase for /home/u/.ssh/id_ed25519", "ubuntu@10.0.0.10", "not set on this device"} {
		if !strings.Contains(v, want) {
			t.Fatalf("passphrase prompt lacks %q:\n%s", want, v)
		}
	}
}

func TestFR_CONN_003_TypedSecretNotShown(t *testing.T) {
	const secret = "canary-typed-secret" //nolint:gosec // a made-up test secret
	m, reply := promptModel(t, passwordPrompt)
	m = typeText(m, secret)
	m, _ = m.Update(tea.PasteMsg{Content: "+pasted"})
	if v := m.View().Content; strings.Contains(v, "canary") || strings.Contains(v, "pasted") || strings.Contains(v, "*") || strings.Contains(v, "•••") {
		t.Fatal("the prompt shows the typed secret or a mask")
	}
	m, msg := press(m, tea.KeyEnter, "")
	if done, ok := msg.(prompt.DoneMsg); !ok || done.Cancelled {
		t.Fatalf("enter produced %#v", msg)
	}
	r := <-reply
	if r.Cancelled || !slices.Equal(r.Answers, []string{secret + "+pasted"}) {
		t.Fatal("wrong answer delivered")
	}
	if n := len(m.(Model).modals); n != 1 {
		t.Fatalf("%d modals after submit, want Connecting only", n)
	}
}

func TestFR_CONN_003_EscReturnsToList(t *testing.T) {
	m, reply := promptModel(t, passwordPrompt)
	m = typeText(m, "abc")
	m, msg := press(m, tea.KeyEscape, "")
	if done, ok := msg.(prompt.DoneMsg); !ok || !done.Cancelled {
		t.Fatalf("esc produced %#v", msg)
	}
	if r := <-reply; !r.Cancelled {
		t.Fatal("esc did not cancel")
	}
	// The attempt then fails with ErrAuthCancelled.
	root := m.(Model)
	err := fmt.Errorf("ssh: handshake failed: %w", app.ErrAuthCancelled)
	m, _ = m.Update(ConnectFailedMsg{attempt: root.attempt, HostID: "h2", Label: "web-prod", Err: err})
	v := m.View().Content
	if len(m.(Model).modals) != 0 || !strings.Contains(v, "web-prod: authentication cancelled") || !strings.Contains(v, "enter connect") {
		t.Fatalf("after esc:\n%s", v)
	}
}

func TestFR_CONN_003_AuthFailedStatus(t *testing.T) {
	m, _ := promptModel(t, passwordPrompt)
	err := fmt.Errorf("ssh: handshake failed: %w after 3 attempts", app.ErrAuthFailed)
	m, _ = m.Update(ConnectFailedMsg{attempt: m.(Model).attempt, HostID: "h2", Label: "web-prod", Err: err})
	if v := m.View().Content; !strings.Contains(v, "web-prod: authentication failed") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestFR_CONN_008_TabMovesBetweenFields(t *testing.T) {
	m, reply := promptModel(t, app.AuthPromptMsg{Kind: app.PromptKeyboardInteractive, Questions: []app.Question{
		{Text: "Password:"}, {Text: "OTP:", Echo: true},
	}})
	m = typeText(m, "pw")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // next field
	m = typeText(m, "4242")
	if v := m.View().Content; !strings.Contains(v, "OTP: 4242") || strings.Contains(v, "pw") {
		t.Fatalf("view:\n%s", v)
	}
	m, _ = press(m, tea.KeyEnter, "")
	if r := <-reply; !slices.Equal(r.Answers, []string{"pw", "4242"}) {
		t.Fatalf("answers = %d", len(r.Answers))
	}
	_ = m
}

func TestARCH_SESS_002_KbdIntModalGolden(t *testing.T) {
	m, _ := promptModel(t, app.AuthPromptMsg{
		Kind:        app.PromptKeyboardInteractive,
		Instruction: "Duo two-factor login\n\x1b]52;c;ZXZpbA==\x07Enter a passcode",
		Questions:   []app.Question{{Text: "Passcode or option (1-3): ", Echo: true}},
	})
	m = typeText(m, "12")
	golden(t, "kbdint", m)
}

func TestAuthPromptAfterAttemptEnded(t *testing.T) {
	var m tea.Model = New(&fakeService{hosts: testHosts})
	m, _ = m.Update(LoadedMsg{Hosts: testHosts})
	reply := make(chan app.AuthReply, 1)
	m, _ = m.Update(app.AuthPromptMsg{Kind: app.PromptPassword, Reply: reply})
	if r := <-reply; !r.Cancelled || len(m.(Model).modals) != 0 {
		t.Fatalf("stale prompt: %+v, modals = %d", r, len(m.(Model).modals))
	}
}
