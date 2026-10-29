# sshelf functional requirements

Status: Draft · Owner: product · Last updated: 2026-10-29

This file is the catalogue of functional requirements (`FR-*`). It follows the
conventions in [`../README.md`](../README.md): EARS syntax, one testable
sentence per requirement, and a priority of `MVP`, `v1` or `Later`. The product
owner decisions in [`../decisions.md`](../decisions.md) are binding and win any
conflict with this file. Background is in [`research.md`](research.md). Terms
are defined in [`../glossary.md`](../glossary.md).

Cross-references:

- `SEC-*` IDs are in `../security/requirements.md`. `SEC-CRED-*` covers the OS
  credential store.
- `ARCH-*` IDs are in `../architecture/design.md`.

This document states only the user-visible behaviour. Where it cites a `SEC-*`
or `ARCH-*` area, the exact ID is assigned there.

## 1. Product vision

sshelf is a keyboard-driven terminal app for finding and connecting to SSH
hosts. The user launches `sshelf`, types a few characters to find a host and
presses Enter to connect. sshelf brings the Termius data model to a fast,
open-source TUI: reusable identities, groups, jump-host chains, snippets and
port-forwarding rules.

- **Storage.** Host details live in a plain config file. Passwords and private
  keys live in the operating system's credential store (macOS Keychain or
  Linux Secret Service), so there is no extra password to remember and no
  account or cloud.
- **OpenSSH compatibility.** sshelf imports `~/.ssh/config` and trusts
  `~/.ssh/known_hosts`.
- **Simplicity.** sshelf is TUI-only and deliberately small. Panes and tabs are
  left to tmux and the terminal emulator.

## 2. Target personas

| Persona | Context | What they need most |
|---------|---------|---------------------|
| **Dana, platform/DevOps engineer** | 30 to 300 VMs across environments, reached through bastions. Lives in tmux. | Fuzzy find and connect, groups, jump chains |
| **Sam, consultant or MSP technician** | Many clients, each with its own credentials. Many hosts use passwords, not keys. | Saved passwords and keys without an extra master password, reusable identities, grouping by client |
| **Lee, homelab and self-hoster** | 5 to 30 machines (Raspberry Pis, NAS, VPS). Uses `~/.ssh/config` today. | Painless import from `~/.ssh/config`, port forwards to web UIs |
| **Alex, Termius refugee** | Has outgrown a paid, account-based GUI client. Wants local-first FOSS. | Termius concepts (identities, groups, snippets) that work the same way, with no account |

## 3. MVP scope summary

The MVP is the smallest usable app: **add a host (or import it), pick it from
a fuzzy-searchable list, and connect with a password or key, with host-key
verification.** It has 33 requirements.

| Area | MVP | Deferred to v1 / Later |
|------|-----|------------------------|
| Credentials (`FR-CRED`) | Save secrets in the OS store with no sshelf password; clear message when the store is unavailable | Opt-in plaintext fallback (Later) |
| Hosts (`FR-HOST`) | Add or edit, delete, validate, credentials in the host form | Clone, notes, favourites, reachability check, copy command, details pane |
| Identities (`FR-IDENT`) | Manage identities, link to hosts with a username override, key from a file or stored in sshelf, never show secrets | Save a typed password, key generation and deployment, reveal a password |
| Connect (`FR-CONN`) | Session, key, password or keyboard-interactive auth, prompt for missing secrets, resize, return to list, clear errors | ssh-agent, keepalive, reconnect, agent forwarding, startup command, quick connect, `~.` escape |
| Known hosts (`FR-KH`) | Unknown-key prompt (trust and save or cancel), changed-key block with removal instructions, trust `~/.ssh/known_hosts`, sshelf's own `known_hosts` file | Trust once, known-hosts view, certificate authorities |
| Navigation (`FR-NAV`) | Welcome screen, list, fuzzy search with Enter to connect, key bindings, footer hints | Highlighting, help overlay, recent, sort, filters, multi-select |
| Import (`FR-IMPORT`) | `~/.ssh/config` with `Include` and wildcard resolution; result report | Jumps and forwards on import, `ssh_config` export, CSV, other clients |
| Storage and settings (`FR-CFG`) | Config directory, durable writes, safe handling of a broken file | External-change guard, `NO_COLOR`, themes, key bindings, defaults, settings view |
| Command line (`FR-CLI`) | `--help`/`--version`, otherwise start the TUI | `--config <dir>` |
| Groups, tags, snippets, forwarding, jump hosts, SFTP, history, tabs | None | v1 / Later |

## 4. Requirement format

Each requirement uses this layout:

> **ID · Title**
> One EARS sentence.
> Priority · Status · Source (optional)
> Acceptance criteria as bullets.

All requirements in this file have status `Draft`.

---

## 5. Credentials (`FR-CRED`)

Secrets (passwords, private keys, key passphrases) are stored in the OS
credential store (D-003). Host metadata is stored in a plain config file that
never contains secrets. Store access, item naming and file permissions are
defined in `SEC-CRED-*`.

### FR-CRED-001 · Save secrets without an sshelf password
When the user saves a password, private key or key passphrase, the system shall store it in the OS credential store without asking for any sshelf-specific password.

- **Priority:** MVP · **Status:** Draft · **Source:** D-002, D-003
- **Acceptance:**
  - sshelf never shows a screen for creating, entering or changing a master password.
  - After a restart, a host whose identity has a saved password connects without a prompt.
  - Any OS-level approval (for example a keyring unlock dialog) is the only interruption the user sees. Touch ID is not used (D-008).
  - The config file contains none of the saved secret values (checked with `grep`).

### FR-CRED-002 · Credential store unavailable
If the OS credential store is unavailable, then the system shall refuse to save passwords, keys and passphrases, explain why and how to fix it, and keep working for hosts that need no stored secret.

- **Priority:** MVP · **Status:** Draft · **Source:** D-101, SEC-CRED-005
- **Acceptance:**
  - On Linux without a running Secret Service, saving a password shows a message naming the missing service (for example "No Secret Service found: install or start GNOME Keyring or KWallet"), and nothing is written.
  - Hosts with a key file, or authentication through a prompt, still connect.
  - Hosts whose stored secret cannot be read fall back to the prompt in FR-CONN-003.

### FR-CRED-003 · Plaintext fallback
Where the user has explicitly enabled the plaintext secret fallback, the system shall store secrets in a file readable only by the user when no OS credential store is available.

- **Priority:** Later · **Status:** Draft · **Source:** D-101
- **Acceptance:**
  - Enabling it requires typing "yes" after a warning. It is off by default.

---

## 6. Hosts (`FR-HOST`)

### FR-HOST-001 · Add or edit a host
When the user submits the host form, the system shall save the host (new or edited) and select it in the host list.

- **Priority:** MVP · **Status:** Draft · **Source:** Termius parity
- **Acceptance:**
  - MVP fields are label, hostname (DNS name, IPv4 or IPv6), port (default 22), username (optional) and either an identity (optional) or credentials entered directly (FR-HOST-010). v1 adds group, tags, notes, jump hosts, forwarding rules and startup command.
  - Changing the port from 22 to 2222 shows `:2222` in the list, and the next connection uses port 2222.
  - Esc discards the form. The saved host is still present after a restart.

### FR-HOST-002 · Delete a host
When the user confirms deletion of a host, the system shall remove the host.

- **Priority:** MVP · **Status:** Draft
- **Acceptance:**
  - The confirmation names the host and defaults to "No".
  - A linked shared identity is not deleted. A hidden per-host identity (FR-HOST-010) is deleted with its stored secrets.
  - From v1, deleting a host used as a jump host is blocked (FR-JUMP-003).

### FR-HOST-003 · Field validation
If the label is empty, longer than 64 characters or equal (case-insensitive) to another host's label, or the hostname is empty or contains whitespace, or the port is outside 1 to 65535, then the system shall reject the form and show the error next to the offending field.

- **Priority:** MVP · **Status:** Draft · **Source:** D-105 (labels may contain spaces)
- **Acceptance:**
  - Label `Web Prod EU` is accepted. With `web-prod` saved, `Web-Prod` is rejected with "Label already in use".
  - Ports `0`, `65536` and `abc` are rejected. `65535` is accepted.
  - Hostname `my host` is rejected. `2001:db8::1` is accepted.

### FR-HOST-004 · Clone a host
When the user triggers the clone action on a host, the system shall open the host form pre-filled with that host's fields and the label suffixed with " copy".

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - Cloning `web-prod` opens the form with every field copied and the label `web-prod copy`. Esc saves nothing.

### FR-HOST-005 · Host notes
The system shall store an optional free-text note of up to 4,096 characters for each host and show it in the host details pane.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - A multi-line note is shown intact after a restart. Notes are included in fuzzy search.

### FR-HOST-006 · Favourites
When the user toggles favourite on a host, the system shall mark it as a favourite and list favourites in a "Favourites" section at the top of the list.

- **Priority:** v1 · **Status:** Draft · **Source:** lazyssh "pin"
- **Acceptance:**
  - Toggling twice restores the original state. A favourite also stays listed under its group.

### FR-HOST-007 · Reachability check
When the user triggers the check action on a host, the system shall open a TCP connection to the host's address and port and show "reachable" with the round-trip time, or the failure reason.

- **Priority:** v1 · **Status:** Draft · **Source:** lazyssh "ping"
- **Acceptance:**
  - No SSH authentication is attempted.
  - Hosts with a jump chain show "not checked: uses jump host".

### FR-HOST-008 · Copy the ssh command
When the user triggers the copy-command action on a host, the system shall copy an equivalent OpenSSH command line, without any secret, to the clipboard.

- **Priority:** v1 · **Status:** Draft · **Source:** lazyssh
- **Acceptance:**
  - For user `ubuntu`, host `10.0.0.10` and port `2222`, the clipboard holds `ssh -p 2222 ubuntu@10.0.0.10`.
  - Jump hosts appear as `-J`, and key files as `-i <path>`. Keys stored in sshelf are omitted.
  - OSC 52 is used when no system clipboard is available.

### FR-HOST-009 · Host details pane
While the details pane is open, the system shall show the selected host's effective settings and, for each inherited value, the group it came from.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - A host with no port set, in a group with default port 2200, shows `Port 2200 (from group "prod")`.
  - No secret is shown.

### FR-HOST-010 · Credentials in the host form
When the user enters a username plus a password or a key (stored or file path) directly in the host form instead of choosing an identity, the system shall save them as a hidden identity linked only to that host.

- **Priority:** MVP · **Status:** Draft · **Source:** ARCH `Identity.Hidden`
- **Acceptance:**
  - A first-time user adds a host with a username and password and connects without opening the identities view.
  - The hidden identity is not listed in the identities view. Its secrets are stored as in FR-CRED-001.
  - Editing the host's credentials updates the hidden identity. Choosing a shared identity instead deletes the hidden one and its secrets.

---

## 7. Groups (`FR-GROUP`)

### FR-GROUP-001 · Assign a host to a group
The system shall allow each host to belong to at most one group and shall display hosts under a heading for their group, with ungrouped hosts last.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - Groups and the hosts within them are sorted alphabetically.

### FR-GROUP-002 · Manage groups
When the user creates, renames or deletes a group, the system shall apply the change to the group and to every host assigned to it.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - Renaming `prod` to `production` moves every `prod` host.
  - Deleting a non-empty group asks for confirmation and then moves its hosts to "Ungrouped". No host is deleted.
  - Duplicate group names (case-insensitive) are rejected.

### FR-GROUP-003 · Nested groups
The system shall allow a group to have a parent group, up to 4 levels deep, and shall display nested groups as an indented tree.

- **Priority:** v1 · **Status:** Draft · **Source:** Termius, Royal TSX
- **Acceptance:**
  - A group 4 levels deep is accepted and a fifth level is rejected. The tree shows each level indented under its parent.

### FR-GROUP-004 · Collapse and expand
When the user toggles a group heading, the system shall hide or show the hosts and subgroups under it, and shall remember that state across restarts.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - While a search is active, groups containing matches are expanded.

### FR-GROUP-005 · Inherited defaults
Where a group defines a default username, port, identity, jump chain or keepalive interval, the system shall apply that default to each host in the group, and in its subgroups, that does not set the field itself.

- **Priority:** v1 · **Status:** Draft · **Source:** Termius group settings, Royal TSX credential inheritance
- **Acceptance:**
  - The precedence is: the host's value, then the nearest ancestor group's value, then the global default (FR-CFG-008).
  - Changing a group default affects the next connection of every host that does not override it.
  - Clearing a host field restores inheritance.

---

## 8. Tags (`FR-TAG`)

### FR-TAG-001 · Tag hosts
The system shall allow up to 16 tags per host, each a lowercase string of 1 to 32 characters from `a-z`, `0-9`, `-` and `_`.

- **Priority:** v1 · **Status:** Draft · **Source:** Termius, lazyssh
- **Acceptance:**
  - Tags are shown in the list as `#tag`. Input `Prod` is stored as `prod`.

### FR-TAG-002 · Filter by tag
When the search query contains a token `#<tag>`, the system shall show only hosts with that tag and apply the rest of the query as a fuzzy search.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - Multiple tag tokens are combined with AND.

### FR-TAG-003 · Rename a tag everywhere
When the user renames a tag, the system shall replace it on every host that has it and merge it with any existing tag of the new name.

- **Priority:** Later · **Status:** Draft
- **Acceptance:**
  - Renaming `db` to `pg` on 3 hosts, one of which already has `pg`, leaves each of the 3 hosts with exactly one `pg` tag and no `db` tag.

---

## 9. Identities and keys (`FR-IDENT`)

An identity is a reusable, named set of credentials: a username plus an
optional password and/or private key and passphrase. Credentials are attached
only to identities, never directly to a host, so changing a password in one
place updates every linked host. Secret values are stored per FR-CRED-001.

### FR-IDENT-001 · Manage identities
When the user creates, edits or deletes an identity, the system shall save the change and apply it to every linked host from its next connection on.

- **Priority:** MVP · **Status:** Draft · **Source:** Termius identities
- **Acceptance:**
  - The fields are name (unique, case-insensitive), username, password (optional), key (optional) and key passphrase (optional). Secret fields do not echo.
  - Changing the password of an identity linked to 3 hosts lets all 3 connect with the new password, with no host edits.
  - Deleting asks for confirmation, states the number of linked hosts, unlinks them, and removes the identity's secrets from the OS credential store.

### FR-IDENT-002 · Link a host to an identity
When a host is linked to an identity, the system shall authenticate that host with the identity's credentials, using the host's own username when the host sets one and the identity's username otherwise.

- **Priority:** MVP · **Status:** Draft · **Rationale:** avoids Royal TSX's need for duplicate credentials per username.
- **Acceptance:**
  - Identity `ops` (user `deploy`, key K) on host A, which has no username, connects as `deploy` with K. Host B, linked to `ops` with username `root`, connects as `root` with K.

### FR-IDENT-003 · Key from a file or stored in sshelf
Where an identity has a private key, the system shall take it either from a referenced file path read at connection time or from key contents that the user pasted or imported into sshelf and that are stored in the OS credential store.

- **Priority:** MVP · **Status:** Draft · **Source:** D-004, Termius keychain
- **Acceptance:**
  - A path starting with `~` is expanded. A missing file fails with "key file not found: <path>".
  - After a key file is imported into sshelf, deleting the original file does not stop connections.
  - Ed25519, ECDSA and RSA keys are accepted in OpenSSH, PEM and PKCS#8 formats. Pasting an invalid key is rejected on save.
  - The identity shows the key type and SHA256 fingerprint.
  - A pasted or imported key larger than 2800 bytes (for example RSA-4096) is rejected with a hint to reference the key file instead (D-008, SEC-CRED-003).

### FR-IDENT-004 · Never show secrets
The system shall never display a stored password, passphrase or private key in any view, log or error message.

- **Priority:** MVP · **Status:** Draft · **Source:** SEC-LOG-005
- **Acceptance:**
  - Each secret field in the identity form shows "(set)" or "(not set)", never the value or its length.
  - Grepping the debug log after a password login finds no occurrence of the password.

### FR-IDENT-005 · Save a password typed at connection time
When a password typed at a connection prompt authenticates successfully and the user chooses "save", the system shall store it in the host's identity, or in a new identity named after the host label if the host has none.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - A password that failed authentication is never offered for saving. "Don't save" stores nothing.

### FR-IDENT-006 · Generate a key pair
When the user requests a new key for an identity, the system shall generate an Ed25519 key pair, store the private key in sshelf and show the public key in OpenSSH format with a one-key copy action.

- **Priority:** v1 · **Status:** Draft · **Source:** Termius, SecureCRT
- **Acceptance:**
  - The generated public key is accepted by `ssh-keygen -l -f`, and the private key is stored in the OS credential store, not on disk.

### FR-IDENT-007 · Deploy a public key
When the user triggers deploy-key for a host, the system shall append the identity's public key to `~/.ssh/authorized_keys` on that host unless the key is already present.

- **Priority:** v1 · **Status:** Draft · **Source:** `ssh-copy-id`
- **Acceptance:**
  - Running it twice leaves exactly one copy of the key on the server.
  - A missing `~/.ssh` or `authorized_keys` is created with mode `700` or `600` respectively.

---

## 10. Connecting and sessions (`FR-CONN`)

sshelf opens SSH sessions with its own SSH client and hands the whole terminal
to the session while it runs. The TUI resumes when the session ends. The flow
is described in `ARCH-*`. There is no fallback to the system `ssh` binary
(D-104). sshelf never closes open sessions for security reasons (D-006).

### FR-CONN-001 · Open an interactive session
When the user presses Enter on a host, the system shall open an interactive SSH shell session to that host in the current terminal, with the full screen given to the session until it ends.

- **Priority:** MVP · **Status:** Draft
- **Acceptance:**
  - `vim`, `htop` and `tmux` render and accept input correctly.
  - The remote PTY uses the local `TERM` value and the current window size.

### FR-CONN-002 · Authenticate with stored credentials
Where the effective identity has a private key or a password, the system shall authenticate with it without prompting, trying the key (stored or file) first and then password or keyboard-interactive (FR-CONN-008).

- **Priority:** MVP · **Status:** Draft
- **Acceptance:**
  - A host accepting only the identity's key connects without a prompt when the key is unencrypted or its passphrase is stored.
  - Given both a key and a password, against a server accepting both, the key is used (visible in the debug log).
  - Methods the server does not offer are skipped.

### FR-CONN-003 · Prompt for missing secrets
If the server requests a password, or a key passphrase is needed, and none is stored, then the system shall prompt for it with a non-echoing field and continue authentication.

- **Priority:** MVP · **Status:** Draft
- **Acceptance:**
  - The prompt shows `user@host` and what is needed ("password" or "passphrase for <key>").
  - Esc cancels and returns to the list. 3 failed attempts end with "authentication failed".

### FR-CONN-004 · Window resize
While an interactive session is active, when the local terminal is resized, the system shall send the new size to the remote PTY.

- **Priority:** MVP · **Status:** Implemented
- **Acceptance:**
  - After a resize, `tput cols; tput lines` on the remote host prints the new size.

### FR-CONN-005 · Return to the list on exit
When an interactive session ends, the system shall restore the host list with the same host selected and show either the remote exit status or the disconnect reason in the status bar.

- **Priority:** MVP · **Status:** Implemented
- **Acceptance:**
  - `exit 3` shows "web-prod: exited (3)". A server shutdown shows "web-prod: connection lost: <reason>".
  - The terminal is fully restored, with no raw mode and no remote screen remnants.

### FR-CONN-006 · Connection failure reporting
If a connection cannot be established within the connect timeout, then the system shall abort it and report one of: name resolution failed, connection refused, timed out, host key rejected, or authentication failed.

- **Priority:** MVP · **Status:** Draft
- **Acceptance:**
  - Each cause is reproduced in a test and shows its own message.
  - The timeout is 10 s. Esc cancels a pending connection immediately.

### FR-CONN-007 · ssh-agent authentication
Where the `SSH_AUTH_SOCK` environment variable points to a running agent, the system shall offer the agent's keys for public-key authentication after the identity's own key and before password or keyboard-interactive authentication.

- **Priority:** v1 · **Status:** Draft · **Source:** D-005
- **Acceptance:**
  - With the right key in the agent and an identity with no key or password, the connection succeeds without a prompt.
  - With an identity key and an agent, the test server sees the identity key offered first.
  - A per-host setting "Don't use agent" disables this.

### FR-CONN-008 · Keyboard-interactive authentication
When the server sends keyboard-interactive challenges, the system shall answer a single non-echoing password prompt with the identity's stored password and show every other prompt, such as an OTP, to the user, echoing input only when the server allows it.

- **Priority:** MVP · **Status:** Draft · **Rationale:** many servers accept passwords only through keyboard-interactive; OTP and 2FA bastions. **Source:** SEC-SESSION-002, ARCH-CONN-002
- **Acceptance:**
  - A server that accepts passwords only through keyboard-interactive connects with the stored password and no prompt.
  - A test server with a password prompt and an OTP prompt fills the password and shows only the OTP prompt. The OTP field echoes only when the server sets echo.
  - With no stored password, the password prompt is shown to the user as in FR-CONN-003.

### FR-CONN-009 · Keepalive
While a session is active, the system shall send a keepalive request at the effective keepalive interval and treat the connection as lost after 3 consecutive unanswered requests.

- **Priority:** v1 · **Status:** Draft · **Source:** OpenSSH `ServerAliveInterval`
- **Acceptance:**
  - The default interval is 30 s. `0` disables keepalives.

### FR-CONN-010 · Reconnect after a drop
When a session ends because the connection was lost, not because the remote shell exited, the system shall offer to reconnect to the same host with a single key.

- **Priority:** v1 · **Status:** Draft · **Source:** Tabby, Termius
- **Acceptance:**
  - After a drop, the status bar shows "connection lost: press r to reconnect". After a normal `exit`, no reconnect is offered.

### FR-CONN-011 · Agent forwarding
Where agent forwarding is enabled for a host, the system shall forward the local ssh-agent to that host.

- **Priority:** v1 · **Status:** Draft · **Source:** SEC-AGENT-002
- **Acceptance:**
  - Agent forwarding is off by default, and enabling it shows a warning.

### FR-CONN-012 · Startup command
Where a host defines a startup command, the system shall run that command as the session's remote command instead of the login shell.

- **Priority:** v1 · **Status:** Draft · **Source:** OpenSSH `RemoteCommand`
- **Acceptance:**
  - `tmux new -A -s main` attaches to, or creates, the remote tmux session `main`.

### FR-CONN-013 · Quick connect
When the user enters `[user@]host[:port]` in the quick-connect prompt, the system shall connect without saving a host and then offer to save it.

- **Priority:** v1 · **Status:** Draft · **Source:** Tabby, Termius
- **Acceptance:**
  - `root@10.0.0.5:2222` connects as `root` on port 2222 and creates no host until the user accepts the save offer.

### FR-CONN-014 · Disconnect escape
While an interactive session is active, when the user types `~.` at the start of a line, the system shall close the session.

- **Priority:** v1 · **Status:** Draft · **Source:** OpenSSH escape characters
- **Acceptance:**
  - `~~` sends a literal `~`.

### FR-CONN-015 · Connection hooks
Where a host defines an on-connect or on-disconnect local command, the system shall run it when the session starts or ends, with the host label in the environment variable `SSHELF_HOST`.

- **Priority:** Later · **Status:** Draft · **Source:** assh
- **Acceptance:**
  - An on-connect command that writes `$SSHELF_HOST` to a file records the host label. Commands run only for hosts that define them.

### FR-CONN-016 · Mosh
Where a host has Mosh enabled and `mosh-client` is installed, the system shall bootstrap the session over SSH and hand it over to `mosh-client`.

- **Priority:** Later · **Status:** Draft · **Source:** Termius
- **Acceptance:**
  - With `mosh-client` missing, connecting shows "mosh-client not found" and makes no SSH connection.

### FR-CONN-017 · Environment variables
Where a host defines environment variables, the system shall send them in the session's environment requests.

- **Priority:** Later · **Status:** Draft · **Source:** OpenSSH `SetEnv`
- **Acceptance:**
  - With `LANG=C.UTF-8` defined on a host, the test server receives an `env` request for `LANG`. A server rejection does not fail the session.

---

## 11. Known hosts (`FR-KH`)

The trust model and matching rules are defined in `SEC-HOSTKEY-*`.

### FR-KH-001 · Unknown host key prompt
When a server presents a host key that is not trusted for that host and port, the system shall show the host, port, key type and SHA256 fingerprint and ask the user to choose "Trust and save" or "Cancel" before any credentials are sent.

- **Priority:** MVP · **Status:** Draft
- **Acceptance:**
  - The fingerprint format matches `ssh-keygen -lf`.
  - Enter and Esc both mean "Cancel", which aborts the connection. "Trust and save" connects and suppresses the prompt next time.
  - "Trust once" is not offered in the MVP (FR-KH-007).
  - The test server receives no authentication request before the choice.

### FR-KH-002 · Changed host key
If a server presents a host key that differs from a trusted key of the same type for that host and port, then the system shall abort the connection before authentication and show both fingerprints with a possible-attack warning.

- **Priority:** MVP · **Status:** Draft · **Source:** SEC-HOSTKEY-005
- **Acceptance:**
  - No "connect anyway" option is offered.
  - The block screen shows the path and line number of the stale entry (in sshelf's `known_hosts`, or in `~/.ssh/known_hosts` when the old key is there) and tells the user how to remove it, as OpenSSH does (for example "delete line 12 of <path>" or `ssh-keygen -R <host> -f <path>`).
  - An in-app remove action is v1 (FR-KH-005).

### FR-KH-003 · Trust OpenSSH known hosts
The system shall treat host keys in `~/.ssh/known_hosts` as trusted, including hashed entries, `[host]:port` entries and comma-separated host patterns, and shall never modify that file.

- **Priority:** MVP · **Status:** Draft · **Source:** D-102
- **Acceptance:**
  - A host present in `~/.ssh/known_hosts` under a hashed name connects with no prompt.
  - `~/.ssh/known_hosts` has the same checksum before and after any sshelf action.
  - A `@revoked` entry causes rejection with "host key revoked".

### FR-KH-004 · sshelf known_hosts file
When the user chooses "trust and save", the system shall append the key to sshelf's own `known_hosts` file in its config directory.

- **Priority:** MVP · **Status:** Draft · **Source:** D-102
- **Acceptance:**
  - The trust survives a restart.
  - The file uses the OpenSSH format (`ssh-keygen -F <host> -f <file>` finds the entry).

### FR-KH-005 · Manage trusted keys
When the user opens the known-hosts view, the system shall list sshelf's trusted keys with host, port, key type and fingerprint, and allow searching and deleting them.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - After an entry is deleted, the next connection shows the FR-KH-001 prompt.
  - Entries from `~/.ssh/known_hosts` are shown read-only and marked "OpenSSH".

### FR-KH-006 · Host certificate authorities
The system shall accept a host certificate signed by a CA listed with `@cert-authority` in a trusted known-hosts file when the certificate is valid for the hostname.

- **Priority:** Later · **Status:** Draft
- **Acceptance:**
  - A host certificate signed by a listed CA connects with no prompt. An expired certificate, or one for another hostname, is rejected.

### FR-KH-007 · Trust once
When the unknown-host-key prompt is shown, the system shall also offer "Trust once", which connects without saving the key.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - After "Trust once", sshelf's `known_hosts` is unchanged and the next connection shows the FR-KH-001 prompt again.

---

## 12. Navigation and search (`FR-NAV`)

### FR-NAV-001 · Host list
The system shall display each host as one line showing the label and `user@hostname:port`, sorted by label.

- **Priority:** MVP · **Status:** Draft
- **Acceptance:**
  - Port 22 is omitted from the display.
  - Lines wider than the terminal are truncated with `…`.
  - With no hosts, the Welcome screen (FR-NAV-013) is shown instead of the list.

### FR-NAV-002 · Fuzzy search
When the user types in the search field, the system shall filter the host list to hosts whose label, hostname or username fuzzy-matches the query, ordered by match score, and Enter shall connect to the selected result.

- **Priority:** MVP · **Status:** Draft · **Source:** sshs, lazyssh
- **Acceptance:**
  - `/` focuses search. `wbprd` matches `web-prod`, and `10.0.1` matches `10.0.1.20`. Matching is case-insensitive.
  - Typing `wbprd` and pressing Enter connects to `web-prod` when it is the top match.
  - Esc clears the query and returns focus to the list.
  - v1 adds group, tag and note matching.

### FR-NAV-003 · Key bindings
The system shall provide these bindings in the host list: ↑/↓ or `k`/`j` to move, `g`/`G` for first/last, `/` to search, `Enter` to connect, `a` to add, `e` to edit, `d` to delete, `i` for identities, `I` to import, and `q` or `Ctrl+C` to quit.

- **Priority:** MVP · **Status:** Draft
- **Acceptance:**
  - Each binding is covered by a UI test.
  - While the search field is focused, letter keys type into the query.
  - Ctrl+C inside a form cancels the form and does not quit.
  - In any other view, Esc goes back to the previous view. `q` quits only from the host list.

### FR-NAV-004 · Key hints footer
The system shall show a one-line footer with the main key bindings of the current view.

- **Priority:** MVP · **Status:** Draft
- **Acceptance:**
  - The footer on the host list shows at least the connect, search, add and quit bindings, and it changes when another view opens.

### FR-NAV-005 · Match highlighting
While a search query is active, the system shall highlight the matched characters in each listed host.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - With `NO_COLOR`, matches are shown in bold or underline.

### FR-NAV-006 · Help overlay
When the user presses `?`, the system shall show an overlay listing every key binding of the current view, and close it on `?` or Esc.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - On the host list, every binding in FR-NAV-003 appears in the overlay. `?` and Esc both close it.

### FR-NAV-007 · Terminal too small
If the terminal is smaller than 60 columns or 12 rows, then the system shall show only a "Terminal too small (need 60×12)" message until it is resized.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - At 59×24 or 80×11 only the message is shown. Resizing to 60×12 restores the previous view with its state intact.

### FR-NAV-008 · Recent hosts
The system shall show the 5 most recently connected hosts in a "Recent" section at the top while no search is active.

- **Priority:** v1 · **Status:** Draft · **Source:** Termius, Royal TSX
- **Acceptance:**
  - The section is hidden when `show_recent = false` or when no host has been connected yet.

### FR-NAV-009 · Sort order
When the user cycles the sort mode, the system shall order hosts within each group by label, by last connection time, or by hostname, and remember the mode.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - Each sort mode orders a fixture list as expected, and the chosen mode is still active after a restart.

### FR-NAV-010 · Group filter token
When the search query contains a token `g:<name>`, the system shall show only hosts in groups whose path contains `<name>`, including subgroups.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - `g:prod` shows hosts in `prod` and `prod/eu` and hides hosts in `staging`.

### FR-NAV-011 · Multi-select
When the user presses Space on a host, the system shall toggle its selection and show the number of selected hosts in the footer.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - The selection drives multi-host snippet runs (FR-SNIP-004). Esc clears it.

### FR-NAV-012 · Mouse support
Where mouse support is enabled, the system shall select a host on click, connect on double-click and scroll the list with the wheel.

- **Priority:** Later · **Status:** Draft
- **Acceptance:**
  - With mouse support off (the default), clicks are ignored.

### FR-NAV-013 · Welcome screen
While `config.json` contains no hosts, the system shall show a Welcome screen offering "Import from ~/.ssh/config" and "Add host" instead of the host list.

- **Priority:** MVP · **Status:** Draft
- **Acceptance:**
  - The first launch with no config directory shows the Welcome screen.
  - `I` starts the import (FR-IMPORT-001) and `a` opens the host form. `q` quits.
  - After the first host is saved or imported, the host list is shown. Deleting the last host shows the Welcome screen again.

---

## 13. Snippets (`FR-SNIP`)

### FR-SNIP-001 · Manage snippets
When the user creates, edits or deletes a snippet, the system shall store or remove a named command of one or more lines with an optional description.

- **Priority:** v1 · **Status:** Draft · **Source:** Termius snippets, SecureCRT Command Manager
- **Acceptance:**
  - Snippet names are unique (case-insensitive).
  - The UI warns that snippets are saved in the plain config file, so they should not contain secrets.

### FR-SNIP-002 · Snippet variables
When a snippet that contains `{{name}}` or `{{name:default}}` placeholders is run, the system shall prompt once for each distinct variable and substitute the values before running it.

- **Priority:** v1 · **Status:** Draft · **Source:** Warp Workflows
- **Acceptance:**
  - `systemctl restart {{svc:nginx}}` prompts for `svc`, pre-filled with `nginx`. `\{{` escapes a literal `{{`.

### FR-SNIP-003 · Run a snippet on one host
When the user runs a snippet on a host, the system shall execute it non-interactively over SSH and show its output and exit status in a scrollable result view.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - Esc cancels a running snippet.

### FR-SNIP-004 · Run a snippet on many hosts
When the user runs a snippet with several hosts selected, the system shall execute it on all of them with at most the configured number of concurrent connections and show each host's status (pending, running, ok or failed) and output.

- **Priority:** v1 · **Status:** Draft · **Source:** Termius, MobaXterm multi-exec
- **Acceptance:**
  - The default concurrency is 5. A failure on one host does not stop the others. The summary shows the counts of ok and failed hosts.

### FR-SNIP-005 · No prompts in batch runs
If a host in a multi-host run needs a host-key decision or a secret that is not stored, then the system shall mark that host as failed with the reason and continue with the others.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - In a 3-host run where one host has an unknown host key, that host is marked failed with "host key not trusted" and the other 2 complete.

### FR-SNIP-006 · Save run output
When the user saves the results of a snippet run, the system shall write each host's output and exit status to a text file the user chooses.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - The saved file contains one section per host with its label, exit status and output.

### FR-SNIP-007 · Snippet folders
The system shall allow snippets to be organised in named folders and shall fuzzy-search snippets by name, description and command.

- **Priority:** Later · **Status:** Draft
- **Acceptance:**
  - Searching `restart` finds a snippet whose command, but not name, contains `restart`.

### FR-SNIP-008 · Paste a snippet into a live session
While an interactive session is active, when the user types `~s` at the start of a line, the system shall open the snippet picker and type the chosen snippet into the session without pressing Enter.

- **Priority:** Later · **Status:** Draft
- **Acceptance:**
  - The chosen snippet's text reaches the remote shell with no trailing newline.

---

## 14. Port forwarding (`FR-FWD`)

### FR-FWD-001 · Define forwarding rules
The system shall allow a host to have named forwarding rules of type local (`-L`), remote (`-R`) or dynamic SOCKS5 (`-D`), each with a bind address defaulting to `127.0.0.1`.

- **Priority:** v1 · **Status:** Draft · **Source:** Termius, OpenSSH
- **Acceptance:**
  - A rule saved with no bind address uses `127.0.0.1`. Each of the three types can be saved and is shown with its type.

### FR-FWD-002 · Auto-start with the session
Where a forwarding rule is marked auto-start, the system shall open it when an interactive session to its host starts and close it when the session ends.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - With local rule `8080 → localhost:80`, `curl localhost:8080` reaches the remote web server only while the session is open.

### FR-FWD-003 · Tunnel-only mode
When the user starts a host's tunnels without a shell, the system shall connect, open all of the host's forwarding rules without a remote shell, and keep them open until the user stops them or quits.

- **Priority:** v1 · **Status:** Draft · **Source:** `ssh -N`, MobaXterm tunnel manager
- **Acceptance:**
  - The TUI stays usable while the tunnels are open.

### FR-FWD-004 · Tunnels view
When the user opens the tunnels view, the system shall list every active forwarding rule with its host, type, endpoints and state, and let the user stop any one of them.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - Stopping one rule closes its listener, and the other rules keep working.

### FR-FWD-005 · Bind failure
If a forwarding rule cannot bind its port, then the system shall mark that rule as failed with the reason and keep the session and the other rules running.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - With local port 8080 already in use, the rule shows "failed: address already in use" and the shell session stays open.

### FR-FWD-006 · Non-loopback bind warning
If a forwarding rule's bind address is not a loopback address, then the system shall require explicit confirmation when the rule is saved.

- **Priority:** v1 · **Status:** Draft · **Source:** `SEC-*`
- **Acceptance:**
  - Saving a rule with bind address `0.0.0.0` asks for confirmation. Declining leaves the rule unsaved.

### FR-FWD-007 · Quit with active tunnels
If the user quits while tunnels are active, then the system shall list them and ask for confirmation before closing them and exiting.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - With 2 active tunnels, `q` lists both and asks for confirmation. Declining keeps sshelf and the tunnels running.

### FR-FWD-008 · Auto-restart dropped tunnels
Where a tunnel-only connection is marked auto-restart, when its connection drops, the system shall reconnect with exponential backoff from 1 s up to 60 s.

- **Priority:** Later · **Status:** Draft
- **Acceptance:**
  - After a simulated drop, reconnect attempts happen at about 1, 2, 4 … s, capped at 60 s.

---

## 15. Jump hosts and proxies (`FR-JUMP`)

### FR-JUMP-001 · Jump-host chain
Where a host has an ordered chain of jump hosts, the system shall connect through each jump host in order, using that jump host's own address, identity and host-key verification, before connecting to the target.

- **Priority:** v1 · **Status:** Draft · **Source:** Termius host chaining, OpenSSH `ProxyJump`
- **Acceptance:**
  - Jump hosts are saved hosts chosen from a picker. Each hop gets its own FR-KH-001 prompt when needed.
  - The target's credentials are never sent to a jump host.

### FR-JUMP-002 · Inherited jump chain
Where a host has no jump chain of its own and its group defines one, the system shall use the group's chain.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - A host can choose "direct (no jump)" to override an inherited chain.

### FR-JUMP-003 · Chain integrity
If a jump chain would include the host itself, form a cycle, or reference a deleted host, then the system shall reject the change and say why.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - A → B → A is rejected with "cycle: A → B → A".
  - Deleting a host used as a jump host is blocked, and its dependants are listed.

### FR-JUMP-004 · Hop-specific errors
If a connection through a jump chain fails, then the system shall report which hop failed and why.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - Example message: "via bastion-eu (hop 2/3): authentication failed".

### FR-JUMP-005 · SOCKS5 and HTTP proxy
Where a host is configured with a SOCKS5 or HTTP CONNECT proxy, the system shall open the first TCP connection through that proxy.

- **Priority:** Later · **Status:** Draft
- **Acceptance:**
  - Through a test SOCKS5 proxy and a test HTTP CONNECT proxy, the connection succeeds and the proxy logs the target address.

### FR-JUMP-006 · Fallback gateways
Where a host lists alternative first-hop jump hosts, the system shall try them in order until one connects.

- **Priority:** Later · **Status:** Draft · **Source:** assh gateways
- **Acceptance:**
  - With the first gateway down, the connection succeeds through the second, and the status shows which gateway was used.

---

## 16. Import and export (`FR-IMPORT`)

Imports are a one-time copy. sshelf does not keep watching `~/.ssh/config`
(D-103).

### FR-IMPORT-001 · Import from `~/.ssh/config`
When the user triggers import, the system shall create one host for each `Host` alias in `~/.ssh/config` that has no wildcard, mapping `HostName`, `User`, `Port` and `IdentityFile`, and linking hosts that share the same user and key files to a single identity.

- **Priority:** MVP · **Status:** Draft · **Source:** Termius, sshs, lazyssh
- **Acceptance:**
  - `Host web` with `HostName 10.0.0.10`, `User ubuntu` and `IdentityFile ~/.ssh/id_ed25519` creates host `web` linked to an identity with user `ubuntu` and that key path.
  - An alias without `HostName` uses the alias as the hostname. `Host a b` creates hosts `a` and `b`.
  - 10 hosts with `User ubuntu` and `IdentityFile ~/.ssh/aws.pem` share one identity, `ubuntu (aws.pem)`.
  - Key files are referenced by path, not copied into sshelf.

### FR-IMPORT-002 · Resolve wildcards and Include
When importing, the system shall follow `Include` directives and apply values from matching wildcard `Host` blocks using OpenSSH's first-value-wins rule.

- **Priority:** MVP · **Status:** Implemented
- **Acceptance:**
  - `Include config.d/*` is resolved relative to `~/.ssh`.
  - For the fixture set, the imported hostname, user, port and identity file equal the output of `ssh -G <alias>`.

### FR-IMPORT-003 · Import report
When an import finishes, the system shall show how many hosts and identities were created and list the aliases skipped because the label already exists and the directives not imported per host.

- **Priority:** MVP · **Status:** Draft
- **Acceptance:**
  - Importing the same file twice creates nothing the second time and reports every alias as skipped.
  - `Match`, `ProxyCommand`, `ProxyJump` and the `*Forward` directives are listed as "not imported" (`ProxyJump` and forwards are mapped in v1 by FR-IMPORT-005).

### FR-IMPORT-004 · Preview before import
When the user starts an import, the system shall show the hosts and identities that would be created and write nothing until the user confirms.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - Cancelling leaves the hosts unchanged.

### FR-IMPORT-005 · Import jumps, forwards and agent forwarding
When importing, the system shall map `ProxyJump` to a jump chain, `LocalForward`, `RemoteForward` and `DynamicForward` to auto-start forwarding rules, and `ForwardAgent yes` to agent forwarding.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - A `ProxyJump user@host:port` that matches no alias creates a new host for it.

### FR-IMPORT-006 · Export to `ssh_config`
When the user exports to OpenSSH format, the system shall write an `ssh_config` file to a path the user chooses, with one `Host` block per host and no secrets.

- **Priority:** v1 · **Status:** Draft · **Source:** assh
- **Acceptance:**
  - The export refuses to overwrite `~/.ssh/config`.
  - `ssh -G -F <exported> <alias>` reports the same hostname, user, port and jump chain as sshelf. Labels with spaces are written with spaces replaced by `-`.
  - Hosts whose key is stored in sshelf get the comment `# key stored in sshelf` and no `IdentityFile`.

### FR-IMPORT-007 · Import from CSV
When the user imports a CSV file with the columns `label,hostname,port,user,group,tags`, the system shall create the hosts it describes and report rows with errors by line number.

- **Priority:** v1 · **Status:** Draft · **Source:** Termius CSV import
- **Acceptance:**
  - A file with one valid row and one row with port `abc` creates 1 host and reports "line 3: invalid port".

### FR-IMPORT-008 · Import from other clients
When the user imports a PuTTY `.reg` export, a SecureCRT XML export or a MobaXterm `.mxtsession` file, the system shall create hosts and groups for its SSH sessions.

- **Priority:** Later · **Status:** Draft · **Source:** Termius imports
- **Acceptance:**
  - Each supported format has a fixture whose SSH sessions are all imported with hostname, port and user.

---

## 17. File transfer (`FR-SFTP`)

### FR-SFTP-001 · Transfer a single file
When the user chooses upload or download on a host and enters a local and a remote path, the system shall copy the file over SFTP using the host's saved settings and credentials.

- **Priority:** Later · **Status:** Draft · **Rationale:** system `scp` cannot use secrets stored by sshelf.
- **Acceptance:**
  - A 100 MB file has the same SHA-256 checksum on both sides. Progress is shown, and the transfer can be cancelled.

### FR-SFTP-002 · SFTP browser
When the user opens the file browser for a host, the system shall show a two-pane view of local and remote directories that supports navigating, uploading, downloading, renaming, deleting and creating directories.

- **Priority:** Later · **Status:** Draft · **Source:** Termius, MobaXterm, Midnight Commander
- **Acceptance:**
  - Each listed operation works against the in-process test server, and failures show the remote error.

---

## 18. Multiple sessions (`FR-TAB`)

sshelf has no tabs or split panes (see `research.md` §5). Without a
non-interactive CLI (D-001), there is also no simple way to open hosts in new
tmux windows (D-107).

### FR-TAB-001 · In-app session switcher
While several sessions are open in the same sshelf process, the system shall let the user switch between them with a key binding and keep each session's screen state.

- **Priority:** Later · **Status:** Draft · **Rationale:** needs an embedded terminal emulator. Re-evaluate after v1.
- **Acceptance:**
  - With 2 sessions open, switching away and back shows the same screen content in each.

---

## 19. History (`FR-HIST`)

### FR-HIST-001 · Last connection time
When an interactive session to a host is established, the system shall record the time and increment the host's connection count.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - The details pane shows "Last connected 3 h ago (42 times)". The data feeds FR-NAV-008 and FR-NAV-009.

### FR-HIST-002 · Session transcript logging
Where session logging is enabled for a host, the system shall write the session's output to a log file named with the host label and start time.

- **Priority:** Later · **Status:** Draft · **Source:** Termius, SecureCRT
- **Acceptance:**
  - Logging is off by default and per-host opt-in, with a warning that logs may contain secrets.

---

## 20. Storage and settings (`FR-CFG`)

### FR-CFG-001 · Config directory
The system shall keep its hosts, identities (without secrets), settings and `known_hosts` file in the sshelf config directory defined in ADR-005 of `../architecture/design.md`, creating it on first launch.

- **Priority:** MVP · **Status:** Draft · **Source:** D-003
- **Acceptance:**
  - sshelf starts with no existing config directory and shows the Welcome screen (FR-NAV-013).
  - The directory and files are created with the permissions set in SEC-FILE-001 (0700 directory, 0600 files).

### FR-CFG-002 · Durable writes
When the user confirms a change, the system shall write it to disk before reporting it as saved, so that a crash leaves either the old or the new file contents.

- **Priority:** MVP · **Status:** Draft · **Source:** ARCH-DATA-005, SEC-FILE-003 (atomic write)
- **Acceptance:**
  - After "Saved" is shown, a SIGKILL and restart still shows the change.
  - Killing the process during a write never leaves a truncated file.

### FR-CFG-003 · Broken config file
If a config file cannot be parsed, then the system shall show the file, line and problem, and shall not overwrite that file until the user fixes it.

- **Priority:** MVP · **Status:** Draft
- **Acceptance:**
  - A syntax error in `config.json` shows an error screen with the path and line number, and `q` quits. The file is byte-for-byte unchanged afterwards.
  - An unknown setting key produces a warning, and sshelf starts with the default for that setting.

### FR-CFG-004 · External change guard
If a config file changed on disk since this process last read or wrote it, then the system shall refuse to overwrite it and offer to reload it.

- **Priority:** v1 · **Status:** Draft · **Rationale:** two instances, or a config directory synced with Git or Syncthing.
- **Acceptance:**
  - After an external edit to `config.json`, a save in the UI offers reload and leaves the file byte-for-byte unchanged.

### FR-CFG-005 · Respect `NO_COLOR`
Where the `NO_COLOR` environment variable is set to a non-empty value, the system shall render without colour.

- **Priority:** v1 · **Status:** Draft · **Source:** no-color.org
- **Acceptance:**
  - With `NO_COLOR=1`, rendered output contains no colour escape sequences. `NO_COLOR=` (empty) has no effect.

### FR-CFG-006 · Themes
The system shall provide built-in `dark`, `light` and `high-contrast` themes and an `auto` setting that picks dark or light from the terminal background.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - Themes affect only sshelf's UI, never the remote session. `auto` falls back to `dark`.

### FR-CFG-007 · Custom key bindings
Where the settings override key bindings, the system shall use them and warn at startup about any binding that conflicts with another in the same view.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - A setting that maps `x` to connect makes `x` connect. Mapping two actions in one view to the same key prints a startup warning naming both.

### FR-CFG-008 · Global defaults
The system shall let the user set global defaults for username, port, connect timeout, keepalive interval, snippet concurrency and `show_recent`.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - The built-in values are: `$USER`, 22, 10 s, 30 s, 5 and true. The MVP uses them without override.

### FR-CFG-009 · Settings view
When the user opens the settings view, the system shall show every setting with its current value and source and save edits to `config.json`.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - Changing the theme in the settings view writes it to `config.json` and applies it immediately.

### FR-CFG-010 · Custom theme colours
Where the settings define colour overrides, the system shall apply them on top of the selected theme.

- **Priority:** Later · **Status:** Draft
- **Acceptance:**
  - An override of the accent colour changes the accent and leaves the other theme colours unchanged.

---

## 21. Command line (`FR-CLI`)

sshelf is TUI-only (D-001). The binary has no subcommands.

### FR-CLI-001 · Launch, help and version
When `sshelf` is run with `--help` or `--version`, the system shall print usage or version information and exit with status 0, and otherwise shall start the TUI.

- **Priority:** MVP · **Status:** Draft · **Source:** D-001
- **Acceptance:**
  - `--version` prints the version, commit and build date.
  - An unknown argument prints usage and exits with status 2.
  - If stdin or stdout is not a terminal, sshelf exits with status 1 and the message "sshelf needs an interactive terminal".

### FR-CLI-002 · Alternative config directory
When `sshelf` is run with `--config <dir>`, the system shall use `<dir>` as its config directory.

- **Priority:** v1 · **Status:** Draft · **Source:** D-001
- **Acceptance:**
  - Two different directories hold fully independent host lists. Secrets in the OS store are kept separate per directory, as specified in SEC-CRED-002.

---

## 22. Open questions for the product owner

None. The earlier questions are answered by D-001 to D-008 and D-101 to
D-109: encryption of host data, locking, label completion, a system-ssh
backend, ssh-agent priority, stored keys, the label grammar, a `--host` launch
flag for tmux users (D-107: not for now, revisit `Later`) and secrets on a
second machine (D-108: re-enter them; shown as "not set on this device").

