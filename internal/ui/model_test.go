package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/testutil"
)

func TestMain(m *testing.M) { testutil.VerifyTestMain(m) }

func press(m tea.Model, code rune, text string) (tea.Model, tea.Cmd) {
	return m.Update(tea.KeyPressMsg{Code: code, Text: text})
}

func TestModelNavigatesAndQuits(t *testing.T) {
	var m tea.Model = New([]domain.Host{
		{Label: "web-prod", Hostname: "10.0.0.10", User: "ubuntu"},
		{Label: "db-staging", Hostname: "10.0.1.20", Port: 2222, User: "admin"},
	})

	view := m.View().Content
	for _, want := range []string{"> web-prod", "ubuntu@10.0.0.10:22", "admin@10.0.1.20:2222"} {
		if !strings.Contains(view, want) {
			t.Errorf("view does not contain %q:\n%s", want, view)
		}
	}

	m, _ = press(m, 'j', "j")
	m, _ = press(m, tea.KeyDown, "") // already at the bottom
	if got := m.(Model).cursor; got != 1 {
		t.Fatalf("cursor = %d after j, down; want 1", got)
	}
	m, _ = press(m, tea.KeyUp, "")
	if got := m.(Model).cursor; got != 0 {
		t.Fatalf("cursor = %d after up; want 0", got)
	}

	_, cmd := press(m, 'q', "q")
	if cmd == nil {
		t.Fatal("q returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q did not quit")
	}
}
