package sshtest

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"
)

// Key types accepted by GenerateKey.
const (
	Ed25519 = "ed25519"
	ECDSA   = "ecdsa"
	RSA     = "rsa"
)

// GenerateKey returns a new private key of the given type (Ed25519, ECDSA
// P-256 or RSA 2048).
func GenerateKey(kind string) (crypto.Signer, error) {
	switch kind {
	case Ed25519:
		_, k, err := ed25519.GenerateKey(rand.Reader)
		return k, err
	case ECDSA:
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case RSA:
		return rsa.GenerateKey(rand.Reader, 2048)
	default:
		return nil, fmt.Errorf("sshtest: unknown key type %q", kind)
	}
}

// GenerateSigner returns an ssh.Signer for a new key of the given type.
func GenerateSigner(kind string) (ssh.Signer, error) {
	k, err := GenerateKey(kind)
	if err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(k)
}

// WriteKeyFile writes a new private key of the given type to path in OpenSSH
// format with the given mode, and returns its public key.
func WriteKeyFile(path, kind string, mode os.FileMode) (ssh.PublicKey, error) {
	k, err := GenerateKey(kind)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(k, "sshtest")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), mode); err != nil {
		return nil, err
	}
	// WriteFile applies the umask; set the exact mode the caller asked for.
	if err := os.Chmod(path, mode); err != nil {
		return nil, err
	}
	signer, err := ssh.NewSignerFromKey(k)
	if err != nil {
		return nil, err
	}
	return signer.PublicKey(), nil
}
