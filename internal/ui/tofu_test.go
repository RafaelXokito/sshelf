package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/tofu"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
)

const testFingerprint = "SHA256:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU"

// tofuModel returns the root model in a connection attempt to web-prod
// with the TOFU modal open, and the prompt's reply channel.
func tofuModel(t *testing.T) (tea.Model, chan bool) {
	t.Helper()
	var m tea.Model = New(&fakeService{hosts: testHosts})
	m, _ = m.Update(LoadedMsg{Hosts: testHosts})
	m, _ = m.Update(hostlist.ConnectRequestMsg{HostID: "h2", Label: "web-prod"}) // the connect cmd is not run
	reply := make(chan bool, 1)
	m, _ = m.Update(app.HostKeyPromptMsg{
		Host: "10.0.0.10", Port: 22, KeyType: "ssh-ed25519", Fingerprint: testFingerprint, Reply: reply,
	})
	if v := m.View().Content; !strings.Contains(v, "Unknown host key") || !strings.Contains(v, testFingerprint) {
		t.Fatalf("no TOFU modal:\n%s", v)
	}
	return m, reply
}

func press(m tea.Model, code rune, text string) (tea.Model, tea.Msg) {
	m, cmd := m.Update(tea.KeyPressMsg{Code: code, Text: text})
	if cmd == nil {
		return m, nil
	}
	msg := cmd()
	m, _ = m.Update(msg)
	return m, msg
}

func TestFR_KH_001_EnterAndEscCancel(t *testing.T) {
	for _, k := range []struct {
		name string
		code rune
	}{{"enter", tea.KeyEnter}, {"esc", tea.KeyEscape}} {
		t.Run(k.name, func(t *testing.T) {
			m, reply := tofuModel(t)
			m, msg := press(m, k.code, "")
			if done, ok := msg.(tofu.DoneMsg); !ok || done.Trusted {
				t.Fatalf("%s produced %#v, want DoneMsg{Trusted: false}", k.name, msg)
			}
			if trust := <-reply; trust {
				t.Fatalf("%s trusted the key", k.name)
			}
			root := m.(Model)
			if len(root.modals) != 1 || root.cancel == nil {
				t.Fatalf("after %s: %d modals, attempt running = %v; want only Connecting until the attempt ends",
					k.name, len(root.modals), root.cancel != nil)
			}
			// The attempt then fails with ErrHostKeyRejected.
			err := fmt.Errorf("ssh: handshake failed: %w", app.ErrHostKeyRejected)
			m, _ = m.Update(ConnectFailedMsg{attempt: root.attempt, HostID: "h2", Label: "web-prod", Err: err})
			v := m.View().Content
			if !strings.Contains(v, "web-prod: host key not trusted; connection cancelled") || len(m.(Model).modals) != 0 {
				t.Fatalf("view after the rejection:\n%s", v)
			}
		})
	}
}

func TestFR_KH_001_YTrusts(t *testing.T) {
	m, reply := tofuModel(t)
	_, msg := press(m, 'y', "y")
	if done, ok := msg.(tofu.DoneMsg); !ok || !done.Trusted {
		t.Fatalf("y produced %#v", msg)
	}
	if trust := <-reply; !trust {
		t.Fatal("y did not trust the key")
	}
}

func TestFR_KH_001_NoTrustOnceOption(t *testing.T) {
	m, reply := tofuModel(t)
	if v := strings.ToLower(m.View().Content); strings.Contains(v, "once") {
		t.Fatalf("the modal offers trust once:\n%s", v)
	}
	for _, k := range []string{"o", "t", "n", "Y", " "} {
		if _, msg := press(m, rune(k[0]), k); msg != nil {
			t.Fatalf("%q answered the prompt: %#v", k, msg)
		}
	}
	select {
	case v := <-reply:
		t.Fatalf("prompt answered %v", v)
	default:
	}
}

func TestARCH_CONN_003_TofuGolden(t *testing.T) {
	m, _ := tofuModel(t)
	golden(t, "tofu", m)
}

func TestTofuPromptAfterAttemptEnded(t *testing.T) {
	var m tea.Model = New(&fakeService{hosts: testHosts})
	m, _ = m.Update(LoadedMsg{Hosts: testHosts})
	reply := make(chan bool, 1)
	m, _ = m.Update(app.HostKeyPromptMsg{Host: "h", Port: 22, Reply: reply})
	if trust := <-reply; trust || len(m.(Model).modals) != 0 {
		t.Fatalf("stale prompt: trust = %v, modals = %d", trust, len(m.(Model).modals))
	}
}

// TestFR_KH_001_PromptAfterTrustSurvivesLateDone: once the key is
// trusted, authentication goes on and can open a password prompt before
// the TOFU modal's DoneMsg reaches Update. The late DoneMsg must not
// close that prompt.
func TestFR_KH_001_PromptAfterTrustSurvivesLateDone(t *testing.T) {
	m, reply := tofuModel(t)
	m, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if !<-reply {
		t.Fatal("y did not trust")
	}
	pw := make(chan app.AuthReply, 1)
	m, _ = m.Update(app.AuthPromptMsg{Kind: app.PromptPassword, Questions: []app.Question{{Text: "Password:"}},
		Label: "web-prod", User: "ubuntu", Host: "10.0.0.10", Reply: pw})
	if cmd != nil {
		m, _ = m.Update(cmd()) // the TOFU modal's DoneMsg, late
	}
	if v := m.View().Content; len(m.(Model).modals) != 2 || !strings.Contains(v, "Password for ubuntu@10.0.0.10") {
		t.Fatalf("%d modals, view:\n%s", len(m.(Model).modals), v)
	}
}
