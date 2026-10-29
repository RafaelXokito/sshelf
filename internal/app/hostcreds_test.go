package app

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/secrets"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// emptyCredApp is credApp with no identities or hosts, trusting srv.
func emptyCredApp(t *testing.T, store secrets.Store, srv *sshtest.Server) *App {
	t.Helper()
	a := credApp(t, store, nil, `[]`, `[]`)
	if err := os.WriteFile(a.cfg.KnownHostsFiles[0], []byte(srv.KnownHostsLine()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return a
}

// hiddenOf returns the hidden identity of the host with id.
func hiddenOf(t *testing.T, a *App, id domain.ID) (domain.Identity, bool) {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	i := slices.IndexFunc(a.data.Hosts, func(h domain.Host) bool { return h.ID == id })
	if i < 0 {
		t.Fatalf("no host %s", id)
	}
	return a.hiddenIdentity(a.data.Hosts[i])
}

// passwordHost saves host "web" on srv with user deploy and password pw
// typed in the host form.
func passwordHost(t *testing.T, a *App, srv *sshtest.Server, pw string) domain.Host {
	t.Helper()
	saved, err := a.SaveHostWithCredentials(context.Background(),
		domain.Host{Label: "web", Hostname: srv.Host(), Port: srv.Port(), User: "deploy"},
		HostCredentials{Password: secretOf(pw)})
	if err != nil {
		t.Fatal(err)
	}
	return saved.Host
}

func TestFR_HOST_010_FirstTimeUserConnectsWithPassword(t *testing.T) {
	srv := sshtest.New(t, sshtest.WithPassword("deploy", testPassword))
	store := newRecStore()
	a := emptyCredApp(t, store, srv)
	h := passwordHost(t, a, srv, testPassword)
	hidden, ok := hiddenOf(t, a, h.ID)
	if !ok || !hidden.HasPassword || hidden.Label != "web" || hidden.Username != "" {
		t.Fatalf("hidden identity %+v, %v", hidden, ok)
	}
	if got := refsWith(store.MemStore, hidden.ID); len(got) != 1 || got[0].Field != secrets.FieldPassword {
		t.Fatalf("store = %v", store.Keys())
	}
	failOnPrompt(t, a)
	connectOK(t, a, string(h.ID))
}

func TestFR_HOST_010_HiddenNotListed(t *testing.T) {
	srv := sshtest.New(t)
	a := emptyCredApp(t, newRecStore(), srv)
	passwordHost(t, a, srv, "pw")
	list, err := a.Identities(context.Background())
	if err != nil || len(list) != 0 {
		t.Fatalf("identities view = %+v, %v", list, err)
	}
	// Its name, the host's label, stays free for a shared identity.
	if _, err := a.SaveIdentity(context.Background(), IdentityInput{Name: "web"}); err != nil {
		t.Fatal(err)
	}
	// A host with nothing typed gets no hidden identity.
	saved, err := a.SaveHostWithCredentials(context.Background(), domain.Host{Label: "bare", Hostname: "h"}, HostCredentials{})
	if err != nil || saved.Host.IdentityID != "" {
		t.Fatalf("bare host %+v, %v", saved.Host, err)
	}
}

func TestFR_HOST_010_EditUpdatesHidden(t *testing.T) {
	const newPassword = "canary-new-host-pw-77" //nolint:gosec // a made-up test secret
	srv := sshtest.New(t, sshtest.WithPassword("deploy", newPassword))
	store := newRecStore()
	a := emptyCredApp(t, store, srv)
	h := passwordHost(t, a, srv, "old")
	before, _ := hiddenOf(t, a, h.ID)

	h.Label = "web-renamed"
	saved, err := a.SaveHostWithCredentials(context.Background(), h, HostCredentials{Password: secretOf(newPassword), KeyPath: "~/.ssh/id_x"})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := hiddenOf(t, a, h.ID)
	if after.ID != before.ID || saved.Host.IdentityID != before.ID || after.Label != "web-renamed" || after.KeyPath != "~/.ssh/id_x" {
		t.Fatalf("hidden identity %+v -> %+v", before, after)
	}
	// An edit with nothing typed keeps everything, and reads nothing.
	gets := store.Calls(secrets.OpGet)
	if _, err := a.SaveHostWithCredentials(context.Background(), saved.Host, HostCredentials{KeyPath: "~/.ssh/id_x"}); err != nil {
		t.Fatal(err)
	}
	if kept, _ := hiddenOf(t, a, h.ID); !kept.HasPassword || store.Calls(secrets.OpGet) != gets {
		t.Fatalf("kept %+v", kept)
	}
	failOnPrompt(t, a)
	connectOK(t, a, string(h.ID))
	// Clearing the password deletes its item.
	if _, err := a.SaveHostWithCredentials(context.Background(), saved.Host, HostCredentials{ClearPassword: true}); err != nil {
		t.Fatal(err)
	}
	if cleared, _ := hiddenOf(t, a, h.ID); cleared.HasPassword || len(refsWith(store.MemStore, h.IdentityID)) != 0 {
		t.Fatalf("cleared %+v, store %v", cleared, store.Keys())
	}
}

func TestFR_HOST_010_SwitchToSharedDeletesHidden(t *testing.T) {
	srv := sshtest.New(t)
	store := newRecStore()
	a := emptyCredApp(t, store, srv)
	h := passwordHost(t, a, srv, "pw")
	hidden, _ := hiddenOf(t, a, h.ID)
	shared, err := a.SaveIdentity(context.Background(), IdentityInput{Name: "ops", Password: secretOf("shared")})
	if err != nil {
		t.Fatal(err)
	}
	h.IdentityID = shared.Identity.ID
	saved, err := a.SaveHostWithCredentials(context.Background(), h, HostCredentials{})
	if err != nil || len(saved.Warnings) != 0 {
		t.Fatalf("switch: %+v, %v", saved, err)
	}
	if saved.Host.IdentityID != shared.Identity.ID || slices.ContainsFunc(saved.Identities, func(i domain.Identity) bool { return i.ID == hidden.ID }) {
		t.Fatalf("after switch: %+v", saved)
	}
	if refs := refsWith(store.MemStore, hidden.ID); len(refs) != 0 {
		t.Fatalf("hidden secrets left: %v", refs)
	}
	if refs := refsWith(store.MemStore, shared.Identity.ID); len(refs) != 1 {
		t.Fatalf("shared secrets: %v", refs)
	}
}

func TestFR_HOST_010_InvalidHostStoresNothing(t *testing.T) {
	store := secrets.NewMemStore()
	a, _ := storeApp(t, store)
	_, err := a.SaveHostWithCredentials(context.Background(), domain.Host{Label: "", Hostname: "h"}, HostCredentials{Password: secretOf("pw")})
	if _, ok := errors.AsType[*domain.ValidationError](err); !ok || store.Calls(secrets.OpSet) != 0 {
		t.Fatalf("err = %v, %d sets", err, store.Calls(secrets.OpSet))
	}
	// A store failure saves no host either.
	store.FailNext(secrets.OpSet, secrets.ErrDenied)
	_, err = a.SaveHostWithCredentials(context.Background(), domain.Host{Label: "x", Hostname: "h"}, HostCredentials{Password: secretOf("pw")})
	if !errors.Is(err, secrets.ErrDenied) {
		t.Fatalf("err = %v", err)
	}
	if l, _ := a.Identities(context.Background()); len(l) != 0 || len(a.data.Hosts) != 0 {
		t.Fatalf("saved something: %+v %+v", l, a.data.Hosts)
	}
}

func TestFR_HOST_002_HiddenIdentityDeletedWithSecrets(t *testing.T) {
	srv := sshtest.New(t)
	store := newRecStore()
	a := emptyCredApp(t, store, srv)
	h := passwordHost(t, a, srv, "pw")
	hidden, _ := hiddenOf(t, a, h.ID)
	del, err := a.DeleteHost(context.Background(), h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(del.Hosts) != 0 || len(del.Identities) != 0 || len(store.Keys()) != 0 || len(del.Warnings) != 0 {
		t.Fatalf("after delete: %+v, store %v", del, store.Keys())
	}
	// A failed item delete is named in a warning.
	h = passwordHost(t, a, srv, "pw")
	hidden, _ = hiddenOf(t, a, h.ID)
	store.FailNext(secrets.OpDelete, secrets.ErrDenied)
	del, err = a.DeleteHost(context.Background(), h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(del.Warnings) != 1 || !strings.Contains(del.Warnings[0], `"sshelf" / "`+string(hidden.ID)+`/password"`) {
		t.Fatalf("warnings = %q", del.Warnings)
	}
}
