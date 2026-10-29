# Release checklist

Manual checks run before each release. Record the result (pass/fail, OS
version, build) in the release PR. Started by T-010; T-030 completes it.

## Credential store

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
