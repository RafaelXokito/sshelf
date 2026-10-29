package app

import (
	"bytes"
	"errors"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/secrets"
)

// The messages shown next to the key fields of the identity form
// (FR-IDENT-003, SEC-CRED-003).
const (
	MsgKeyTooLarge = "Key is larger than 2800 bytes: reference the key file instead"
	MsgKeyInvalid  = "Not a private key: paste an Ed25519, ECDSA or RSA key (OpenSSH, PEM or PKCS#8)"
	MsgKeyFileNone = "Enter the key file to store"
	// MsgKeyPassphrase: an encrypted PEM key, which has no public part in
	// the clear, did not open with the passphrase typed.
	MsgKeyPassphrase = "Wrong passphrase for this encrypted key"
)

// checkKey validates a pasted or imported private key (FR-IDENT-003): at
// most 2800 bytes (SEC-CRED-003, D-008), and an Ed25519, ECDSA or RSA key
// in OpenSSH, PEM or PKCS#8 form. An encrypted key is accepted; its public
// part, which the OpenSSH format keeps in the clear, or else the key
// decrypted with passphrase when one was typed, gives the authorized_keys
// line it returns for display. It returns "" when an encrypted key has no
// readable public part. Problems are *domain.ValidationError on
// domain.FieldKey.
func checkKey(key, passphrase []byte) (string, error) {
	invalid := func(msg string) error {
		return &domain.ValidationError{Fields: map[domain.Field]string{domain.FieldKey: msg}}
	}
	if len(key) > secrets.MaxSecretSize {
		return "", invalid(MsgKeyTooLarge)
	}
	raw, err := ssh.ParseRawPrivateKey(key)
	var pub ssh.PublicKey
	if pm, ok := errors.AsType[*ssh.PassphraseMissingError](err); ok {
		pub = pm.PublicKey
		if pub == nil && len(passphrase) > 0 {
			raw, err = ssh.ParseRawPrivateKeyWithPassphrase(key, passphrase)
			if err != nil {
				return "", invalid(MsgKeyPassphrase)
			}
		} else if pub == nil {
			return "", nil
		}
	} else if err != nil {
		return "", invalid(MsgKeyInvalid)
	}
	if pub == nil {
		signer, err := ssh.NewSignerFromKey(raw)
		if err != nil {
			return "", invalid(MsgKeyInvalid)
		}
		pub = signer.PublicKey()
	}
	switch t := pub.Type(); {
	case t == ssh.KeyAlgoED25519, t == ssh.KeyAlgoRSA, strings.HasPrefix(t, "ecdsa-sha2-"):
	default:
		return "", invalid(MsgKeyInvalid)
	}
	return string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(pub))), nil
}

// keyDisplay returns the type and SHA256 fingerprint of an authorized_keys
// line, as ssh-keygen -l shows them ("ED25519", "SHA256:…"), or "" when
// the line does not parse.
func keyDisplay(line string) (typ, fingerprint string) {
	if line == "" {
		return "", ""
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return "", ""
	}
	typ = strings.ToUpper(strings.TrimPrefix(pub.Type(), "ssh-"))
	if strings.HasPrefix(pub.Type(), "ecdsa-") {
		typ = "ECDSA"
	}
	return typ, ssh.FingerprintSHA256(pub)
}

// importKeyFile reads the identity's key file for "Store key in keychain"
// (ADR-010): the file must exist and be private to the user
// (SEC-FILE-009); it is only read, never changed. The caller owns the
// returned bytes.
func (a *App) importKeyFile(keyPath string) ([]byte, error) {
	if strings.TrimSpace(keyPath) == "" {
		return nil, &domain.ValidationError{Fields: map[domain.Field]string{domain.FieldKeyPath: MsgKeyFileNone}}
	}
	c := Credentials{Home: a.cfg.Home}
	_, b, err := c.readKeyFile(strings.TrimSpace(keyPath))
	if err != nil {
		clear(b)
		return nil, &domain.ValidationError{Fields: map[domain.Field]string{domain.FieldKeyPath: capitalise(err.Error())}}
	}
	if len(b) > secrets.MaxSecretSize {
		clear(b)
		return nil, &domain.ValidationError{Fields: map[domain.Field]string{domain.FieldKeyPath: MsgKeyTooLarge}}
	}
	return b, nil
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
