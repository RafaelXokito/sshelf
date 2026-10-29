package app

import (
	"context"
	"errors"
	"testing"

	"github.com/RafaelXokito/sshelf/internal/secrets"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

func TestSEC_CRED_006_DeniedNoRetryNoCache(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{secrets.ErrDenied, "Access to " + StoreName() + " was denied"},
	} {
		srv := sshtest.New(t, sshtest.WithPassword("deploy", testPassword))
		store := newRecStore()
		store.put(t, "i1", secrets.FieldPassword, []byte(testPassword))
		a := credApp(t, store, []*sshtest.Server{srv}, `[{"id": "i1", "name": "ops", "has_password": true}]`, oneHost)
		prompts := cancelPrompts(a)
		store.FailNext(secrets.OpGet, tc.err)
		s, err := a.Connect(context.Background(), "h1")
		if err == nil {
			_ = s.Close()
			t.Fatalf("%v: connected", tc.err)
		}
		se, ok := errors.AsType[*StoreError](err)
		if !ok || !errors.Is(err, tc.err) || se.Error() != tc.want {
			t.Fatalf("err = %v, want a StoreError %q", err, tc.want)
		}
		if n := store.Calls(secrets.OpGet); n != 1 {
			t.Fatalf("%v: %d Get calls, want 1 (no retry)", tc.err, n)
		}
		if n := prompts.Load(); n != 0 {
			t.Fatalf("%v: %d prompts, want none", tc.err, n)
		}
		// Nothing was cached: the next connect asks the store again.
		connectOK(t, a, "h1")
		if n := store.Calls(secrets.OpGet); n != 2 {
			t.Fatalf("%v: %d Get calls after the second connect, want 2", tc.err, n)
		}
	}
}

func TestFR_CONN_006_ProgressReachesUI(t *testing.T) {
	srv := sshtest.New(t, sshtest.WithPassword("deploy", testPassword))
	store := newRecStore()
	store.put(t, "i1", secrets.FieldPassword, []byte(testPassword))
	a := credApp(t, store, []*sshtest.Server{srv}, `[{"id": "i1", "name": "ops", "has_password": true}]`, oneHost)
	msgs := make(chan any, 16)
	a.SetSend(func(msg any) { msgs <- msg })
	connectOK(t, a, "h1")
	close(msgs)
	var stages []Stage
	for m := range msgs {
		if p, ok := m.(ConnectProgressMsg); ok && p.HostID == "h1" {
			stages = append(stages, p.Stage)
		}
	}
	if len(stages) != 3 || stages[0] != StageDialling || stages[2] != StageAuthenticating {
		t.Fatalf("stages = %v", stages)
	}
}
