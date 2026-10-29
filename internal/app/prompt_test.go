package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/RafaelXokito/sshelf/internal/secrets"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// answerPrompts makes App answer every auth prompt with answer and
// records the prompts.
type answerPrompts struct {
	mu   sync.Mutex
	msgs []AuthPromptMsg
}

func (p *answerPrompts) install(a *App, answer func(AuthPromptMsg) []string) {
	a.SetSend(func(msg any) {
		m, ok := msg.(AuthPromptMsg)
		if !ok {
			panic("unexpected prompt")
		}
		p.mu.Lock()
		p.msgs = append(p.msgs, m)
		p.mu.Unlock()
		m.Answer(answer(m))
	})
}

func (p *answerPrompts) got() []AuthPromptMsg {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]AuthPromptMsg(nil), p.msgs...)
}

func TestSEC_CRED_007_MissingItemPromptsOnce(t *testing.T) {
	srv := sshtest.New(t, sshtest.WithPassword("deploy", testPassword))
	store := newRecStore() // has_password is set, but the store is empty (D-108)
	a := credApp(t, store, []*sshtest.Server{srv}, `[{"id": "i1", "name": "ops", "has_password": true}]`, oneHost)
	var p answerPrompts
	p.install(a, func(AuthPromptMsg) []string { return []string{testPassword} })
	for i := 1; i <= 2; i++ {
		connectOK(t, a, "h1")
		msgs := p.got()
		if len(msgs) != i {
			t.Fatalf("connect %d: %d prompts in total, want %d (one per connection)", i, len(msgs), i)
		}
		m := msgs[i-1]
		if m.Kind != PromptPassword || !strings.Contains(m.Note, "not set on this device") || m.User != "deploy" || m.Label != "web-prod" {
			t.Fatalf("prompt = %+v", m)
		}
	}
	if keys := store.Keys(); len(keys) != 0 {
		t.Fatalf("the typed password was stored: %v", keys)
	}
}

func TestFR_CONN_003_PassphrasePrompted(t *testing.T) {
	home := t.TempDir()
	k, pub := newStoredKey(t, testPassphrase)
	path := filepath.Join(home, "id_enc")
	if err := os.WriteFile(path, k, 0o600); err != nil {
		t.Fatal(err)
	}
	srv := sshtest.New(t, sshtest.WithAuthorizedKey("deploy", pub))
	a := credApp(t, newRecStore(), []*sshtest.Server{srv}, `[{"id": "i1", "name": "ops", "key_path": "`+path+`"}]`, oneHost)
	var p answerPrompts
	tries := 0
	p.install(a, func(AuthPromptMsg) []string {
		tries++
		if tries == 1 {
			return []string{"wrong"}
		}
		return []string{testPassphrase}
	})
	connectOK(t, a, "h1")
	msgs := p.got()
	if len(msgs) != 2 {
		t.Fatalf("%d prompts, want 2 (a wrong passphrase, then the right one)", len(msgs))
	}
	if m := msgs[0]; m.Kind != PromptPassphrase || m.Key != path || m.Note != "" || m.User != "deploy" {
		t.Fatalf("first prompt = %+v", m)
	}
	if m := msgs[1]; !strings.Contains(m.Note, "Wrong passphrase") {
		t.Fatalf("second prompt = %+v", m)
	}
}

func TestFR_CONN_008_PasswordPlusOTPEndToEnd(t *testing.T) {
	srv := sshtest.New(t, sshtest.WithKeyboardInteractive("deploy",
		sshtest.Challenge{Questions: []sshtest.Question{{Prompt: "Password: ", Answer: testPassword}}},
		sshtest.Challenge{Instruction: "2FA", Questions: []sshtest.Question{{Prompt: "OTP: ", Echo: true, Answer: "424242"}}},
	))
	store := newRecStore()
	store.put(t, "i1", secrets.FieldPassword, []byte(testPassword))
	a := credApp(t, store, []*sshtest.Server{srv}, `[{"id": "i1", "name": "ops", "has_password": true}]`, oneHost)
	var p answerPrompts
	p.install(a, func(AuthPromptMsg) []string { return []string{"424242"} })
	s, err := a.Connect(context.Background(), "h1")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	msgs := p.got()
	if len(msgs) != 1 || msgs[0].Kind != PromptKeyboardInteractive || msgs[0].Questions[0].Text != "OTP: " || !msgs[0].Questions[0].Echo {
		t.Fatalf("prompts = %+v, want only the OTP", msgs)
	}
}
