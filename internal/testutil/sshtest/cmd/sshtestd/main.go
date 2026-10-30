// Command sshtestd runs the in-process sshtest server on 127.0.0.1 for demos.
// It prints its address, a known_hosts line and the path of a demo private
// key that every user name can log in with. Stop it with Ctrl+C.
//
//	go run ./internal/testutil/sshtest/cmd/sshtestd [-dir DIR] [-port N] [-password PW [-otp CODE]] [-sftp DIR]
//
// With -dir, the host key and demo key are kept in DIR and reused, so the
// known_hosts line stays valid across runs. With -port the address stays
// the same too (0, the default, picks a free port).
//
// With -password, password authentication with PW is accepted too. With
// -otp as well, the password is asked through keyboard-interactive
// instead, followed by a second round that asks for CODE, as a server
// with a second factor does. Neither is ever a real secret: this is a
// demo server on 127.0.0.1.
//
// With -sftp, the sftp subsystem serves DIR as "/" with the home
// directory /home/pi inside it (DIR/home/pi, created if missing), for the
// M3 acceptance run (ARCH-TEST-004).
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
	port := flag.Int("port", 0, "port on 127.0.0.1 (default: a free port)")
	password := flag.String("password", "", "also accept password authentication with this password, for any user")
	otp := flag.String("otp", "", "with -password: ask for the password and then this one-time code through keyboard-interactive, instead of password authentication")
	sftpDir := flag.String("sftp", "", "serve the sftp subsystem from this directory (home: <DIR>/home/pi)")
	flag.Parse()
	if *otp != "" && *password == "" {
		fmt.Fprintln(os.Stderr, "sshtestd: -otp needs -password")
		os.Exit(2)
	}
	if err := run(*dir, *port, *password, *otp, *sftpDir); err != nil {
		fmt.Fprintln(os.Stderr, "sshtestd:", err)
		os.Exit(1)
	}
}

func run(dir string, port int, password, otp, sftpDir string) error {
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

	opts := []sshtest.Option{
		sshtest.WithHostKey(hostKey),
		sshtest.WithAuthorizedKey(sshtest.AnyUser, demoKey.PublicKey()),
	}
	auth := "public key (the demo key)"
	switch {
	case otp != "":
		opts = append(opts, sshtest.WithKeyboardInteractive(sshtest.AnyUser,
			sshtest.Challenge{Questions: []sshtest.Question{{Prompt: "Password: ", Answer: password}}},
			sshtest.Challenge{Instruction: "Second factor", Questions: []sshtest.Question{{Prompt: "Verification code: ", Echo: true, Answer: otp}}}))
		auth += "; keyboard-interactive: the -password, then the -otp code"
	case password != "":
		opts = append(opts, sshtest.WithPassword(sshtest.AnyUser, password))
		auth += "; password: the -password value"
	}
	if port != 0 {
		opts = append(opts, sshtest.WithPort(port))
	}
	if sftpDir != "" {
		opts = append(opts, sshtest.WithSFTP(sftpDir))
		auth += "; sftp subsystem serving " + sftpDir
	}
	srv, err := sshtest.Start(opts...)
	if err != nil {
		return err
	}
	defer func() { _ = srv.Close() }()

	fmt.Printf(`sshtestd listening on %s
accepted authentication: %s

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
`, srv.Addr(), auth, srv.KnownHostsLine(), keyPath, keyPath, srv.Host(), srv.Port())

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
