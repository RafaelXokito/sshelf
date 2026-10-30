# Release checklist

Manual checks run before each release. Record the result (pass/fail, OS
version, build) in the release PR. Started by T-010; T-030 completes it.
The step-by-step commands are in [`specs/acceptance.md`](../specs/acceptance.md);
run that whole checklist on macOS and Linux before a release.

## Automated gates (must be green on the release commit)

- [ ] CI: lint, `go test -race` on Linux and macOS, cross-compile, keyring jobs.
- [ ] `go run ./scripts/tracecheck` reports 0 problems (NFR-QUAL-001).
- [ ] `go run ./scripts/covercheck coverage.out` passes (NFR-QUAL-003).

## Sessions and terminals

- [ ] **FR-CONN-001: `vim`, `htop` and `tmux` against a real `sshd`**
  render and accept input, including after a window resize
  (acceptance §6).
- [ ] **NFR-PORT-002: named terminals.** Start, list, connect, exit and
  `stty -a` round trip in iTerm2, Terminal.app, kitty, WezTerm, Alacritty,
  Ghostty, GNOME Terminal, Konsole, xterm, tmux and screen, as available
  (acceptance §12.2).
- [ ] **ADR-007:** keys on the wire, first keystroke after a session and
  `stty -a` round trip (acceptance §10).

## Credential store

- [ ] **FR-CRED-001: OS unlock prompt.** With the Linux login keyring
  locked, connecting a host with a stored password shows the desktop's
  unlock dialog and then authenticates (acceptance §11.1).
- [ ] **SEC-CRED-009: no Keychain dialog after an upgrade (macOS).**
  1. Install build N (unsigned). Save an identity with a password and link a
     host to it.
  2. Install build N+1 (unsigned) over it.
  3. Connect to the host. No Keychain "allow access" dialog appears and the
     connection authenticates with the stored password.
- [ ] **go-keyring version bump review (D-008, SEC-MEM-008 AC2).** If
  `github.com/zalando/go-keyring` changed since the last release, read the
  diff of `keyring_darwin.go` and `keyring_unix.go` and confirm:
  - macOS `Set` still writes `add-generic-password` to `/usr/bin/security -i`
    over **stdin**, so the secret is never in argv;
  - macOS `Get` still reads the value from `find-generic-password -w` stdout;
  - the `ErrNotFound` and `ErrSetDataTooBig` sentinels and the error shapes
    that `internal/secrets` maps (`security` exit statuses, the Secret
    Service "failed to unlock correct collection" error) are unchanged;
  - the `security -i` command limit is still 4096 bytes, so
    `MaxSecretSize` (2800) still fits;
  - no cgo backend was added for darwin or linux.
