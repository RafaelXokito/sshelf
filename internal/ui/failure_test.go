package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
)

// connectingModel returns the root model in a connection attempt to
// web-prod (the connect command is not run).
func connectingModel(t *testing.T) tea.Model {
	t.Helper()
	var m tea.Model = New(&fakeService{hosts: testHosts})
	m, _ = m.Update(LoadedMsg{Hosts: testHosts})
	m, _ = m.Update(hostlist.ConnectRequestMsg{HostID: "h2", Label: "web-prod"})
	return m
}

func failWith(m tea.Model, err error) tea.Model {
	m, _ = m.Update(ConnectFailedMsg{attempt: m.(Model).attempt, HostID: "h2", Label: "web-prod", Err: err})
	return m
}

// Each FR-CONN-006 cause has its own message and golden error toast.
func TestFR_CONN_006_ErrorToasts(t *testing.T) {
	for _, tc := range []struct {
		golden string
		err    error
		want   string
	}{
		{"fail-name", fmt.Errorf("%w: lookup db.invalid: no such host", app.ErrNameResolution), "web-prod: name resolution failed"},
		{"fail-refused", fmt.Errorf("%w: dial tcp: connect: connection refused", app.ErrConnectionRefused), "web-prod: connection refused"},
		{"fail-timeout", fmt.Errorf("%w after 10s", app.ErrTimedOut), "web-prod: timed out"},
		{"fail-hostkey", fmt.Errorf("ssh: handshake failed: %w", app.ErrHostKeyRejected), "web-prod: host key not trusted; connection cancelled"},
		{"fail-auth", fmt.Errorf("ssh: handshake failed: %w: ssh: unable to authenticate", app.ErrAuthFailed), "web-prod: authentication failed"},
		{"fail-denied", fmt.Errorf("ssh: handshake failed: %w", &app.StoreError{Store: "Keychain", Err: app.ErrStoreDenied}), "web-prod: Access to Keychain was denied"},
	} {
		t.Run(tc.golden, func(t *testing.T) {
			m := failWith(connectingModel(t), tc.err)
			v := m.View().Content
			if !strings.Contains(v, tc.want) || len(m.(Model).modals) != 0 {
				t.Fatalf("want %q and no modal:\n%s", tc.want, v)
			}
			golden(t, tc.golden, m)
		})
	}
}

func TestFR_CONN_006_StagesShown(t *testing.T) {
	m := connectingModel(t)
	for _, s := range []app.Stage{app.StageVerifying, app.StageAuthenticating, app.StageDialling} {
		m, _ = m.Update(app.ConnectProgressMsg{HostID: "h2", Stage: s})
	}
	m, _ = m.Update(app.ConnectProgressMsg{HostID: "other", Stage: app.StageDialling}) // another host: ignored
	v := m.View().Content
	if !strings.Contains(ansiSeq.ReplaceAllString(v, ""), "✓ dialling → ✓ verifying → › authenticating") {
		t.Fatalf("view:\n%s", v)
	}
	golden(t, "connecting", m)
}

func TestFR_CONN_006_ServerTextSanitised(t *testing.T) {
	m := failWith(connectingModel(t), errors.New("ssh: disconnect: \x1b]52;c;ZXZpbA==\x07bye\x1b[2J"))
	if v := m.View().Content; strings.Contains(v, "]52;") || strings.Contains(v, "\x07") || !strings.Contains(v, "web-prod: ssh: disconnect: bye") {
		t.Fatalf("view:\n%q", v)
	}
}

// cancelService is an app.Service whose Connect blocks until ctx ends.
type cancelService struct {
	*fakeService
	started chan struct{}
	ended   chan error
}

func (b *cancelService) Connect(ctx context.Context, _ domain.ID, _ app.Size) (app.Session, error) {
	close(b.started)
	<-ctx.Done()
	b.ended <- ctx.Err()
	return nil, ctx.Err()
}

func TestARCH_CONN_005_EscCancelsPendingConnection(t *testing.T) {
	svc := &cancelService{fakeService: &fakeService{hosts: testHosts}, started: make(chan struct{}), ended: make(chan error, 1)}
	var m tea.Model = New(svc)
	m, _ = m.Update(LoadedMsg{Hosts: testHosts})
	m, cmd := m.Update(hostlist.ConnectRequestMsg{HostID: "h2", Label: "web-prod"})
	results := make(chan tea.Msg, 1)
	go func() { results <- cmd() }()
	<-svc.started
	start := time.Now()
	m, _ = press(m, tea.KeyEscape, "")
	if len(m.(Model).modals) != 0 || !strings.Contains(m.View().Content, "connection cancelled") {
		t.Fatalf("after esc:\n%s", m.View().Content)
	}
	select {
	case err := <-svc.ended:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("connect ended with %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("the connection attempt was not cancelled within 200 ms")
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("cancel took %v", d)
	}
	// The late failure of the cancelled attempt changes nothing.
	m, _ = m.Update(<-results)
	if !strings.Contains(m.View().Content, "connection cancelled") {
		t.Fatalf("after the late failure:\n%s", m.View().Content)
	}
}
