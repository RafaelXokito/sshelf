// Command sshtestd runs the in-process sshtest server on 127.0.0.1 for demos.
// It prints its address, a known_hosts line and the path of a demo private
// key that every user name can log in with. Stop it with Ctrl+C.
//
//	go run ./internal/testutil/sshtest/cmd/sshtestd [-dir DIR]
//
// With -dir, the host key and demo key are kept in DIR and reused, so the
// known_hosts line stays valid across runs (the port still changes).
package main

import (
	"context"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"golang.org/x/crypto/ssh"

	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

func main() {
	dir := flag.String("dir", "", "directory for the host key and demo key (default: a new temp dir)")
	flag.Parse()
	if err := run(*dir); err != nil {
		fmt.Fprintln(os.Stderr, "sshtestd:", err)
		os.Exit(1)
	}
}

func run(dir string) error {
	if dir == "" {
		d, err := os.MkdirTemp("", "sshtestd-")
		if err != nil {
			return err
		}
		dir = d
	} else if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	hostKey, err := loadOrCreateKey(filepath.Join(dir, "host_ed25519"))
	if err != nil {
		return err
	}
	keyPath := filepath.Join(dir, "demo_ed25519")
	demoKey, err := loadOrCreateKey(keyPath)
	if err != nil {
		return err
	}

	srv, err := sshtest.Start(
		sshtest.WithHostKey(hostKey),
		sshtest.WithAuthorizedKey(sshtest.AnyUser, demoKey.PublicKey()),
	)
	if err != nil {
		return err
	}
	defer func() { _ = srv.Close() }()

	fmt.Printf(`sshtestd listening on %s

known_hosts line (append it to <configDir>/known_hosts):
%s

demo key (any user name is accepted): %s

config.json example:
{
  "version": 1,
  "identities": [{"id": "0192a000-0000-7000-8000-000000000001", "name": "demo", "key_path": %q}],
  "hosts": [{"id": "0192a000-0000-7000-8000-000000000002", "label": "web-prod",
             "hostname": %q, "port": %d, "user": "demo",
             "identity_id": "0192a000-0000-7000-8000-000000000001"}]
}

The shell understands: echo <words>, size, exit [N]. Ctrl+C stops the server.
`, srv.Addr(), srv.KnownHostsLine(), keyPath, keyPath, srv.Host(), srv.Port())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	return nil
}

// loadOrCreateKey reads an OpenSSH private key from path, or writes a new
// Ed25519 key there (mode 0600) if the file does not exist.
func loadOrCreateKey(path string) (ssh.Signer, error) {
	b, err := os.ReadFile(path) //nolint:gosec // path is under the directory the user chose
	if errors.Is(err, fs.ErrNotExist) {
		if _, err := sshtest.WriteKeyFile(path, sshtest.Ed25519, 0o600); err != nil {
			return nil, err
		}
		b, err = os.ReadFile(path) //nolint:gosec // just written
	}
	if err != nil {
		return nil, err
	}
	if blk, _ := pem.Decode(b); blk == nil {
		return nil, fmt.Errorf("%s: not a PEM private key", path)
	}
	return ssh.ParsePrivateKey(b)
}
