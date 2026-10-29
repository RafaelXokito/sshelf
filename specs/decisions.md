# Product owner decisions

Binding decisions that override anything in other spec files. When a spec file
conflicts with this one, this one wins and the spec file must be updated.

| ID | Date | Decision | Consequence |
|----|------|----------|-------------|
| D-001 | 2026-10-29 | **TUI-only app.** The user launches `sshelf`, picks a host inside the app and connects. There are no non-interactive commands such as `sshelf connect <label>` or `sshelf list`. | Drop `FR-CLI-*` connect/list/completion requirements. The binary keeps only `--version`, `--help` and optionally `--config <dir>`. No CLI framework needed. |
| D-002 | 2026-10-29 | **No master password.** The user never creates, types or manages a password for sshelf itself. | No encrypted vault file, no Argon2/KDF, no unlock screen, no auto-lock, no change-password, no recovery key. |
| D-003 | 2026-10-29 | **Secrets live in the OS credential store.** Passwords, private keys and key passphrases are stored in the macOS Keychain or Linux Secret Service (GNOME Keyring / KWallet), so access is gated by the user's OS login (and Touch ID where the OS offers it). | Host metadata (labels, hostnames, ports, users, groups, tags, identity references) is stored in a plain config file with 0600 permissions. That file never contains secrets. Linux caveat accepted: any process running as the user can read an unlocked Secret Service keyring. |
| D-004 | 2026-10-29 | **Private keys can be stored inside sshelf (in the OS credential store) in the MVP.** Referencing a key file on disk is also supported. | Paste/import key into keychain is `MVP`. |
| D-005 | 2026-10-29 | **ssh-agent authentication is `v1`, not MVP.** | |
| D-006 | 2026-10-29 | **Open sessions are never closed by sshelf for security reasons.** (There is no lock.) | |
| D-007 | 2026-10-29 | **Keep it simple.** Prefer the smallest design that meets the requirement; no speculative security machinery. | Reviewers should flag over-engineering. |
| D-008 | 2026-10-29 | **Use `zalando/go-keyring` (pure Go) and accept that any process running as the user can read sshelf's secrets** (macOS via `/usr/bin/security`, Linux via an unlocked Secret Service). Equivalent to the exposure of `~/.ssh` key files. | No cgo, no code signing needed, no re-prompts after updates. A strict, signed native macOS backend is `Later`. Touch ID is not planned. Secrets > 2800 bytes (e.g. RSA-4096 keys) must be referenced as key files. |

## Defaults chosen by the orchestrator (change if the product owner objects)

| ID | Default |
|----|---------|
| D-101 | If no OS credential store is available (e.g. headless Linux without Secret Service), sshelf still works for key-file and ssh-agent hosts but refuses to save passwords/keys, and explains why. A plaintext fallback is `Later` and opt-in. |
| D-102 | Accepted host keys are written to sshelf's own `known_hosts` file in its config directory; `~/.ssh/known_hosts` is read but never modified. |
| D-103 | Hosts imported from `~/.ssh/config` are a one-time copy (no live sync). |
| D-104 | No fallback to the system `ssh` binary in MVP/v1 (`Later`). |
| D-105 | Host labels may contain spaces (no CLI to break). |
| D-106 | Host metadata is stored in a single `config.json` (stdlib `encoding/json`). |
| D-107 | No `--host <label>` launch flag for now (would reintroduce a CLI connect path, see D-001). Revisit `Later`. |
| D-108 | Secrets do not sync between machines. A synced config on a new machine shows affected identities as "not set on this device" and lets the user re-enter them. |
| D-109 | Bubble Tea v2 (v2.1.x) is adopted from the start. |
