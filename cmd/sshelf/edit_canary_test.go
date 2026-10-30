package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/secrets"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
	"github.com/RafaelXokito/sshelf/internal/ui/editor"
)

// TestSEC_MEM_008_M3_EditorArgsNoCanary is SEC-MEM-008 M3 revision AC3:
// after a connect with a canary password in the credential store, the
// editor command's arguments are exactly the words of $EDITOR plus the
// temp file's path, its Env is nil (the process environment, unchanged),
// and neither holds the canary. The command is built, never run.
func TestSEC_MEM_008_M3_EditorArgsNoCanary(t *testing.T) {
	const pw = "CANARY-editor-pw-7a3e91" //nolint:gosec // a made-up test secret
	cs := canaries{{"password", pw}}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "nano -w")

	srv := sshtest.New(t, sshtest.WithPassword("alice", pw))
	home := t.TempDir()
	cfgDir := filepath.Join(t.TempDir(), "sshelf")
	if _, err := paths.EnsureDir(cfgDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.KnownHostsFile(cfgDir), []byte(srv.KnownHostsLine()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := app.New(app.Config{Dir: cfgDir, KnownHostsFiles: paths.KnownHostsFiles(cfgDir, home), Secrets: secrets.NewMemStore(), Home: home})
	ctx := context.Background()
	if _, err := a.Load(ctx); err != nil {
		t.Fatal(err)
	}
	saved, err := a.SaveHostWithCredentials(ctx, domain.Host{Label: "pw", Hostname: srv.Host(), Port: srv.Port(), User: "alice"},
		app.HostCredentials{Password: app.NewSecret([]byte(pw))})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := a.Connect(ctx, saved.Host.ID, app.Size{Cols: 80, Rows: 22})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close(); <-sess.Done() }()

	path := filepath.Join(t.TempDir(), "sshelf-edit-1", "notes.txt")
	c := editor.Command(editor.Words(os.Getenv), path)
	if want := []string{"nano", "-w", path}; !slices.Equal(c.Args, want) {
		t.Fatalf("Args = %q, want %q", c.Args, want)
	}
	if c.Env != nil {
		t.Fatal("Env is set; the editor must inherit the environment unchanged")
	}
	if hits := append(cs.hits("editor args", []byte(strings.Join(c.Args, "\n"))), cs.envHits()...); len(hits) > 0 {
		t.Fatalf("canary found: %v", hits)
	}
}
