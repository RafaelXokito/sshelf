package app

import (
	"context"

	"github.com/RafaelXokito/sshelf/internal/sshclient"
)

// Authentication errors, re-exported from sshclient for the UI.
var (
	// ErrAuthCancelled means the user cancelled an authentication prompt
	// (ARCH-CONN-002).
	ErrAuthCancelled = sshclient.ErrAuthCancelled
	// ErrAuthFailed means the server rejected every credential, or the
	// user failed three times (FR-CONN-003).
	ErrAuthFailed = sshclient.ErrAuthFailed
)

type (
	// PromptKind says what an AuthPromptMsg asks for.
	PromptKind = sshclient.PromptKind
	// Question is one field of an AuthPromptMsg.
	Question = sshclient.Question
)

// The prompt kinds.
const (
	PromptPassword            = sshclient.PromptPassword
	PromptPassphrase          = sshclient.PromptPassphrase
	PromptKeyboardInteractive = sshclient.PromptKeyboardInteractive
)

// AuthPromptMsg asks the UI for a password, a key passphrase or the
// answers to a server's keyboard-interactive questions (FR-CONN-003,
// FR-CONN-008), while the connection waits. App sends it with the function
// given to SetSend. Server-supplied text in it is already sanitised
// (SEC-SESSION-003); it carries no secret. The UI replies once, with
// Answer or Cancel, neither of which blocks.
type AuthPromptMsg struct {
	Kind        PromptKind
	Label       string // the host's label
	User        string
	Host        string
	Key         string // PromptPassphrase: which key
	Name        string // keyboard-interactive name, from the server
	Instruction string // keyboard-interactive instruction, from the server
	Questions   []Question
	Note        string // sshelf's own hint, e.g. "not set on this device"
	Reply       chan<- AuthReply
}

// AuthReply is the UI's answer to an AuthPromptMsg.
type AuthReply struct {
	Answers   []string // one per question
	Cancelled bool
}

// Answer sends the user's answers. Only the first reply counts.
func (m AuthPromptMsg) Answer(answers []string) { m.reply(AuthReply{Answers: answers}) }

// Cancel tells the connection that the user cancelled.
func (m AuthPromptMsg) Cancel() { m.reply(AuthReply{Cancelled: true}) }

func (m AuthPromptMsg) reply(r AuthReply) {
	select {
	case m.Reply <- r:
	default:
	}
}

// Ask implements sshclient.Prompter (design §8.3): it shows the prompt
// and waits for the answer or for ctx to end. Without SetSend every
// prompt is cancelled.
func (a *App) Ask(ctx context.Context, p sshclient.AuthPrompt) ([]string, error) {
	a.mu.Lock()
	send := a.send
	a.mu.Unlock()
	if send == nil {
		return nil, ErrAuthCancelled
	}
	reply := make(chan AuthReply, 1)
	send(AuthPromptMsg{
		Kind: p.Kind, Label: p.Label, User: p.User, Host: p.Host, Key: p.Key,
		Name: p.Name, Instruction: p.Instruction, Questions: p.Questions, Note: p.Note,
		Reply: reply,
	})
	select {
	case r := <-reply:
		if r.Cancelled {
			return nil, ErrAuthCancelled
		}
		return r.Answers, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
