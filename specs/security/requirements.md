# sshelf: security requirements and threat model

Status: **Draft** · Owner: security · Last updated: 2026-10-29
Companion: [`credential-storage.md`](credential-storage.md), which covers the config format, keychain keying, the
`secrets` API, the flows and the tests.
Binding inputs: [`../decisions.md`](../decisions.md). The most relevant are D-002 (no master password), D-003
(secrets in the OS credential store), D-006 (sessions are never closed for security reasons), D-007 (keep it
simple), D-008 (`zalando/go-keyring`, no cgo), D-101 (no store means no saved secrets), D-102 (sshelf's own
`known_hosts`) and D-106 (`config.json`).

Format follows [`../README.md`](../README.md): one EARS sentence per requirement, a priority (`MVP` / `v1` /
`Later`) and testable acceptance criteria (`TestSEC_CRED_004_...`). Gaps in the numbering are IDs retired with the
encrypted-vault design. They are not reused.

---

## 1. Security goals

1. **No secret in sshelf's own files.** Passwords, private keys and key passphrases live only in the OS credential
   store: macOS Keychain, or the Linux Secret Service (GNOME Keyring, or KWallet through its Secret Service
   interface). The config file holds metadata only.
2. **Authentic servers.** A credential is never sent before the server's host key is verified.
3. **No incidental leaks.** Secrets never reach logs, error text, the screen, the clipboard, argv, the environment
   or core dumps. Secrets are fetched only when a connection needs them.
4. **Fail safe without a store.** If the credential store is missing or access is denied, sshelf saves no secret
   and never falls back to plaintext (D-101).
5. **No data loss.** Writes to the config file are atomic and survive crashes.

Non-goals: protection from code running as the same OS user, protection from root, and a "Touch ID per
connection" prompt (not achievable from a plain Go binary, see credential-storage §2.3; not planned per D-008).

---

## 2. Assets, actors, trust boundaries

| ID | Asset | Needs | Where it lives |
|----|-------|-------|----------------|
| A-1 | Credentials: passwords, private keys, key passphrases | C | OS credential store, and sshelf memory during a connection attempt |
| A-2 | Host metadata: labels, hostnames, ports, users, groups, tags, identity references, key-file paths | I (C desirable) | `config.json`, **plaintext**, mode 0600 |
| A-3 | Trust store: accepted host keys | **I** | sshelf `known_hosts` (0600), plus `~/.ssh/known_hosts` (read-only) |
| A-4 | Referenced key files (`~/.ssh/id_*`) | C | User's disk. sshelf only reads them. |
| A-5 | Live sessions and forwarded agents | C, I | Process memory and the network |

| ID | Actor | Capability |
|----|-------|-----------|
| AC-1 | User | Logged-in OS user who runs sshelf |
| AC-2 | Thief | Has the disk while the machine is off, or a powered, unlocked machine for a short time |
| AC-3 | File leaker | Obtains `config.json` or `known_hosts` (dotfiles repo, backups, sync) |
| AC-4 | Same-user process | Malware or any tool running as the user. It can call `security` or D-Bus, read files and read the clipboard. |
| AC-5 | Other local OS users | Can read files with permissive modes |
| AC-6 | Network attacker | MITM, including on the first connection |
| AC-7 | Malicious server | Controls banners, prompts and output. It can use a forwarded agent. |
| AC-8 | Onlooker | Sees the screen, or a screen share |

| ID | Boundary | What crosses it |
|----|----------|-----------------|
| TB-1 | sshelf ↔ OS credential store | secret get, set and delete (through `/usr/bin/security` on macOS, D-Bus on Linux) |
| TB-2 | sshelf ↔ local disk | `config.json`, `known_hosts`, log, referenced key files |
| TB-3 | sshelf ↔ network and SSH server | handshake, host key, authentication, session |
| TB-4 | sshelf ↔ terminal, clipboard and OS | rendered text, copied text, memory paging, core dumps |
| TB-5 | sshelf ↔ ssh-agent (v1) | signing requests, and forwarding to remote hosts |

---

## 3. Threat model (STRIDE)

| ID | STRIDE | Threat | Mitigation | Residual / decision |
|----|--------|--------|------------|---------------------|
| T-1 | I | **Laptop stolen while off.** | Keychain and Secret Service items are encrypted under the user's login password. Docs recommend FileVault or LUKS. | Without disk encryption, `config.json` (hosts, users) is readable. Accepted. Secrets stay protected by the login password. |
| T-2 | I | **`config.json` leaked** (public dotfiles repo, backup, sync). | No secret fields exist in the schema (SEC-CRED-001). Mode 0600 (SEC-FILE-001). | **Accepted:** the host inventory is reconnaissance value, and the user is warned not to publish the config dir. |
| T-3 | I | **Same-user process reads credentials from the store without a prompt.** On Linux, any process can read an unlocked Secret Service collection over D-Bus. On macOS, with the recommended backend, items trust `/usr/bin/security`, so any process can run `security find-generic-password -w -s sshelf …` silently. | Only what we can do: secrets are never in files (SEC-CRED-001) or env (SEC-MEM-008). | **Accepted (D-003, D-008).** Equivalent to the exposure of `~/.ssh` key files. A strict, signed native (cgo) macOS backend is `Later` (D-008). |
| T-4 | I | **Same-user malware** keylogs or reads process memory. | Secrets are fetched just in time and zeroed after authentication (SEC-MEM-009). No core dumps (SEC-MEM-003). | Accepted. No user-space mitigation exists. |
| T-5 | I | **Other OS users** read files. | 0600/0700 regardless of umask, plus a check on startup (SEC-FILE-001/002). The credential store is per user. | None known. |
| T-6 | D, I | **Credential store unavailable, locked, or access denied** (headless Linux, no D-Bus, user cancels the unlock prompt). | Clear message. Key-file and agent hosts keep working. No plaintext fallback (SEC-CRED-005/006). | The user cannot save passwords on such systems until a store exists. Accepted (D-101). |
| T-7 | I | **Secrets in swap or core dumps.** | `RLIMIT_CORE=0` and `PR_SET_DUMPABLE=0` (SEC-MEM-003). Short secret lifetime (SEC-MEM-009). | Linux swap without encryption is accepted. macOS encrypts swap by default. |
| T-8 | T, S | **Tampering with `config.json` or `known_hosts`**, for example pointing a host at an attacker's server or adding an attacker's key. | Hostname changes cause a TOFU prompt for the new host (SEC-HOSTKEY-004). | Requires same-user write access, which already implies T-3. Accepted. |
| T-9 | I | **Orphaned credentials** left in the store after an identity is deleted, a delete fails, or the config is lost. | Delete on identity delete, with a warning naming any item that could not be deleted (SEC-CRED-004). Cleanup action (SEC-CRED-008, Later). | Items are still protected by the store, so there is no exposure beyond T-3. |
| T-10 | S | **MITM on first connect.** | TOFU prompt with fingerprint and default reject. OpenSSH known_hosts honoured (SEC-HOSTKEY-003/004); host CAs are `Later` (SEC-HOSTKEY-011). | First-contact trust. Accepted, as in OpenSSH. |
| T-11 | S | **Host key changed.** | Hard failure before auth, with no one-key override (SEC-HOSTKEY-005). | None known. |
| T-12 | S | **Host-key algorithm downgrade** that forces a TOFU prompt. | Pin to the algorithms of the stored keys (SEC-HOSTKEY-007). | None known. |
| T-13 | E | **Forwarded agent abused** by a compromised server. | Off by default, per-host opt-in with a warning (SEC-AGENT-002). | Accepted while the user enables it. |
| T-14 | S | **Server phishing** through keyboard-interactive prompts (asking for the macOS login password, say). | Prompts labelled as coming from the server, auto-answer rules (SEC-SESSION-002). | None known. |
| T-15 | T | **Escape-sequence injection** through banners, errors or imported labels. | Sanitize before rendering (SEC-SESSION-003). | Session output goes to the user's terminal, as in `ssh`. Accepted. |
| T-16 | I | **Credential sent to the wrong host**, or before verification. | SEC-SESSION-001. | None known. |
| T-17 | I | **Secrets in logs, screen, clipboard, argv or env.** | SEC-LOG-*, SEC-CLIP-005, SEC-MEM-001/008. | None known. |
| T-18 | D | **Config corruption** from a crash, two instances or an external edit. | Atomic write (SEC-FILE-003), change detection (SEC-FILE-005, v1). | None known. |
| T-19 | E | **Malicious `~/.ssh/config` import** (`ProxyCommand`, `LocalCommand`, `Match exec`). | Import flags these as unsupported and never executes them (ARCH-DATA-003). There is no system ssh fallback (D-104). | None known. |
| T-20 | E | **Vulnerable dependencies** (`x/crypto/ssh`, the keyring library, `godbus`). | Minimal dependencies, `govulncheck` in CI, Terrapin-fixed x/crypto (SEC-SESSION-004). | None known. |
| T-21 | E | **Prompt fatigue.** Repeated macOS "allow access" dialogs after each rebuild or update train users to click "Always Allow". | The recommended backend does not trigger per-binary ACL prompts (SEC-CRED-009). | Trade-off against T-3, accepted by D-008. |

---

## 4. Requirements

### 4.1 Credential store (SEC-CRED)

**SEC-CRED-001: Secrets only in the OS credential store.**
The system shall store passwords, private keys and key passphrases only in the OS credential store through the
`secrets` package, and shall never write them to `config.json`, `known_hosts`, backups, logs or any other file.
Priority: MVP · Status: Implemented · Threats: T-2, T-17
- AC1: Save an identity with canary password, key and passphrase values. Byte-search every file under the config,
  state and temp dirs: zero hits. The fake store holds all three.
- AC2: A reflection test asserts that the config schema structs contain no `[]byte`, `secrets.Secret`, or string
  field named `password`, `passphrase` or `private_key`.

**SEC-CRED-002: Item naming.**
The system shall store each secret as one generic-password item with service `sshelf` and account
`<identity-uuid>/<field>`, where `<field>` is `password`, `private-key` or `key-passphrase`.
Priority: MVP · Status: Implemented · Threats: T-9
- AC1: A fake-store test asserts the exact keys after creating an identity with all three fields.
- AC2: Renaming an identity changes no keys. The UUID is stable.

**SEC-CRED-003: Size limit.**
If a secret exceeds 2800 bytes, then the system shall refuse to store it and suggest referencing the key file
instead.
Priority: MVP · Status: Implemented · Threats: T-6
- AC1: A 2800-byte value is stored and a 2801-byte value returns `secrets.ErrTooLarge` (an RSA-4096 OpenSSH key is
  about 3.4 KB). The message mentions "reference the key file".

**SEC-CRED-004: Delete with the identity.**
When an identity is deleted, or one of its secret fields is cleared, the system shall delete the matching store
items and, for any delete that fails, show a warning naming the orphaned item (service and account) without
tracking it.
Priority: MVP · Status: Implemented · Threats: T-9
- AC1: Deleting an identity leaves the fake store empty for that UUID.
- AC2: With a fake that fails `Delete`, the identity is still removed from the config, a warning names
  `sshelf` / `<uuid>/password`, and nothing about the item is written to `config.json`.

**SEC-CRED-005: Store unavailable.**
If the credential store is unavailable at startup or on use, then the system shall keep hosts that use key files
or the agent working, disable the secret fields in forms with an explanation, and never store a secret elsewhere.
Priority: MVP · Status: Implemented · Threats: T-6
- AC1: With the fake returning `secrets.ErrUnavailable`, the host list loads, a key-file host connects, the
  password field is disabled with the D-101 message, and no file contains the canary.

**SEC-CRED-006: Access denied or cancelled.**
If the store denies access or the user cancels an OS unlock prompt, then the system shall abort only the current
connect or save with a message that names the store, without retrying automatically and without caching the
failure. A store that is not available at all (`ErrUnavailable`) is not a denial: per D-101 a connect does not abort
but uses the key file and asks for a needed password or passphrase for that connection only, without saving it, and
a save is refused with the D-101 explanation (SEC-CRED-005).
Priority: MVP · Status: Implemented · Threats: T-6
- AC1: Fake returns `ErrDenied` on `Get`: connect fails with the "Access to Keychain was denied" message, makes no
  second `Get` call, and the next connect calls `Get` again.
- AC2: Fake returns `ErrUnavailable` on every call: a host with a stored password opens the password prompt (noting
  that the store is not available), connects with the typed value, and nothing is saved.

**SEC-CRED-007: Missing item.**
If an identity references a secret that the store does not contain (new machine, synced config, manual
deletion), then the system shall show the field as "not set on this device" and ask for the value at connect time.
Priority: MVP · Status: Implemented · Threats: T-6, T-9
- AC1: Config with a password reference and an empty fake: connect opens the password prompt, and the typed value
  is used for that connection only. Offering to save it is v1 (FR-IDENT-005).

**SEC-CRED-008: Orphan cleanup.**
When the user runs "Clean up stored credentials", the system shall list the `sshelf` store items whose identity
UUID is not in the config and delete the ones the user confirms.
Priority: Later · Status: Draft · Threats: T-9
- AC1: A fake with 3 items, 1 referenced: the dialog lists 2, and confirming deletes 2.
- Note: this needs backend enumeration (Secret Service `SearchItems`, `security dump-keychain`). Out of MVP scope
  per D-007.

**SEC-CRED-009: No per-binary ACL prompts.**
The system shall use a credential-store backend whose access does not depend on the sshelf binary's code
signature, so that rebuilding or updating sshelf does not trigger new OS access prompts.
Priority: MVP · Status: Draft · Threats: T-21
- AC1: Release checklist (manual, macOS): store a password with build N, install build N+1 (unsigned), connect.
  No Keychain dialog appears.
- Note: a future native (cgo) backend binds the item ACL to the binary. That needs signed releases with a stable
  designated requirement before it can satisfy this requirement. That backend is `Later` (D-008).

### 4.2 Host-key verification (SEC-HOSTKEY)

**SEC-HOSTKEY-001: Always verify.**
The system shall verify every server host key through the sshelf `HostKeyCallback` and shall never use
`ssh.InsecureIgnoreHostKey`.
Priority: MVP · Status: Implemented · Threats: T-10, T-11
- AC1: CI fails if `InsecureIgnoreHostKey` appears outside `_test.go`.
- AC2: Integration test (in-process `x/crypto/ssh` server): an unknown key with "Cancel" fails before any auth
  callback runs.

**SEC-HOSTKEY-002: sshelf known_hosts file.**
The system shall record accepted host keys in `known_hosts` in the sshelf config directory, in OpenSSH format,
written atomically with mode 0600 (D-102).
Priority: MVP · Status: Implemented · Threats: T-10
- AC1: After "Trust and save", the file contains one parseable line (`knownhosts.New` accepts it), and
  `~/.ssh/known_hosts` is byte-identical to before.

**SEC-HOSTKEY-003: Honour OpenSSH trust, read-only.**
The system shall also consult `~/.ssh/known_hosts` and `/etc/ssh/ssh_known_hosts` through
`golang.org/x/crypto/ssh/knownhosts`, including plain and hashed host entries, `[host]:port` entries and `@revoked`
markers, and shall never modify them.
Note: `@cert-authority` support is SEC-HOSTKEY-011 (`Later`).
Priority: MVP · Status: Implemented · Threats: T-10, T-11
- AC1: A hashed fixture entry connects without a prompt.
- AC2: A key marked `@revoked` fails even when sshelf's file trusts it, and the UI offers no trust action.

**SEC-HOSTKEY-004: TOFU prompt.**
When a server presents a host key that no source knows, the system shall show host, port, key type and SHA-256
fingerprint and offer exactly two choices, "Trust and save" and "Cancel", where Enter and Esc both mean "Cancel".
Priority: MVP · Status: Implemented · Threats: T-10, T-8
- AC1: Model test: Enter and Esc on the initial prompt both cancel. The fingerprint equals
  `ssh.FingerprintSHA256(key)`. "Trust once" is v1 (FR-KH-007) and writes nothing.

**SEC-HOSTKEY-005: Changed key is a hard failure.**
If a server presents a key that differs from a stored key of the same type for that host, then the system shall
abort before authentication, show both fingerprints, and offer no single-keystroke override.
Priority: MVP · Status: Implemented · Threats: T-11
- AC1: Connect returns `ErrHostKeyMismatch`, no auth callback fires, and the test server records zero auth
  attempts.
- AC2: The block screen shows the path and line number of the stale entry and how to remove it. After the user
  deletes that line, the next connect shows a fresh TOFU prompt. An in-app remove action is v1 (FR-KH-005).

**SEC-HOSTKEY-007: No algorithm downgrade.**
When keys are known for a host, the system shall restrict `ssh.ClientConfig.HostKeyAlgorithms` to the algorithms
of those keys.
Priority: MVP · Status: Implemented · Threats: T-12
- AC1: With a stored ed25519 key, `HostKeyAlgorithms` contains only ed25519 algorithms. An RSA-only server fails
  without a TOFU prompt.

**SEC-HOSTKEY-011: Host certificate authorities.**
Where a trusted known-hosts file lists a CA with `@cert-authority`, the system shall accept a host certificate
signed by that CA only when the certificate is valid for the hostname and not expired.
Priority: Later · Status: Draft · Threats: T-10 · Source: FR-KH-006
- AC1: A certificate signed by a listed CA connects with no prompt. An expired certificate, or one for another
  hostname, is rejected.

**SEC-HOSTKEY-010: Per-hop verification.**
When connecting through jump hosts, the system shall verify each hop's host key independently under SEC-HOSTKEY-001 to SEC-HOSTKEY-007.
Priority: v1 · Status: Draft · Threats: T-10, T-11
- AC1: Two-hop test with a mismatched inner key returns `ErrHostKeyMismatch` for hop 2.

### 4.3 SSH agent (SEC-AGENT), v1 per D-005

**SEC-AGENT-001: Agent use is bounded.**
Where agent use is allowed for a host, the system shall offer the agent's keys only after the identity's own key
(stored or file) and before password or keyboard-interactive authentication, and shall not contact the agent when
agent use is disabled for the host.
Priority: v1 · Status: Draft · Threats: T-13
- AC1: A logging test agent receives zero `List` calls when the identity's key-file key is accepted, and when the
  host has "Don't use agent" set.

**SEC-AGENT-002: Forwarding off by default.**
The system shall disable agent forwarding by default, and shall enable it per host only after the user
acknowledges that root on the remote host can use the forwarded keys during the session.
Priority: v1 · Status: Draft · Threats: T-13
- AC1: With default settings, the test server receives no `auth-agent-req@openssh.com` request.
- AC2: Enabling forwarding without the acknowledgement is impossible (model test).

### 4.4 Memory (SEC-MEM)

**SEC-MEM-001: Secret type.**
The system shall carry secrets inside sshelf as `secrets.Secret` (a `[]byte` wrapper with `Destroy()`), never as a
Go `string` beyond the boundary of the keyring library.
Priority: MVP · Status: Implemented · Threats: T-4, T-17
- AC1: `Destroy()` zeroes the backing array (an alias-slice assertion).
- AC2: `go vet`-style check: no exported `secrets` function returns `string` secrets.
- Note: the recommended keyring library's API is `string`-based, so one immutable copy per get or set is
  unavoidable and accepted (credential-storage §6).

**SEC-MEM-003: No core dumps.**
When the process starts, the system shall set `RLIMIT_CORE` to 0 and, on Linux, call `prctl(PR_SET_DUMPABLE, 0)`.
Priority: MVP · Status: Implemented · Threats: T-7, T-4
- AC1: The hardening is an unexported function in `cmd/sshelf` (main), called first thing at startup. After it
  runs, `Getrlimit(RLIMIT_CORE)` returns `{0,0}`. On Linux, `PR_GET_DUMPABLE` returns 0.

**SEC-MEM-008: No secrets to child processes.**
The system shall never pass a secret through argv or environment variables, and the only child process that may
receive secrets is `/usr/bin/security`, invoked by `zalando/go-keyring` on macOS through stdin and stdout (D-008).
Priority: MVP · Status: Draft · Threats: T-17
- AC1: The import-rule test (`internal/archtest`, ADR-009) finds no import of `os/exec` in any sshelf package, so
  sshelf itself starts no child process that could receive a secret.
- AC2: A dependency review confirms the keyring library uses `security -i` (stdin) for set operations.

**SEC-MEM-009: Just-in-time secrets.**
When connecting, the system shall read the identity's secrets from the store only for that connection attempt,
and shall destroy them (and drop any parsed `ssh.Signer`) once authentication completes or fails.
Priority: MVP · Status: Implemented · Threats: T-4, T-7
- AC1: The fake store counts `Get` calls: zero at startup, one per needed field per connect.
- AC2: After auth, the `Secret` buffers are zero (test hook).

### 4.5 Logging and display (SEC-LOG)

**SEC-LOG-001: Never log secrets.**
The system shall never write a secret to a log, error message, panic report, stdout or stderr, and shall send no
telemetry.
Priority: MVP · Status: Implemented · Threats: T-17
- AC1: A canary harness runs these flows with debug logging on: create an identity, connect (success, wrong
  password, host-key mismatch), store denied, store unavailable. It captures stdout, stderr and the log, and finds
  zero canary hits.

**SEC-LOG-002: Redaction.**
The system shall render `secrets.Secret` as `[REDACTED]` in `fmt` (`%s %v %+v %#v %q`), `encoding/json` and
`log/slog`.
Priority: MVP · Status: Implemented · Threats: T-17
- AC1: A table test over every formatter.

**SEC-LOG-003: Logging off by default.**
Where debug logging is enabled by setting the environment variable `SSHELF_DEBUG=1`, the system shall write the log with mode 0600 in the state directory and rotate it
at 5 MiB.
Priority: MVP · Status: Implemented · Threats: T-17, T-5
- AC1: The default run creates no log file. With logging on, the file mode is 0600.

**SEC-LOG-005: Secrets never displayed.**
The system shall show each secret field only as "set", "not set" or "not set on this device", never as its value
or length.
Priority: MVP · Status: Implemented · Threats: T-17, AC-8
- AC1: Golden test of the identity form, with a canary password, contains no canary and no run of bullets of the
  same length.

### 4.6 Clipboard (SEC-CLIP)

**SEC-CLIP-005: Never copy secrets.**
The system shall never place a secret on the clipboard, and copied command lines shall contain no secret.
Priority: MVP · Status: Implemented · Threats: T-17
- AC1: The import-rule test (`internal/archtest`) finds no clipboard package imported by any sshelf package, so
  the MVP has no path that can copy a secret.
- Note: when a copy feature arrives (FR-HOST-008, v1), its clipboard writer takes `string` only and gets a canary
  test of "copy ssh command".

### 4.7 Files (SEC-FILE)

**SEC-FILE-001: Modes.**
The system shall create the config directory with mode 0700 and `config.json`, `known_hosts` and the log with
mode 0600, independent of the umask.
Priority: MVP · Status: Implemented · Threats: T-5, T-2
- AC1: Under `umask 000`, `os.Stat` modes match.

**SEC-FILE-002: Permission check.**
If `config.json`, `known_hosts` or the config directory is accessible by group or others at startup, then the
system shall restore modes 0600/0700 and show a one-line notice.
Priority: MVP · Status: Implemented · Threats: T-5
- AC1: A 0644 config ends up as 0600 after start, and the notice is shown once.

**SEC-FILE-003: Atomic write.**
When saving `config.json` or `known_hosts`, the system shall write a temp file in the same directory
(`O_CREATE|O_EXCL`, 0600), `Sync` it, `rename` it over the target (resolving symlinks first), and `fsync` the
directory.
Priority: MVP · Status: Implemented · Threats: T-18
- AC1: Fault injection at each step (through an `fs` fake) leaves the old or the new file, never a partial file.
- AC2: A symlinked config stays a symlink, and the target is updated.

**SEC-FILE-005: External changes.**
If `config.json` changed on disk since it was loaded, then the system shall refuse to overwrite it and offer to
reload it.
Priority: v1 · Status: Draft · Threats: T-18 · Source: FR-CFG-004
- AC1: An external edit followed by a save in the UI shows the reload offer, and the file is byte-for-byte
  unchanged.

**SEC-FILE-009: Unprotected key files.**
If an identity references a private key file readable by group or others, then the system shall refuse to use it
and show OpenSSH's "unprotected private key file" error.
Priority: MVP · Status: Implemented · Threats: T-5
- AC1: A 0644 key is refused and a 0600 key is used.

### 4.8 SSH session safety (SEC-SESSION)

**SEC-SESSION-001: Credentials only to verified owners.**
The system shall offer a credential only to the hosts linked to its identity, and only after that connection's
host key has been verified.
Priority: MVP · Status: Implemented · Threats: T-16
- AC1: With a rejected host key, no store `Get` and no auth callback happens. Host A's password is never sent to
  host B.

**SEC-SESSION-002: Keyboard-interactive prompts.**
When a server sends keyboard-interactive prompts, the system shall auto-answer only a single echo-off prompt
matching `/password/i` with the identity's stored password, and shall show every other prompt (such as an OTP) to
the user labelled "Prompt from <host>" with control characters stripped.
Priority: MVP · Status: Implemented · Threats: T-14
- AC1: A password-only keyboard-interactive server gets the stored password with no user prompt. Two prompts get
  no auto-answer. An instruction containing `\x1b]52;` renders escaped.

**SEC-SESSION-003: Sanitize untrusted strings.**
The system shall strip C0 controls (except `\t` and `\n`), C1 controls and ANSI escape sequences from
server-supplied and imported strings before rendering them in sshelf's UI.
Priority: MVP · Status: Implemented · Threats: T-15
- AC1: Fuzz `Sanitize`: the output never contains runes U+0000–U+0008, U+000B–U+001F, U+007F or U+0080–U+009F.

**SEC-SESSION-004: Algorithms and dependencies.**
The system shall use `golang.org/x/crypto/ssh` default algorithms (≥ v0.17.0, Terrapin strict-KEX,
[terrapin-attack.com](https://terrapin-attack.com)), shall not enable legacy algorithms, and shall run
`govulncheck` in CI.
Priority: MVP · Status: Implemented · Threats: T-20
- AC1: CI `govulncheck ./...` passes. A unit test asserts that the client config contains no `ssh-dss`, `3des-cbc`,
  `arcfour*` or `diffie-hellman-group1-sha1`.

---

## 5. Requirement count

| Priority | Count |
|----------|-------|
| MVP | 31 |
| v1 | 4 |
| Later | 2 |
| **Total** | **37** |

## 6. Resolved decisions

The open decisions from earlier drafts are settled in `../decisions.md`:

| Topic | Resolution |
|-------|------------|
| "Only sshelf can read the credentials" (T-3) | Not a goal. `zalando/go-keyring` is used and same-user access is accepted. A signed native macOS backend is `Later` (D-008). |
| Touch ID or user presence per connection | Not planned (D-008). |
| Plaintext fallback when no store exists | `Later` and opt-in (D-101). |
| Secrets per device, not synced | Accepted. A synced `config.json` shows "not set on this device" (D-108). |
