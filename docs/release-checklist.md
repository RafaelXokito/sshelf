# Release checklist

Manual checks run before each release. Record the result (pass/fail, OS
version, build) in the release PR. Started by T-010; T-030 completes it.
The step-by-step commands are in [`specs/acceptance.md`](../specs/acceptance.md);
run that whole checklist on macOS and Linux before a release.

## Automated gates (must be green on the release commit)

- [ ] CI: lint, `go test -race` on Linux and macOS, cross-compile, keyring jobs.
- [ ] `go run ./scripts/tracecheck` reports 0 problems over MVP and M2 (NFR-QUAL-001).
- [ ] `go run ./scripts/covercheck coverage.out` passes (NFR-QUAL-003).

## Sessions and terminals

- [ ] **FR-CONN-001: `vim`, `htop`, `less` and `tmux` inside the frame
  against a real `sshd`** render colours, the alternate screen and the
  cursor correctly and accept input, including after a window resize with
  two tabs open (acceptance M2.4, M2.6).
- [ ] **FR-TAB-011: mouse in `vim`, `htop` and `tmux`** lands on the
  clicked cell; header clicks are ignored while the remote has the mouse
  (acceptance M2.4.3, M2.4.4, M2.4.7).
- [ ] **NFR-UX-001 / D-012: native selection in a plain shell.** Select
  and copy with the terminal's own selection in a session tab and in
  scrollback mode (acceptance M2.5, M2.7.3).
- [ ] **SEC-SESSION-005: hostile output.** A remote OSC 52 write and OSC 0
  title do not change the local clipboard or window title
  (acceptance M2.8).
- [ ] **NFR-PORT-002 with tabs:** two tabs, prefix switching, a
  double-click connect and mouse in `vim` in each named terminal
  (acceptance M2.12).
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
