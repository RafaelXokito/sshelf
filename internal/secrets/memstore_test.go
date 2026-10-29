package secrets

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

const testID = "0192f0a4-7c1e-7b3a-9d2f-3c4b5a6e7f80"

func TestSEC_CRED_002_ItemNames(t *testing.T) {
	ctx := context.Background()
	m := NewMemStore()
	for _, f := range []Field{FieldPassword, FieldPrivateKey, FieldKeyPassphrase} {
		if err := m.Set(ctx, Ref{IdentityID: testID, Field: f}, NewSecret([]byte("v-"+string(f)))); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for _, r := range m.Keys() {
		got = append(got, r.Account())
	}
	want := []string{testID + "/key-passphrase", testID + "/password", testID + "/private-key"}
	if !slices.Equal(got, want) {
		t.Fatalf("accounts = %q, want %q", got, want)
	}
	for _, a := range want {
		r, err := ParseRef(a)
		if err != nil || r.Account() != a || r.IdentityID != testID {
			t.Errorf("ParseRef(%q) = %+v, %v", a, r, err)
		}
	}
}

func TestParseRef_Invalid(t *testing.T) {
	for _, a := range []string{
		"", "nofield", "/password", testID + "/", testID + "/passwd",
		testID + "/password/x", "a\x00b/password", "a\x7fb/password",
	} {
		if r, err := ParseRef(a); err == nil {
			t.Errorf("ParseRef(%q) = %+v, want error", a, r)
		}
	}
}

func FuzzParseRef(f *testing.F) {
	for _, s := range []string{testID + "/password", testID + "/private-key", "x/key-passphrase", "", "/", "a/b/c", "\x00/password"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, account string) {
		r, err := ParseRef(account)
		if err != nil {
			if r != (Ref{}) {
				t.Fatalf("error with non-zero Ref %+v", r)
			}
			return
		}
		if r.Account() != account {
			t.Fatalf("round trip: %q -> %+v -> %q", account, r, r.Account())
		}
		if r.IdentityID == "" || strings.Contains(r.IdentityID, "/") || !r.Field.valid() {
			t.Fatalf("accepted invalid ref %+v", r)
		}
		if utf8.ValidString(account) && strings.ContainsFunc(r.IdentityID, func(c rune) bool { return c < 0x20 || c == 0x7f }) {
			t.Fatalf("accepted control character in %q", account)
		}
	})
}

func TestSEC_CRED_003_SizeBoundary(t *testing.T) {
	ctx := context.Background()
	m := NewMemStore()
	ref := Ref{IdentityID: testID, Field: FieldPrivateKey}
	ok := bytes.Repeat([]byte("k"), MaxSecretSize)
	if err := m.Set(ctx, ref, NewSecret(ok)); err != nil {
		t.Fatalf("Set(%d bytes) = %v", MaxSecretSize, err)
	}
	big := bytes.Repeat([]byte("k"), MaxSecretSize+1)
	if err := m.Set(ctx, ref, NewSecret(big)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Set(%d bytes) = %v, want ErrTooLarge", MaxSecretSize+1, err)
	}
	got, err := m.Get(ctx, ref)
	if err != nil || got.Len() != MaxSecretSize {
		t.Fatalf("after refused Set, Get = %d bytes, %v; want the 2800-byte value kept", got.Len(), err)
	}
}

func TestMemStore_RoundTripAndOwnership(t *testing.T) {
	ctx := context.Background()
	m := NewMemStore()
	ref := Ref{IdentityID: testID, Field: FieldPassword}

	if _, err := m.Get(ctx, ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing = %v, want ErrNotFound", err)
	}
	in := NewSecret([]byte("pw1"))
	if err := m.Set(ctx, ref, in); err != nil {
		t.Fatal(err)
	}
	in.Destroy() // the store keeps its own copy
	out, err := m.Get(ctx, ref)
	if err != nil || string(out.Bytes()) != "pw1" {
		t.Fatalf("Get = %q, %v", out.Bytes(), err)
	}
	out.Destroy() // the caller's copy does not affect the store
	again, err := m.Get(ctx, ref)
	if err != nil || string(again.Bytes()) != "pw1" {
		t.Fatalf("second Get = %q, %v", again.Bytes(), err)
	}

	if err := m.Set(ctx, ref, NewSecret([]byte("pw2"))); err != nil {
		t.Fatal(err)
	}
	if s, _ := m.Get(ctx, ref); string(s.Bytes()) != "pw2" {
		t.Fatalf("overwrite: Get = %q", s.Bytes())
	}
	if err := m.Delete(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, ref); err != nil {
		t.Fatalf("Delete absent = %v, want nil", err)
	}
	if len(m.Keys()) != 0 {
		t.Fatalf("Keys = %v after Delete", m.Keys())
	}
	if err := m.Available(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestMemStore_InvalidRef(t *testing.T) {
	ctx := context.Background()
	m := NewMemStore()
	bad := Ref{IdentityID: "", Field: FieldPassword}
	if _, err := m.Get(ctx, bad); err == nil {
		t.Error("Get accepted invalid ref")
	}
	if err := m.Set(ctx, bad, NewSecret([]byte("x"))); err == nil {
		t.Error("Set accepted invalid ref")
	}
	if err := m.Delete(ctx, Ref{IdentityID: testID, Field: "other"}); err == nil {
		t.Error("Delete accepted invalid ref")
	}
	if len(m.Keys()) != 0 {
		t.Fatal("invalid ref stored")
	}
}

func TestMemStore_FailNextAndCalls(t *testing.T) {
	ctx := context.Background()
	m := NewMemStore()
	ref := Ref{IdentityID: testID, Field: FieldPassword}
	if err := m.Set(ctx, ref, NewSecret([]byte("pw"))); err != nil {
		t.Fatal(err)
	}

	m.FailNext(OpGet, ErrDenied)
	m.FailNext(OpGet, ErrUnavailable)
	m.FailNext(OpSet, ErrUnavailable)
	m.FailNext(OpDelete, ErrDenied)
	m.FailNext(OpAvailable, ErrUnavailable)

	if _, err := m.Get(ctx, ref); !errors.Is(err, ErrDenied) {
		t.Errorf("Get 1 = %v, want ErrDenied", err)
	}
	if _, err := m.Get(ctx, ref); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Get 2 = %v, want ErrUnavailable", err)
	}
	if _, err := m.Get(ctx, ref); err != nil {
		t.Errorf("Get 3 = %v, want success", err)
	}
	if err := m.Set(ctx, ref, NewSecret([]byte("new"))); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Set = %v", err)
	}
	if s, _ := m.Get(ctx, ref); string(s.Bytes()) != "pw" {
		t.Errorf("failed Set changed the value to %q", s.Bytes())
	}
	if err := m.Delete(ctx, ref); !errors.Is(err, ErrDenied) {
		t.Errorf("Delete = %v", err)
	}
	if len(m.Keys()) != 1 {
		t.Error("failed Delete removed the item")
	}
	if err := m.Available(ctx); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Available = %v", err)
	}
	if err := m.Available(ctx); err != nil {
		t.Errorf("Available after injected failure = %v", err)
	}

	for op, want := range map[string]int{OpGet: 4, OpSet: 2, OpDelete: 1, OpAvailable: 2} {
		if got := m.Calls(op); got != want {
			t.Errorf("Calls(%q) = %d, want %d", op, got, want)
		}
	}
}

func TestMemStore_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := NewMemStore()
	ref := Ref{IdentityID: testID, Field: FieldPassword}
	if _, err := m.Get(ctx, ref); !errors.Is(err, context.Canceled) {
		t.Errorf("Get = %v", err)
	}
	if err := m.Set(ctx, ref, NewSecret([]byte("x"))); !errors.Is(err, context.Canceled) {
		t.Errorf("Set = %v", err)
	}
	if err := m.Delete(ctx, ref); !errors.Is(err, context.Canceled) {
		t.Errorf("Delete = %v", err)
	}
	if err := m.Available(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Available = %v", err)
	}
	if len(m.Keys()) != 0 {
		t.Error("Set with cancelled context stored a value")
	}
}

func TestMemStore_UnknownOpPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("FailNext with unknown op did not panic")
		}
	}()
	NewMemStore().FailNext("list", ErrDenied)
}
