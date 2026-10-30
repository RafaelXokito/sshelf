# sshelf: security requirements and threat model

Status: **Draft** · Owner: security · Last updated: 2026-10-30 (M3: D-017 to D-020)
Companion: [`credential-storage.md`](credential-storage.md), which covers the config format, keychain keying, the
`secrets` API, the flows and the tests.
Binding inputs: [`../decisions.md`](../decisions.md). The most relevant are D-002 (no master password), D-003
(secrets in the OS credential store), D-006 (sessions are never closed for security reasons), D-007 (keep it
simple), D-008 (`zalando/go-keyring`, no cgo), D-101 (no store means no saved secrets), D-102 (sshelf's own
`known_hosts`), D-106 (`config.json`) and, for M3, D-019 (the editor exception) and D-020 (path safety).

Format follows [`../README.md`](../README.md): one EARS sentence per requirement, a priority (`MVP` / `M2` /
`M3` / `v1` / `Later`) and testable acceptance criteria (`TestSEC_CRED_004_...`). Gaps in the numbering are IDs retired with the
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
| A-6 | The user's local files, above all those outside the folder a download targets | I, C | User's disk |
| A-7 | File contents in transit and the edit-locally copy (M3) | C, I | Network, process memory, a private temp dir |

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
| TB-6 | sshelf ↔ SFTP server (M3) | directory listings (names, sizes, times, types), file contents, status messages |
| TB-7 | sshelf ↔ the user's editor (M3) | one temp file path; the terminal while the editor runs |

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
| T-15 | T | **Escape-sequence injection** through banners, errors or imported labels, and (until M2) through session output. | Sanitize before rendering (SEC-SESSION-003). From M2, session output reaches only sshelf's emulator (SEC-SESSION-005). | MVP: session output goes to the user's terminal, as in `ssh`; accepted. M2: closed, except for bugs in sshelf's emulator, which runs in sshelf's process and has no access to the user's terminal. |
| T-16 | I | **Credential sent to the wrong host**, or before verification. | SEC-SESSION-001. | None known. |
| T-17 | I | **Secrets in logs, screen, clipboard, argv or env.** | SEC-LOG-*, SEC-CLIP-005, SEC-MEM-001/008. | None known. |
| T-18 | D | **Config corruption** from a crash, two instances or an external edit. | Atomic write (SEC-FILE-003), change detection (SEC-FILE-005, v1). | None known. |
| T-19 | E | **Malicious `~/.ssh/config` import** (`ProxyCommand`, `LocalCommand`, `Match exec`). | Import flags these as unsupported and never executes them (ARCH-DATA-003). There is no system ssh fallback (D-104). | None known. |
| T-20 | E | **Vulnerable dependencies** (`x/crypto/ssh`, the keyring library, `godbus`). | Minimal dependencies, `govulncheck` in CI, Terrapin-fixed x/crypto (SEC-SESSION-004). | None known. |
| T-22 | T, E | **Path traversal by a malicious SFTP server** (AC-7): listing entries named `../../.ssh/authorized_keys`, `a/b`, `.`, `..`, or with NUL or control characters, so that a download writes outside the chosen folder or the UI is spoofed. | Names are validated before they are shown or used; unsafe entries are skipped with a warning (SEC-SFTP-001). Local writes are confined to the target folder (SEC-SFTP-002). | None known. |
| T-23 | T | **Local symlink tricks**: a symlink planted in the download folder (`notes.txt -> ~/.bashrc`, or `photos -> /`) so that a download writes through it; or a race that swaps a file for a link between check and open. | Never follow a local symlink when writing: `os.Root`, `O_EXCL`/`O_NOFOLLOW` creates, `Lstat` checks, and rename into place (SEC-SFTP-002, SEC-SFTP-003). | An attacker who can write into the user's folders is a same-user process (AC-4). Accepted beyond this. |
| T-24 | D | **Resource exhaustion by a server**: millions of directory entries, endless directory depth, a file that never ends. | Listing cap and byte budget, recursion depth cap, transfers bounded by the size the server reported (SEC-SFTP-004). | A server can still make transfers slow; the user cancels. |
| T-25 | I | **The edit-locally copy is read or left behind.** | Private 0700 directory, 0600 file, deleted in every path including a crash (SEC-SFTP-005). | `SIGKILL` or power loss leaves the copy in the user's temp dir, readable only by the user. Accepted. |
| T-26 | E | **Command injection through the editor launch** (a file name with shell metacharacters, a hostile `$EDITOR`). | No shell; the file path is one argv element; `os/exec` exists in one package (SEC-SFTP-006). | `$EDITOR` is set by the user or a same-user process (AC-4). Accepted. |
| T-27 | I | **File contents or private paths in logs.** | Contents never logged; paths only at debug level; canary harness (SEC-SFTP-007). | None known. |
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
  about 3.4 KB). The message shown for an oversized private key (`app.MsgKeyTooLarge`, "Key is larger than 2800
  bytes: reference the key file instead") mentions "reference the key file". Implemented with the hint in the `app`
  message rather than in the `ErrTooLarge` sentinel text ("secrets: secret exceeds 2800 bytes"), because the
  sentinel also covers passwords and passphrases, for which a key file is no alternative
  (`credential-storage.md` §5).

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
Priority: MVP · Status: Implemented · Threats: T-21
- AC1: Release checklist (manual, macOS): store a password with build N, install build N+1 (unsigned), connect.
  No Keychain dialog appears.
- AC2 (automated, `TestSEC_CRED_009_BackendIsGoKeyring`): `secrets` uses go-keyring, and no cgo Keychain library is
  linked on darwin or linux.
- Note: AC1 cannot be automated (it needs two installed builds and the macOS Keychain UI). It is a manual step in
  `specs/acceptance.md` and `docs/release-checklist.md`.
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
  Implemented as a status-bar line ("`<label>`: host key revoked (`<file>` line `<n>`); connection refused") rather
  than a block screen, because there is nothing to compare and no action to offer (design §9.1).

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
- AC2: With a stored RSA key, `HostKeyAlgorithms` contains only `rsa-sha2-512` and `rsa-sha2-256`. Implemented
  without SHA-1 `ssh-rsa` in the pinned list, because the list is drawn from x/crypto's supported (not insecure)
  algorithms, which SEC-SESSION-004 requires.

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
Priority: MVP · Status: Implemented · Threats: T-17
- AC1: The import-rule test (`internal/archtest`, ADR-009) finds no import of `os/exec` in any sshelf package, so
  sshelf itself starts no child process that could receive a secret.
- AC2: A dependency review confirms the keyring library uses `security -i` (stdin) for set operations.
- **M3 revision** · Priority: M3 · Status: Draft · Source: D-019
  - *Revised:* The system shall never pass a secret through argv or environment variables, and shall start no child
    process other than `/usr/bin/security`, invoked by `zalando/go-keyring` on macOS through stdin and stdout (D-008),
    and the user's editor for edit-locally (FR-SFTP-019), started only by `internal/ui/editor`.
  - *Acceptance:*
    - AC1: The import-rule test (`internal/archtest`, ADR-009 as revised by ADR-019) finds `os/exec` imported by
      `internal/ui/editor` and by no other sshelf package.
    - AC2: A dependency review confirms the keyring library uses `security -i` (stdin) for set operations.
    - AC3: A unit test with `SSHELF_TEST_CANARY` in the credential store, after a connect, checks that the editor
      command's `Args` are exactly the words of `$VISUAL`/`$EDITOR`/`vi` plus the temp file path, its `Env` is
      the process environment unchanged, and neither contains the canary (sshelf never puts a secret in its own
      environment, so the editor inherits none).

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

**SEC-SESSION-005: Session output never reaches the terminal raw.**
The system shall pass remote session output only to sshelf's terminal emulator and shall write to the user's
terminal only characters and SGR styles rendered from emulator cells, so that no OSC (titles, clipboard, hyperlinks,
colour changes), DCS, APC, PM, SOS or terminal query from a server reaches the user's terminal.
Priority: M2 · Status: Implemented · Threats: T-15 · Source: D-009, ADR-012
- AC1: sshtest's `hostile` script (OSC 52 clipboard write, OSC 0/2 titles, OSC 8 link, OSC 4/10/11 colour sets,
  DCS, APC, `ESC[21t`, DECRQSS, `ESC c`, 8-bit C1 forms) runs in a session; the PTY output of sshelf contains
  none of `ESC ]`, `ESC P`, `ESC _`, `ESC ^`, `ESC X` or a C1 byte, and the window title is unchanged.
- AC2: A fuzz test feeds random bytes to a session's emulator and asserts the same property on the rendered view.
- AC3: Tab labels and the status bar show the host label and address only, never a remote-set title, and never a
  secret (canary harness, SEC-LOG-001).

**SEC-SESSION-006: Session end reasons are safe to show and log.**
When a session ends with a reason that carries server-supplied text (the exit-signal name, the disconnect message),
the system shall sanitise that text (SEC-SESSION-003), fold every run of whitespace, including newlines, to one
space, and cut the reason to 80 columns with `…`, before it is shown in the ended tab's status bar or written to
the debug log.
Priority: M2 · Status: Implemented · Threats: T-15, T-17 · Source: FR-CONN-005, ARCH-SESS-008
- AC1: Unit test: an `ExitError` whose signal name, and a `DisconnectError` whose message, hold an OSC 52 write, an
  OSC 0 title, a CSI, a C1 control, a newline, a tab and 200 extra columns render as one line of at most 80 columns
  ending in `…`, with no control rune and none of the escapes' payloads; the debug log's `session: ended` record
  holds the same safe text.
- AC2: sshtest ends a session with that hostile `exit-signal` name; `Err()` is an `*ExitError` with the safe reason.
- AC3: An ended tab on a 200-column terminal whose server sent that hostile disconnect message keeps a frame of
  exactly the terminal's height, and its status bar reason is at most 80 columns (a newline in the reason used to
  push the frame one row down).
- AC4: The reason never holds a secret: `TestSEC_LOG_001_CanaryHarnessM2` (an ended tab, a reconnect and a
  disconnect, with `SSHELF_DEBUG=1`) finds no canary on the screen or in the debug log (SEC-LOG-001).

### 4.9 File transfer (SEC-SFTP), M3

These requirements implement D-019 and D-020 for the SFTP tab (`product/requirements.md` §17). "Remote name" means
an entry name or a path the SFTP server returns (a listing, `realpath`, `readlink`, a status message); all of them
cross TB-6 and are untrusted.

**SEC-SFTP-001: Unsafe remote names are rejected.**
When a remote directory listing contains an entry whose name is empty, `.` or `..`, or contains `/`, NUL or a
control character (C0, DEL or C1), the system shall leave that entry out of the listing and out of every operation,
and shall report how many entries it left out.
Priority: M3 · Status: Draft · Threats: T-22, T-15 · Source: D-020
- AC1: sshtest's hostile SFTP mode (ARCH-TEST-004) lists `ok.txt`, `../evil`, `a/b`, `x/..`, `.`, `..`, an empty
  name, `x\x00y`, `bell\x07`, `esc\x1b[31m`, `c1\u009b`, `del\x7f` and `tab\tname`. `github.com/pkg/sftp` drops
  `.` and `..` and reduces every name to its last path element (`../evil` arrives as `evil`, `a/b` as `b`, `x/..`
  as `..`, the empty name as `.`), so sshelf applies its check to what the library returns. The pane shows exactly
  `ok.txt`, `evil` and `b`; the warning says "⚠ 8 entries with unsafe names not shown"; no shown name contains
  `/`, a control rune, or equals `.` or `..`. The check runs again on every name before it is joined to a local path.
- AC2: A recursive download of a remote directory containing those entries (the hostile handler serves content for
  every path it is asked for) creates files only as direct children of the target folder, and only for names the
  pane showed; a walk of the target folder's parent before and after finds no other change.
- AC3: Fuzz test of the name check: for every input it accepts, `filepath.Join(dir, name)` stays a direct child of
  `dir` (`filepath.Dir` of the result is `dir`), and the name contains no rune for which `unicode.IsControl` is true.

**SEC-SFTP-002: Local writes stay in the target folder and never follow symlinks.**
When the system creates, writes, renames or deletes a local file or directory for a transfer, an edit-locally upload
fallback (FR-SFTP-019) or a pane operation, it shall resolve the path under the target directory opened as an
`os.Root`, shall create files with `O_CREATE|O_EXCL` and open existing part files with `O_NOFOLLOW`, and shall never
write through a symbolic link.
Priority: M3 · Status: Draft · Threats: T-22, T-23 · Source: D-020
- AC1: With `notes.txt` in the download folder being a symlink to a file outside it, downloading `notes.txt` and
  choosing Overwrite replaces the link with a regular file; the link's former target is byte-for-byte unchanged.
- AC2: With `photos` in the download folder being a symlink to another directory, a recursive download of a remote
  `photos/` fails that entry with "exists and is not a directory" and writes nothing in the link's target.
- AC3: With `big.iso.sshelf-part` being a symlink, a download of `big.iso` does not resume through it: it fails
  with "unsafe part file" and the link's target is unchanged.
- AC4: A race test swaps a part file for a symlink between the `Lstat` check and the open (test hook): the open fails
  and nothing is written through the link.
- AC5: Deleting a local directory that contains a symlink to a directory removes the link and leaves the target's
  contents untouched (FR-SFTP-011).

**SEC-SFTP-003: Part files and atomic completion.**
When the system downloads or uploads a file, it shall write the data to `<name>.sshelf-part` in the target directory,
created with mode 0600, and only after the last byte is written and the size checked shall it set the file's mode
and modification time and rename it to the final name.
Priority: M3 · Status: Draft · Threats: T-18, T-23 · Source: D-020
- AC1: A download interrupted at 50 % leaves the old target (if any) byte-for-byte unchanged and a
  `<name>.sshelf-part` file of mode 0600; no file with the final name and partial content ever exists (polling test).
- AC2: A completed transfer whose received size differs from the size the source reported fails with "size
  changed during transfer" and is not renamed.
- AC3: Remote renames use `posix-rename@openssh.com` when the server offers it, and otherwise remove the target and
  rename (the window is documented in ADR-018).

**SEC-SFTP-004: Bounded listings, walks and transfers.**
The system shall stop reading a remote directory listing after 100,000 entries or 64 MiB of listing data, shall not
descend more than 64 directory levels in a recursive copy, delete or count, and shall stop a download once it has
received the size the server reported when the file was opened.
Priority: M3 · Status: Draft · Threats: T-24
- Note: when the server allows only one SFTP channel per tab (ADR-018), listing data cannot be told apart from
  transfer data, so the 64 MiB budget is replaced by a 30 s timeout on each listing; the 100,000-entry cap still
  applies.
- AC1: sshtest's hostile mode with 150,000 entries: the pane lists 100,000 and says "⚠ listing stopped at 100,000
  entries"; the heap stays within NFR-SIZE-004's bound.
- AC2: A directory chain 70 levels deep is copied to level 64 and the deeper entries are reported as skipped.
- AC3: A hostile file that keeps returning data past its reported size is cut at that size and fails with "size
  changed during transfer".

**SEC-SFTP-005: The edit-locally copy is private and always removed.**
When the system downloads a file for editing, it shall place it in a new directory created with mode 0700 in the
OS temp directory and create the file with mode 0600, and shall delete the file and the directory when editing
ends, whatever the outcome, and from the crash handler.
Priority: M3 · Status: Draft · Threats: T-25 · Source: D-019
- AC1: While the fake editor runs, `stat` shows the directory 0700 and the file 0600 under `umask 000`.
- AC2: After each of these endings the directory no longer exists: unchanged, changed and uploaded, editor exit
  status 1, editor not found, remote changed with each of the three answers, upload failed then discarded,
  connection lost during upload, and a panic forced in `Update` after the download (`sshelf_faultinject`).
- AC3: The copy is never in the config directory or the state directory.

**SEC-SFTP-006: The editor runs without a shell, from one package.**
When the system starts the editor, it shall run the program named by the first word of `$VISUAL`, `$EDITOR` or `vi`
directly, with the remaining words and the temp file path as separate arguments and no shell, from
`internal/ui/editor` only.
Priority: M3 · Status: Draft · Threats: T-26 · Source: D-019
- AC1: A remote file named `$(touch pwned);x.txt` is edited with a fake editor; no file `pwned` is created anywhere,
  and the editor receives the name as one argument.
- AC2: archtest: `os/exec` is imported by `internal/ui/editor` only (SEC-MEM-008 M3 revision AC1).

**SEC-SFTP-007: No file contents or paths in logs.**
The system shall never write file contents to the debug log, the screen outside the panes, a crash report or test
output, and shall write file paths and names to the debug log only at debug level.
Priority: M3 · Status: Draft · Threats: T-27, T-17
- AC1: The canary harness (SEC-LOG-001) is extended with M3 flows, with `SSHELF_DEBUG=1`: upload and download of a
  file whose content holds the canary, a transfer cancelled, a resume, edit-locally (changed, remote changed), a
  dropped connection and a reconnect. The canary content appears in no log record, crash report, stdout or stderr.
- AC2: With logging off (the default), no record about a transfer is produced at all; with it on, transfer records
  carry the path, direction, size and outcome only.
- AC3: The password canary used to connect never appears in the SFTP tab's frames (pane titles, progress line,
  modals), as for the session tab (FR-TAB-007).

**SEC-SFTP-008: Server-supplied text is sanitised in the SFTP tab.**
The system shall pass every server-supplied string shown in the SFTP tab or written to the log (status messages,
`realpath` results, symlink targets) through `domain.Sanitize` (SEC-SESSION-003), fold whitespace to single spaces,
and cut it to 80 columns with `…`.
Priority: M3 · Status: Draft · Threats: T-15
- AC1: sshtest's hostile mode answers a failed `open` with a status message holding an OSC 52 write, an OSC 0
  title, a CSI, a newline and 200 extra columns; the error line is one line of at most 80 columns of the message
  with no control rune, and the PTY output scan (SEC-SESSION-005 AC1) finds no `ESC ]`.
- AC2: A `realpath(".")` reply with control characters is shown sanitised in the pane title; operations use the
  exact bytes.

---

## 5. Requirement count

| Priority | Count |
|----------|-------|
| MVP | 31 (SEC-MEM-008 carries an M3 revision) |
| M2 | 2 |
| M3 | 8 (SEC-SFTP-001 … SEC-SFTP-008) |
| v1 | 4 |
| Later | 2 |
| **Total** | **47** |

## 6. Resolved decisions

The open decisions from earlier drafts are settled in `../decisions.md`:

| Topic | Resolution |
|-------|------------|
| "Only sshelf can read the credentials" (T-3) | Not a goal. `zalando/go-keyring` is used and same-user access is accepted. A signed native macOS backend is `Later` (D-008). |
| Touch ID or user presence per connection | Not planned (D-008). |
| Plaintext fallback when no store exists | `Later` and opt-in (D-101). |
| Secrets per device, not synced | Accepted. A synced `config.json` shows "not set on this device" (D-108). |
