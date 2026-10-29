//go:build !sshelf_faultinject

package main

import "github.com/RafaelXokito/sshelf/internal/secrets"

// newSecretStore returns the OS credential store (D-003, D-008).
func newSecretStore() secrets.Store { return secrets.NewOSStore("sshelf") }
