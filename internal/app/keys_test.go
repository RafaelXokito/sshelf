package app

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/secrets"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// keySecret is a new unencrypted Ed25519 key in OpenSSH form.
func keySecret(t *testing.T) Secret {
	t.Helper()
	key, _ := newStoredKey(t, "")
	return NewSecret(key)
}

// encodeKey returns k as PEM in format "openssh", "pem" (PKCS#1 for RSA,
// SEC 1 for ECDSA; Ed25519 has no such form, so PKCS#8) or "pkcs8".
func encodeKey(t *testing.T, k crypto.Signer, format string) []byte {
	t.Helper()
	var (
		block *pem.Block
		err   error
	)
	switch format {
	case "openssh":
		block, err = ssh.MarshalPrivateKey(k, "")
	case "pem":
		switch k := k.(type) {
		case *rsa.PrivateKey:
			block = &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}
		case *ecdsa.PrivateKey:
			var der []byte
			der, err = x509.MarshalECPrivateKey(k)
			block = &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}
		default:
			return encodeKey(t, k, "pkcs8")
		}
	case "pkcs8":
		var der []byte
		der, err = x509.MarshalPKCS8PrivateKey(k)
		block = &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	}
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(block)
}

func publicOf(t *testing.T, k crypto.Signer) ssh.PublicKey {
	t.Helper()
	s, err := ssh.NewSignerFromKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return s.PublicKey()
}

func keyFieldError(err error) string {
	ve, ok := errors.AsType[*domain.ValidationError](err)
	if !ok {
		return ""
	}
	return ve.Fields[domain.FieldKey] + ve.Fields[domain.FieldKeyPath]
}

func TestFR_IDENT_003_FormatsAccepted(t *testing.T) {
	store := secrets.NewMemStore()
	a, _ := storeApp(t, store)
	ctx := context.Background()
	for _, kind := range []string{sshtest.Ed25519, sshtest.ECDSA, sshtest.RSA} {
		k, err := sshtest.GenerateKey(kind)
		if err != nil {
			t.Fatal(err)
		}
		for _, format := range []string{"openssh", "pem", "pkcs8"} {
			pemBytes := encodeKey(t, k, format)
			saved, err := a.SaveIdentity(ctx, IdentityInput{Name: kind + "-" + format, Key: NewSecret(bytes.Clone(pemBytes))})
			if err != nil {
				t.Fatalf("%s %s: %v", kind, format, err)
			}
			got, err := store.Get(ctx, secrets.Ref{IdentityID: string(saved.Identity.ID), Field: secrets.FieldPrivateKey})
			if err != nil || !bytes.Equal(got.Bytes(), pemBytes) {
				t.Fatalf("%s %s: the key is not stored as pasted (%v)", kind, format, err)
			}
			got.Destroy()
			if want := publicOf(t, k); !strings.HasPrefix(saved.Identity.PublicKey, want.Type()+" ") {
				t.Fatalf("%s %s: public key %q", kind, format, saved.Identity.PublicKey)
			}
		}
	}

	// Encrypted keys are accepted. The OpenSSH format shows its public
	// part; a legacy encrypted PEM key needs the passphrase for that.
	k, _ := sshtest.GenerateKey(sshtest.RSA)
	block, err := ssh.MarshalPrivateKeyWithPassphrase(k, "", []byte(testPassphrase))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := a.SaveIdentity(ctx, IdentityInput{Name: "enc-openssh", Key: NewSecret(pem.EncodeToMemory(block))})
	if err != nil || saved.Identity.PublicKey == "" {
		t.Fatalf("encrypted OpenSSH: %+v, %v", saved.Identity, err)
	}
	legacy, err := x509.EncryptPEMBlock(rand.Reader, "RSA PRIVATE KEY", //nolint:staticcheck // a legacy format users still have
		x509.MarshalPKCS1PrivateKey(k.(*rsa.PrivateKey)), []byte(testPassphrase), x509.PEMCipherAES256)
	if err != nil {
		t.Fatal(err)
	}
	legacyPEM := pem.EncodeToMemory(legacy)
	saved, err = a.SaveIdentity(ctx, IdentityInput{Name: "enc-pem", Key: NewSecret(bytes.Clone(legacyPEM))})
	if err != nil || saved.Identity.PublicKey != "" || !saved.Identity.HasKey {
		t.Fatalf("encrypted PEM without passphrase: %+v, %v", saved.Identity, err)
	}
	saved, err = a.SaveIdentity(ctx, IdentityInput{Name: "enc-pem-2", Key: NewSecret(bytes.Clone(legacyPEM)), Passphrase: secretOf(testPassphrase)})
	if err != nil || saved.Identity.PublicKey == "" || !saved.Identity.HasPassphrase {
		t.Fatalf("encrypted PEM with passphrase: %+v, %v", saved.Identity, err)
	}
	_, err = a.SaveIdentity(ctx, IdentityInput{Name: "enc-pem-3", Key: NewSecret(bytes.Clone(legacyPEM)), Passphrase: secretOf("wrong")})
	if keyFieldError(err) != MsgKeyPassphrase {
		t.Fatalf("wrong passphrase: %v", err)
	}
}

func TestFR_IDENT_003_InvalidPasteRejected(t *testing.T) {
	store := secrets.NewMemStore()
	a, _ := storeApp(t, store)
	key, pub := newStoredKey(t, "")
	for name, paste := range map[string][]byte{
		"text":       []byte("hello"),
		"public key": ssh.MarshalAuthorizedKey(pub),
		"truncated":  key[:len(key)/2],
		"bad base64": []byte("-----BEGIN OPENSSH PRIVATE KEY-----\n!!!\n-----END OPENSSH PRIVATE KEY-----\n"),
	} {
		_, err := a.SaveIdentity(context.Background(), IdentityInput{Name: "x", Key: NewSecret(paste)})
		if keyFieldError(err) != MsgKeyInvalid {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	if n := store.Calls(secrets.OpSet); n != 0 || len(store.Keys()) != 0 {
		t.Fatalf("%d Set calls for invalid keys", n)
	}
	if list, _ := a.Identities(context.Background()); len(list) != 0 {
		t.Fatalf("saved %+v", list)
	}
}

func TestFR_IDENT_003_ShowsTypeAndFingerprint(t *testing.T) {
	a, _ := storeApp(t, secrets.NewMemStore())
	for kind, want := range map[string]string{sshtest.Ed25519: "ED25519", sshtest.ECDSA: "ECDSA", sshtest.RSA: "RSA"} {
		k, _ := sshtest.GenerateKey(kind)
		if _, err := a.SaveIdentity(context.Background(), IdentityInput{Name: kind, Key: NewSecret(encodeKey(t, k, "openssh"))}); err != nil {
			t.Fatal(err)
		}
		list, _ := a.Identities(context.Background())
		for _, info := range list {
			if info.Identity.Label != kind {
				continue
			}
			if info.KeyType != want || info.KeyFingerprint != ssh.FingerprintSHA256(publicOf(t, k)) || info.Key != SecretSet {
				t.Fatalf("%s: %+v", kind, info)
			}
		}
	}
}

func TestFR_IDENT_003_ImportThenDeleteFileStillConnects(t *testing.T) {
	store := newRecStore()
	a := credApp(t, store, nil, `[]`, `[]`)
	keyPath := filepath.Join(a.cfg.Home, ".ssh", "id_test")
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	pub, err := sshtest.WriteKeyFile(keyPath, sshtest.Ed25519, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	srv := sshtest.New(t, sshtest.WithAuthorizedKey("deploy", pub))
	if err := os.WriteFile(a.cfg.KnownHostsFiles[0], []byte(srv.KnownHostsLine()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(keyPath) //nolint:gosec // a test temp dir
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// A key file others can read is refused (SEC-FILE-009).
	if err := os.Chmod(keyPath, 0o644); err != nil { //nolint:gosec // an unprotected key, on purpose
		t.Fatal(err)
	}
	_, err = a.SaveIdentity(ctx, IdentityInput{Name: "ops", Username: "deploy", KeyPath: "~/.ssh/id_test", ImportKeyFile: true})
	if !strings.Contains(keyFieldError(err), "Unprotected private key file") {
		t.Fatalf("0644 import: %v", err)
	}
	if err := os.Chmod(keyPath, 0o600); err != nil {
		t.Fatal(err)
	}

	saved, err := a.SaveIdentity(ctx, IdentityInput{Name: "ops", Username: "deploy", KeyPath: "~/.ssh/id_test", ImportKeyFile: true})
	if err != nil {
		t.Fatal(err)
	}
	if id := saved.Identity; id.KeyPath != "" || !id.HasKey || id.PublicKey == "" {
		t.Fatalf("imported identity %+v", id)
	}
	after, err := os.ReadFile(keyPath) //nolint:gosec // a test temp dir
	if fi, _ := os.Stat(keyPath); err != nil || !bytes.Equal(after, original) || fi.Mode().Perm() != 0o600 {
		t.Fatal("the key file changed")
	}
	host, err := a.SaveHost(ctx, domain.Host{Label: "web", Hostname: srv.Host(), Port: srv.Port(), IdentityID: saved.Identity.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	failOnPrompt(t, a)
	connectOK(t, a, string(host.Host.ID))

	// Importing with no key file named is an error on that field.
	_, err = a.SaveIdentity(ctx, IdentityInput{Name: "none", ImportKeyFile: true})
	if keyFieldError(err) != MsgKeyFileNone {
		t.Fatalf("no key file: %v", err)
	}
}

// rsa4096 is an RSA-4096 key in OpenSSH form, about 3.4 KB.
func rsa4096(t *testing.T) []byte {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		t.Fatal(err)
	}
	return encodeKey(t, k, "openssh")
}

func TestFR_IDENT_003_Over2800Rejected(t *testing.T) {
	store := secrets.NewMemStore()
	a, _ := storeApp(t, store)
	big := rsa4096(t)
	if len(big) <= secrets.MaxSecretSize {
		t.Fatalf("RSA-4096 key is only %d bytes", len(big))
	}
	_, err := a.SaveIdentity(context.Background(), IdentityInput{Name: "big", Key: NewSecret(bytes.Clone(big))})
	if keyFieldError(err) != MsgKeyTooLarge {
		t.Fatalf("paste: %v", err)
	}
	path := filepath.Join(a.cfg.Home, "id_rsa")
	if err := os.WriteFile(path, big, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = a.SaveIdentity(context.Background(), IdentityInput{Name: "big", KeyPath: path, ImportKeyFile: true})
	if keyFieldError(err) != MsgKeyTooLarge {
		t.Fatalf("import: %v", err)
	}
	if store.Calls(secrets.OpSet) != 0 {
		t.Fatal("an oversized key reached the store")
	}
	// Referencing the same file by path is fine.
	if _, err := a.SaveIdentity(context.Background(), IdentityInput{Name: "big", KeyPath: path}); err != nil {
		t.Fatal(err)
	}
}

func TestSEC_CRED_003_HintMentionsKeyFile(t *testing.T) {
	if !strings.Contains(MsgKeyTooLarge, "reference the key file") || !strings.Contains(MsgKeyTooLarge, "2800 bytes") {
		t.Fatalf("message %q", MsgKeyTooLarge)
	}
	// Any other secret over the limit is refused before the store.
	store := secrets.NewMemStore()
	a, _ := storeApp(t, store)
	_, err := a.SaveIdentity(context.Background(), IdentityInput{Name: "x", Password: NewSecret(bytes.Repeat([]byte("p"), secrets.MaxSecretSize+1))})
	if !errors.Is(err, secrets.ErrTooLarge) || store.Calls(secrets.OpSet) != 0 {
		t.Fatalf("err = %v", err)
	}
	if _, err := a.SaveIdentity(context.Background(), IdentityInput{Name: "x", Password: NewSecret(bytes.Repeat([]byte("p"), secrets.MaxSecretSize))}); err != nil {
		t.Fatal(err)
	}
}
