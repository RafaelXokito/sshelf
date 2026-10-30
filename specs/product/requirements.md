# sshelf functional requirements

Status: Draft · Owner: product · Last updated: 2026-10-30 (M3: D-017 to D-020)

This file is the catalogue of functional requirements (`FR-*`). It follows the
conventions in [`../README.md`](../README.md): EARS syntax, one testable
sentence per requirement, and a priority of `MVP`, `M2`, `M3`, `v1` or `Later`. The product
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
- **Simplicity.** sshelf is TUI-only and deliberately small. From milestone M2,
  sessions run inside sshelf's frame as tabs, so several hosts can be open at
  once and the host list stays one key away (D-009, D-010). From milestone M3,
  an SFTP tab copies files between the local machine and a host, on the same
  connection as a session when wanted (D-017, D-018). Split panes, file
  synchronisation and sessions that survive a restart of sshelf are left to
  tmux and other tools (D-007).

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
| Hosts (`FR-HOST`) | Add or edit, delete, validate, credentials in the host form | Duplicate, notes, favourites, reachability check, copy command, details pane |
| Identities (`FR-IDENT`) | Manage identities, link to hosts with a username override, key from a file or stored in sshelf, never show secrets | Save a typed password, key generation and deployment, reveal a password |
| Connect (`FR-CONN`) | Session, key, password or keyboard-interactive auth, prompt for missing secrets, resize, return to list, clear errors | ssh-agent, keepalive, reconnect, agent forwarding, startup command, quick connect, `~.` escape |
| Known hosts (`FR-KH`) | Unknown-key prompt (trust and save or cancel), changed-key block with removal instructions, trust `~/.ssh/known_hosts`, sshelf's own `known_hosts` file | Trust once, known-hosts view, certificate authorities |
| Navigation (`FR-NAV`) | Welcome screen, list, fuzzy search with Enter to connect, key bindings, footer hints | Highlighting, help overlay, recent, sort, filters, multi-select |
| Import (`FR-IMPORT`) | `~/.ssh/config` with `Include` and wildcard resolution; result report | Jumps and forwards on import, `ssh_config` export, CSV, other clients |
| Storage and settings (`FR-CFG`) | Config directory, durable writes, safe handling of a broken file | External-change guard, `NO_COLOR`, themes, key bindings, defaults, settings view |
| Command line (`FR-CLI`) | `--help`/`--version`, otherwise start the TUI | `--config <dir>` |
| Groups, tags, snippets, forwarding, jump hosts, SFTP, history, tabs | None | M2 (tabs), M3 (SFTP), v1 / Later |

### 3.1 Milestone M2: framed sessions, tabs and mouse

M2 follows the MVP and comes before v1 (D-009 to D-012). It replaces the
MVP's "hand the whole terminal to the session" model with sessions rendered
inside sshelf's frame by an embedded terminal emulator.

| Area | M2 | Not in M2 |
|------|----|-----------|
| Frame (D-009) | One-line header with the tab bar, the session area, one-line status bar (FR-TAB-003, FR-TAB-007); `vim`, `htop`, `less` and `tmux` render inside the frame (FR-CONN-001 revision) | Themes, configurable layout |
| Tabs (D-010) | A new tab per connection, the same host twice, switching with a prefix key, activity marker, background sessions keep running, ended tabs with reconnect and close, confirm on quit, scrollback mode (FR-TAB-001 … FR-TAB-010, FR-CONN-005 revision, FR-CONN-010, FR-CONN-014) | Split panes, detaching to the shell, sessions that survive an sshelf restart, renaming or reordering tabs, SFTP |
| Mouse on sshelf screens (D-011) | Host list, Welcome screen, forms with `[ Save ]` and `[ Cancel ]` buttons, modal buttons, tab bar (FR-NAV-012, FR-NAV-014 … FR-NAV-017) | Drag, right-click menus, hover effects |
| Mouse in sessions (D-012) | Forwarded only while the remote app asks for it (FR-TAB-011) | sshelf's own selection or copy |

M2 has 18 new or promoted functional requirements, and revised
FR-CONN-001, FR-CONN-004 and FR-CONN-005, which are now `M2` (the M2
gate, T-043, folded each revision into its requirement). The non-functional and
architecture changes are in [`../nfr.md`](../nfr.md) and
[`../architecture/design.md`](../architecture/design.md) (ADR-012 to ADR-015).

### 3.2 Milestone M3: SFTP

M3 follows M2 and comes before v1 (D-017 to D-020). It adds a second kind of
tab, the **SFTP tab**: two panes, local on the left and remote on the right,
with a transfer queue and a progress line (§17).

| Area | M3 | Not in M3 |
|------|----|-----------|
| Opening (D-017, D-018) | `s` on the host list with the normal connect flow; `Ctrl+]` `s` on a session tab reusing its connection with no second login; the connection stays open while either tab uses it (FR-SFTP-002 … FR-SFTP-004, FR-TAB-005 revision, FR-CONN-014 revision) | SFTP through jump hosts (comes with jump hosts in v1) |
| Browsing (D-017) | Starting directories, listing with sizes, item counts and times, navigation, hidden files, filter, marks, refresh, 80×24 layout, mouse (FR-SFTP-005 … FR-SFTP-009, FR-SFTP-013, FR-SFTP-021, FR-SFTP-023) | Bookmarks, a file viewer or preview, search across directories, sorting by other columns |
| File operations (D-018) | Copy files and folders both ways, rename, delete with a counted confirmation, mkdir, errors (FR-SFTP-001, FR-SFTP-010 … FR-SFTP-012, FR-SFTP-024) | Permission or owner editing (chmod, chown), creating symlinks, copying within one side, archive browsing |
| Transfers (D-018, D-020) | A per-connection queue, 2 at a time, progress line, transfers view with cancel and retry, overwrite / skip / keep both / apply to all, resume within a run, pause on a dropped connection and resume on `r`, drag to copy (FR-SFTP-014 … FR-SFTP-018, FR-SFTP-022, FR-SFTP-025) | Resume across sshelf restarts (a persistent queue), bandwidth limits, a concurrency setting |
| Edit locally (D-019) | `e` on a remote file: private temp copy, `$VISUAL`/`$EDITOR`/`vi`, upload on change, ask if the remote changed (FR-SFTP-019, FR-SFTP-020) | Editing local files, uploading on every save while the editor runs |
| Safety (D-020) | Unsafe remote names rejected, downloads never follow local symlinks, `.sshelf-part` then rename, listing limits, no contents in logs (`SEC-SFTP-*`) | |

**Out of scope for M3 (D-007).** FTP, FTPS and SCP; synchronising or
mirroring directories; remote-to-remote copy (between two hosts, or two
remote panes); a permission or ownership editor (chmod, chown, ACLs, extended
attributes; plain permission bits are copied, FR-SFTP-001); bookmarks;
following symlinks inside recursive copies; preserving owners, hard links or
directory times; a persistent transfer queue.

M3 has 25 new or promoted functional requirements (FR-SFTP-001 … FR-SFTP-025)
and revises FR-TAB-003, FR-TAB-005, FR-TAB-009 and FR-CONN-014 with **M3
revision** blocks that the M3 gate folds in. The security, non-functional and
architecture changes are in `../security/requirements.md` (`SEC-SFTP-*`, the
SEC-MEM-008 revision), [`../nfr.md`](../nfr.md) and
[`../architecture/design.md`](../architecture/design.md) (ADR-016 to ADR-020).

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

- **Priority:** MVP · **Status:** Implemented · **Source:** D-002, D-003
- **Acceptance:**
  - sshelf never shows a screen for creating, entering or changing a master password.
  - After a restart, a host whose identity has a saved password connects without a prompt.
  - Any OS-level approval (for example a keyring unlock dialog) is the only interruption the user sees. Touch ID is not used (D-008).
  - The config file contains none of the saved secret values (checked with `grep`).

### FR-CRED-002 · Credential store unavailable
If the OS credential store is unavailable, then the system shall refuse to save passwords, keys and passphrases, explain why and how to fix it, and keep working for hosts that need no stored secret.

- **Priority:** MVP · **Status:** Implemented · **Source:** D-101, SEC-CRED-005
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

- **Priority:** MVP · **Status:** Implemented · **Source:** Termius parity
- **Acceptance:**
  - MVP fields are label, hostname (DNS name, IPv4 or IPv6), port (default 22), username (optional) and either an identity (optional) or credentials entered directly (FR-HOST-010). v1 adds group, tags, notes, jump hosts, forwarding rules and startup command.
  - Changing the port from 22 to 2222 shows `:2222` in the list, and the next connection uses port 2222.
  - Esc discards the form. The saved host is still present after a restart.

### FR-HOST-002 · Delete a host
When the user confirms deletion of a host, the system shall remove the host.

- **Priority:** MVP · **Status:** Implemented
- **Acceptance:**
  - The confirmation names the host and defaults to "No".
  - A linked shared identity is not deleted. A hidden per-host identity (FR-HOST-010) is deleted with its stored secrets.
  - From v1, deleting a host used as a jump host is blocked (FR-JUMP-003).

### FR-HOST-003 · Field validation
If the label is empty, longer than 64 characters or equal (case-insensitive) to another host's label, or the hostname is empty or contains whitespace, or the port is outside 1 to 65535, then the system shall reject the form and show the error next to the offending field.

- **Priority:** MVP · **Status:** Implemented · **Source:** D-105 (labels may contain spaces)
- **Acceptance:**
  - Label `Web Prod EU` is accepted. With `web-prod` saved, `Web-Prod` is rejected with "Label already in use".
  - Ports `0`, `65536` and `abc` are rejected. `65535` is accepted.
  - Hostname `my host` is rejected. `2001:db8::1` is accepted.

### FR-HOST-004 · Duplicate a host
When the user presses `c` on a host in the host list, the system shall open the host form pre-filled with that host's fields and credentials and labelled `<label> Copy`, or `<label> Copy <n>` with the smallest `n` from 2 that no host's label equals case-insensitively, and shall create nothing until the form is saved.

- **Priority:** v1 · **Status:** Implemented · **Source:** D-022
- **Acceptance:**
  - Duplicating `web-prod` opens "Duplicate host web-prod" with the hostname, port, username and identity choice copied and the label `web-prod Copy`. With `WEB-PROD copy` saved, the label is `web-prod Copy 2`; with that saved too, `web-prod Copy 3`.
  - A host with its own credentials (FR-HOST-010) shows "(none)" as the identity and its key file path, and each secret field shows only "set" or "not set" (FR-IDENT-004). No secret is read to open the form.
  - Esc, Ctrl+C or `[ Cancel ]` closes the form and creates nothing: `config.json` and the credential store are unchanged, and the store is not called.
  - The saved copy is a new host with a new ID that keeps every other field of the source (group, tags, notes, jump hosts, forwards, options, favourite) but has no `last_connected_at` and no import source. The source host is unchanged.
  - The label is not shortened: a copy label over 64 characters gets the FR-HOST-003 error on Save.
  - The host list footer shows `c duplicate`, and `c` is in the host list's keymap (FR-NAV-003 v1 revision).

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

- **Priority:** MVP · **Status:** Implemented · **Source:** ARCH `Identity.Hidden`
- **Acceptance:**
  - A first-time user adds a host with a username and password and connects without opening the identities view.
  - The hidden identity is not listed in the identities view. Its secrets are stored as in FR-CRED-001.
  - Editing the host's credentials updates the hidden identity. Choosing a shared identity instead deletes the hidden one and its secrets.

### FR-HOST-011 · Duplicated credentials
When the user saves a duplicate (FR-HOST-004) of a host that has its own credentials (FR-HOST-010), the system shall give the new host its own hidden identity and copy each of the source's stored secrets that the form left unchanged to a new credential-store item under the new identity, reading it with `Get` and writing it with `Set`, without displaying it.

- **Priority:** v1 · **Status:** Implemented · **Source:** D-022
- **Acceptance:**
  - The new hidden identity has a new ID and the source's username, key file path and `has_*` flags. The source's items are untouched, and the copies are independent: editing or deleting either host leaves the other's secrets as they were.
  - A secret typed in the form replaces the copied value, and a secret cleared in the form is not copied.
  - A host linked to a shared identity is duplicated with a link to the same identity: no identity is created and the store is not called. Choosing a shared identity in the duplicate's form copies nothing either.
  - If a `Get` fails (access denied, no store, or the secret is not on this device, D-108), the save fails with a message naming the field, such as "could not copy the password: Access to Keychain was denied" (with no store at all, the D-101 explanation of FR-CRED-002). A failed `Set` or config write fails the save as for typed credentials (FR-HOST-010). In every case the items already written are deleted, `config.json` is not written, and the form stays open with the error under it.
  - No secret value appears on screen, in `config.json` or in the log.

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

- **Priority:** MVP · **Status:** Implemented · **Source:** Termius identities
- **Acceptance:**
  - The fields are name (unique, case-insensitive), username, password (optional), key (optional) and key passphrase (optional). Secret fields do not echo.
  - Changing the password of an identity linked to 3 hosts lets all 3 connect with the new password, with no host edits.
  - Deleting asks for confirmation, states the number of linked hosts, unlinks them, and removes the identity's secrets from the OS credential store.

### FR-IDENT-002 · Link a host to an identity
When a host is linked to an identity, the system shall authenticate that host with the identity's credentials, using the host's own username when the host sets one and the identity's username otherwise.

- **Priority:** MVP · **Status:** Implemented · **Rationale:** avoids Royal TSX's need for duplicate credentials per username.
- **Acceptance:**
  - Identity `ops` (user `deploy`, key K) on host A, which has no username, connects as `deploy` with K. Host B, linked to `ops` with username `root`, connects as `root` with K.

### FR-IDENT-003 · Key from a file or stored in sshelf
Where an identity has a private key, the system shall take it either from a referenced file path read at connection time or from key contents that the user pasted or imported into sshelf and that are stored in the OS credential store.

- **Priority:** MVP · **Status:** Implemented · **Source:** D-004, Termius keychain
- **Acceptance:**
  - A path starting with `~` is expanded. A missing file fails with "key file not found: <path>".
  - After a key file is imported into sshelf, deleting the original file does not stop connections.
  - Ed25519, ECDSA and RSA keys are accepted in OpenSSH, PEM and PKCS#8 formats. Pasting an invalid key is rejected on save.
  - The identity shows the key type and SHA256 fingerprint.
  - A pasted or imported key larger than 2800 bytes (for example RSA-4096) is rejected with a hint to reference the key file instead (D-008, SEC-CRED-003).

### FR-IDENT-004 · Never show secrets
The system shall never display a stored password, passphrase or private key in any view, log or error message.

- **Priority:** MVP · **Status:** Implemented · **Source:** SEC-LOG-005
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

sshelf opens SSH sessions with its own SSH client. In the MVP it hands the
whole terminal to the session while it runs, and the TUI resumes when the
session ends. From M2 (D-009) every session runs in its own tab inside
sshelf's frame: the remote output goes into an embedded terminal emulator,
and sshelf draws that emulator's screen between its header and status bar
(ADR-012). The flow is described in `ARCH-*`. There is no fallback to the
system `ssh` binary (D-104). sshelf never closes open sessions for security
reasons (D-006).

### FR-CONN-001 · Open an interactive session
When the user presses Enter on a host, the system shall open an interactive SSH shell session to that host in a new tab and render the remote screen inside sshelf's frame through its terminal emulator.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-009, D-010, ADR-012 (the MVP gave the whole terminal to the session until it ended)
- **Acceptance:**
  - Replaying the recorded output of `vim`, `htop` and `less` (ARCH-TEST-002) into a session produces the expected screen golden, including colours, the alternate screen and the cursor position.
  - The remote PTY's `TERM` is `xterm-256color`, the terminal sshelf's emulator implements, whatever the local `TERM` is; a host's own `Term` option still wins. Its size is the session area: the terminal width by the terminal height minus 2 rows.
  - `vim`, `htop`, `less` and `tmux` against a real `sshd` render and accept input correctly inside the frame (manual, M2 section of [`acceptance.md`](../acceptance.md)).
  - No byte of remote output is written to the local terminal except through sshelf's renderer (SEC-SESSION-005).
- **Note:** the real-`sshd` criterion needs a person at a real terminal (the test server has only a scripted shell); it is a manual step in [`acceptance.md`](../acceptance.md) and the release checklist. The replay goldens, `TERM`, PTY size and tab are automated (`TestFR_CONN_001_*`, `TestARCH_TEST_002_*`).

### FR-CONN-002 · Authenticate with stored credentials
Where the effective identity has a private key or a password, the system shall authenticate with it without prompting, trying the key (stored or file) first and then password or keyboard-interactive (FR-CONN-008).

- **Priority:** MVP · **Status:** Implemented
- **Acceptance:**
  - A host accepting only the identity's key connects without a prompt when the key is unencrypted or its passphrase is stored.
  - Given both a key and a password, against a server accepting both, the key is used (visible in the debug log).
  - Methods the server does not offer are skipped.

### FR-CONN-003 · Prompt for missing secrets
If the server requests a password, or a key passphrase is needed, and none is stored, then the system shall prompt for it with a non-echoing field and continue authentication.

- **Priority:** MVP · **Status:** Implemented
- **Acceptance:**
  - The prompt shows `user@host` and what is needed ("password" or "passphrase for <key>").
  - Esc cancels and returns to the list. 3 failed attempts end with "authentication failed".

### FR-CONN-004 · Window resize
When the local terminal is resized, the system shall resize every open session's emulator to the new session area and send each session's remote PTY the new size.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-009, D-010
- **Acceptance:**
  - With 3 sessions open (one focused, two in the background), resizing from 120×40 to 100×30 makes `size` on each sshtest shell print `100 28`.
  - A burst of resizes within 50 ms sends each session at most 2 `window-change` requests (ARCH-SESS-004).
  - After a resize, `tput cols; tput lines` on a real host prints the session area, not the full terminal size (manual).

### FR-CONN-005 · Return to the list on exit
When a session ends, the system shall keep its tab open with the last remote screen, show the remote exit status or the disconnect reason in that tab's status bar together with `r` reconnect and `x` close, and, when the user presses `x`, close the tab.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-010 (the MVP returned to the host list)
- **Acceptance:**
  - `exit 3` shows `✕ exited (3) · r reconnect · x close` in that tab's status bar; a server shutdown shows `✕ connection lost: <reason> · r reconnect · x close`. The last remote screen stays visible above it. The reason is sanitised, on one line and at most 80 columns (SEC-SESSION-006).
  - A session that ends in a background tab marks that tab `✕` (FR-TAB-004) and does not take the focus.
  - `x` closes the tab and focuses the tab to its left; when that is the Hosts tab, the session's host is selected in the host list.
  - While the tab shows an ended session, keys other than `r`, `x` and the prefix key (FR-TAB-005) are ignored. Scrollback mode still works.
  - Nothing is written to the local terminal when a session ends: the frame is simply redrawn (ARCH-SESS-005).

### FR-CONN-006 · Connection failure reporting
If a connection cannot be established within the connect timeout, then the system shall abort it and report one of: name resolution failed, connection refused, timed out, host key rejected, or authentication failed.

- **Priority:** MVP · **Status:** Implemented
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

- **Priority:** MVP · **Status:** Implemented · **Rationale:** many servers accept passwords only through keyboard-interactive; OTP and 2FA bastions. **Source:** SEC-SESSION-002, ARCH-CONN-002
- **Acceptance:**
  - A server that accepts passwords only through keyboard-interactive connects with the stored password and no prompt.
  - A test server with a password prompt and an OTP prompt fills the password and shows only the OTP prompt. The OTP field echoes only when the server sets echo.
  - With no stored password, the password prompt is shown to the user as in FR-CONN-003.

### FR-CONN-009 · Keepalive
While a session is active, the system shall send a keepalive request at the effective keepalive interval and treat the connection as lost after 3 consecutive unanswered requests.

- **Priority:** v1 · **Status:** Draft · **Source:** OpenSSH `ServerAliveInterval`
- **Acceptance:**
  - The default interval is 30 s. `0` disables keepalives.

### FR-CONN-010 · Reconnect from an ended tab
While a tab shows an ended session, when the user presses `r`, the system shall connect to the same host again and run the new session in the same tab.

- **Priority:** M2 · **Status:** Implemented · **Source:** Tabby, Termius, D-010 (was v1 "Reconnect after a drop")
- **Acceptance:**
  - `r` is offered after a drop and after a normal `exit` alike (FR-CONN-005 revision). The host is connected exactly as from the host list, with the same modals, shown over the ended tab.
  - The new session keeps the tab's position, and so its number (D-023); the old screen and scrollback are discarded once the new session is established.
  - If the reconnect fails or is cancelled, the tab keeps the ended session and its status bar shows the failure (FR-CONN-006 messages), still offering `r` and `x`.
  - sshelf never reconnects on its own (design §8.7).

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

### FR-CONN-014 · Disconnect with the prefix key
While a session tab is focused, when the user presses the prefix key followed by `q` and confirms, the system shall close that session and its tab, and shall close its SSH connection unless an SFTP tab still uses it.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-009 (replaces the v1 `~.` escape; one way to do it, D-007); M3: D-018 (the SFTP tab reuses the session's connection)
- **Acceptance:**
  - With no SFTP tab on the connection: `Ctrl+]` `q` opens "Disconnect web-prod?" defaulting to No; `y` closes the session channel, the SSH connection and the tab, and focuses the tab to its left; `n`, Enter and Esc keep the session.
  - With an SFTP tab on the same connection: the question is "Close this web-prod session? SFTP tab 3 stays connected."; `y` closes the session channel and the tab only, and the SFTP tab keeps listing and transferring (sshtest records the session channel close and no connection close).
  - sshtest records the channel close within 1 s.
  - Typing `~.` at the start of a line is sent to the remote like any other text.

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

- **Priority:** MVP · **Status:** Implemented
- **Acceptance:**
  - The fingerprint format matches `ssh-keygen -lf`.
  - Enter and Esc both mean "Cancel", which aborts the connection. "Trust and save" connects and suppresses the prompt next time.
  - "Trust once" is not offered in the MVP (FR-KH-007).
  - The test server receives no authentication request before the choice.

### FR-KH-002 · Changed host key
If a server presents a host key that differs from a trusted key of the same type for that host and port, then the system shall abort the connection before authentication and show both fingerprints with a possible-attack warning.

- **Priority:** MVP · **Status:** Implemented · **Source:** SEC-HOSTKEY-005
- **Acceptance:**
  - No "connect anyway" option is offered.
  - The block screen shows the path and line number of the stale entry (in sshelf's `known_hosts`, or in `~/.ssh/known_hosts` when the old key is there) and tells the user how to remove it, as OpenSSH does (for example "delete line 12 of <path>" or `ssh-keygen -R <host> -f <path>`).
  - An in-app remove action is v1 (FR-KH-005).

### FR-KH-003 · Trust OpenSSH known hosts
The system shall treat host keys in `~/.ssh/known_hosts` as trusted, including hashed entries, `[host]:port` entries and comma-separated host patterns, and shall never modify that file.

- **Priority:** MVP · **Status:** Implemented · **Source:** D-102
- **Acceptance:**
  - A host present in `~/.ssh/known_hosts` under a hashed name connects with no prompt.
  - `~/.ssh/known_hosts` has the same checksum before and after any sshelf action.
  - A `@revoked` entry causes rejection with "host key revoked", shown in the status bar with the file and line of the `@revoked` entry and no trust action. Implemented as a status line rather than a block screen, because there is nothing to compare and nothing to offer (design §9.1).
  - Like OpenSSH, a line that cannot be parsed (e.g. two entries joined by a missing newline) is skipped and logged as a warning with its file and line; every other line still trusts, blocks and reports its original file and line. A file with more than 100 unparseable lines is rejected. Found with a real `~/.ssh/known_hosts` during acceptance testing.

### FR-KH-004 · sshelf known_hosts file
When the user chooses "trust and save", the system shall append the key to sshelf's own `known_hosts` file in its config directory.

- **Priority:** MVP · **Status:** Implemented · **Source:** D-102
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

- **Priority:** MVP · **Status:** Implemented
- **Acceptance:**
  - Port 22 is omitted from the display.
  - Lines wider than the terminal are truncated with `…`.
  - With no hosts, the Welcome screen (FR-NAV-013) is shown instead of the list.
- **v1 revision** · Priority: v1 · Status: Implemented · Source: D-016
  - *Revised:* The system shall display each host as one line showing the label and `user@hostname:port`, in the order of FR-NAV-018.
  - *Acceptance:* the criteria above, with the order, the relative time and the narrow-terminal rule of FR-NAV-018.

### FR-NAV-002 · Fuzzy search
When the user types in the search field, the system shall filter the host list to hosts whose label, hostname or username fuzzy-matches the query, ordered by match score, and Enter shall connect to the selected result.

- **Priority:** MVP · **Status:** Implemented · **Source:** sshs, lazyssh
- **Acceptance:**
  - `/` focuses search. `wbprd` matches `web-prod`, and `10.0.1` matches `10.0.1.20`. Matching is case-insensitive.
  - Typing `wbprd` and pressing Enter connects to `web-prod` when it is the top match.
  - Esc clears the query and returns focus to the list.
  - v1 adds group, tag and note matching.

### FR-NAV-003 · Key bindings
The system shall provide these bindings in the host list: ↑/↓ or `k`/`j` to move, `g`/`G` for first/last, `/` to search, `Enter` to connect, `a` to add, `e` to edit, `d` to delete, `i` for identities, `I` to import, and `q` or `Ctrl+C` to quit.

- **Priority:** MVP · **Status:** Implemented
- **Acceptance:**
  - Each binding is covered by a UI test.
  - While the search field is focused, letter keys type into the query.
  - Ctrl+C inside a form cancels the form and does not quit.
  - In any other view, Esc goes back to the previous view. `q` quits only from the host list.
- **v1 revision** · Priority: v1 · Status: Implemented · Source: D-022
  - *Revised:* The system shall provide these bindings in the host list: ↑/↓ or `k`/`j` to move, `g`/`G` for first/last, `/` to search, `Enter` to connect, `a` to add, `c` to duplicate (FR-HOST-004), `e` to edit, `d` to delete, `i` for identities, `I` to import, and `q` or `Ctrl+C` to quit.
  - *Acceptance:* the criteria above. The footer shows `c duplicate`; to fit 80 columns it no longer shows `I import`, which stays bound and is listed in the keymap and the README.

### FR-NAV-004 · Key hints footer
The system shall show a one-line footer with the main key bindings of the current view.

- **Priority:** MVP · **Status:** Implemented
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

### FR-NAV-009 · Sort order
When the user cycles the sort mode, the system shall order hosts within each group by label, by last connection time, or by hostname, and remember the mode.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - Each sort mode orders a fixture list as expected, and the chosen mode is still active after a restart.
  - The default mode is last use (FR-NAV-018, D-016).

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

### FR-NAV-012 · Mouse on the host list
While the host list is shown, the system shall select a host on a left click, connect to it on a double-click, move the selection with the wheel, and focus the search field on a click in it.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-011 (was `Later` behind a setting; M2 has no setting)
- **Acceptance:**
  - A click on a host row selects it; a click on a blank row below the last host does nothing.
  - A double-click is two left presses on the same row within 400 ms (ARCH-UI-007) and connects exactly like Enter. Two presses on different rows, or more than 400 ms apart, only select.
  - Each wheel notch moves the selection 3 rows, clamped to the first and last host, scrolling the list as needed.
  - A click on the search field focuses it as `/` does. A click on a host row while search is focused moves focus back to the list and selects that host, keeping the query.
  - Tests send synthetic `tea.MouseClickMsg` and `tea.MouseWheelMsg` through the root model, with coordinates that include the header row.

### FR-NAV-013 · Welcome screen
While `config.json` contains no hosts, the system shall show a Welcome screen offering "Import from ~/.ssh/config" and "Add host" instead of the host list.

- **Priority:** MVP · **Status:** Implemented
- **Acceptance:**
  - The first launch with no config directory shows the Welcome screen.
  - `I` starts the import (FR-IMPORT-001) and `a` opens the host form. `q` quits.
  - After the first host is saved or imported, the host list is shown. Deleting the last host shows the Welcome screen again.

### FR-NAV-014 · Mouse on the Welcome screen
When the user clicks one of the Welcome screen's choices, the system shall act as if that choice's key had been pressed.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-011
- **Acceptance:**
  - A click anywhere on the `[I] Import from ~/.ssh/config` line starts the import; a click on the `[a] Add host` line opens the host form. Clicks elsewhere do nothing.

### FR-NAV-015 · Buttons and mouse in forms
The system shall show `[ Save ]` and `[ Cancel ]` buttons after every form's fields, and, when the user clicks a field, a choice arrow or a button, shall focus that field, change that choice, or act as `ctrl+s` or `esc`.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-011
- **Acceptance:**
  - The host form and the identity form show both buttons on one row after the last field, at 80×24 and 120×40 (goldens).
  - The buttons follow the last field in the Tab order; Enter or Space on a focused button presses it, so the buttons are keyboard-reachable too (NFR-UX-001).
  - A click on a field's label or value focuses that field, with the text cursor at the end of its value.
  - A click on `‹` or `›` of a choice field selects the previous or next option; a click elsewhere on it only focuses it.
  - A click on `[ Save ]` validates and saves exactly like `ctrl+s`, with errors next to the fields; a click on `[ Cancel ]` behaves exactly like `esc`, including the unsaved-changes confirmation.
  - A read-only field (no credential store, FR-CRED-002) can be focused by a click but not changed.

### FR-NAV-016 · Mouse in modals
When the user clicks a button shown in the top modal, the system shall act as if that button's key had been pressed.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-011
- **Acceptance:**
  - Every modal shows its actions as bracketed buttons, for example `[y] Trust and save` and `[enter/esc] Cancel` in the TOFU modal and `[y] Yes` and `[n] No` in confirmations, and each is clickable. The prompt modal's fields are clickable as in FR-NAV-015.
  - A click outside the top modal does nothing: it neither closes the modal nor reaches the screen below.
  - In the TOFU modal, only `y` or a click on `[y] Trust and save` trusts a key (SEC-HOSTKEY-004).

### FR-NAV-017 · Mouse on the tab bar
While the Hosts tab is focused and no modal is open, when the user clicks a tab in the tab bar, the system shall focus that tab.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-011, D-012
- **Acceptance:**
  - A click on `2 web-prod` focuses that session tab; a click on `sshelf` or a separator does nothing.
  - While a session tab is focused, tabs are switched with the keyboard (FR-TAB-005, D-012): header clicks are either not reported (mouse tracking off) or ignored (FR-TAB-011).

### FR-NAV-018 · Hosts ordered by last use
The system shall order the host list by the time each host's last session was established, most recent first, followed by the hosts never connected in A–Z order by label, and show that time on each row as a dim relative time.

- **Priority:** v1 · **Status:** Implemented · **Source:** D-016
- **Acceptance:**
  - A session counts once it is established: authenticated and its shell (or remote command) opened (ARCH-SESS-002). sshelf then sets the host's `last_connected_at` to the current time (RFC 3339, UTC, whole seconds) and writes `config.json` through the atomic save (SEC-FILE-003). Reconnecting an ended tab (FR-CONN-010) counts.
  - A failed dial, a cancelled TOFU prompt (FR-KH-001), an authentication failure or cancel, a session that could not be opened and a cancelled attempt leave `last_connected_at` and `config.json` unchanged.
  - If that write fails, the session still opens; the time is kept in memory and written with the next save.
  - Order: `last_connected_at` newest first; hosts without it come after, A–Z by label, case-insensitively; equal times are A–Z by label. The list is re-sorted by the time the user returns to the Hosts tab, and the selection stays on the same host when the order changes under it (as FR-NAV-002 and FR-CONN-005 keep it).
  - Search (FR-NAV-002) orders by match score; for equal scores the more recently used host comes first, then the list order.
  - Each row ends with the time, right-aligned and dim: `just now` (under a minute, or in the future), `5m ago` (under an hour), `2h ago` (under a day), `3d ago` (under 30 days), then the local date, `Oct 12` in the current year and `Oct 2025` before it. A never-connected host shows nothing there.
  - The time column is hidden while the list is narrower than 60 columns; rows are truncated as in FR-NAV-001. Goldens at 80×24 and 120×40.
  - `last_connected_at` is optional (credential-storage §3.3): a `config.json` without it loads with every host never connected, the field round-trips, and the schema version stays 1. A value that is not an RFC 3339 time is ignored with a one-line warning, like an unknown setting (FR-CFG-003).
  - Imported hosts (FR-IMPORT-001) have no time. Editing a host (FR-HOST-001) keeps its time.

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

- **Priority:** MVP · **Status:** Implemented · **Source:** Termius, sshs, lazyssh
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

- **Priority:** MVP · **Status:** Implemented
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

Milestone M3 (D-017 to D-020). An **SFTP tab** is a tab next to the session
tabs with two panes, the local file system on the left and the remote host's
on the right, a one-line progress line and the key-hints footer. The layout
the product owner approved:

```
sshelf │ Hosts │ 1 raspberrypi4 │[2 sftp: raspberrypi4]│
┌ Local: ~/Downloads ────────┐┌ Remote: /home/pi ──────────┐
│ ..                         ││ ..                         │
│ ▸ photos/          4 items ││ ▸ scripts/         12 items│
│   report.pdf        1.2 MB ││ > backup.tar.gz    840 MB │
│   notes.txt          3 KB  ││   .bashrc            4 KB  │
└────────────────────────────┘└────────────────────────────┘
 ↓ backup.tar.gz  412/840 MB  █████░░░░░ 49%  12 MB/s
 tab switch pane · c copy · r rename · d delete · m mkdir
```

`>` marks the cursor row of the **focused pane** (the remote pane above);
`▸` marks a directory. Everything is over SFTP with `github.com/pkg/sftp`
(ADR-016); there is no FTP and no SCP. Remote names and paths are untrusted
input (D-020, `SEC-SFTP-*`). Keys in this section apply while an SFTP tab is
focused, no modal is open and the prefix key is not pending; the prefix key
works on SFTP tabs as on session tabs (FR-TAB-005).

FR-SFTP-001 and FR-SFTP-002 were `Later` placeholders ("Transfer a single
file", "SFTP browser"); M3 rewords and promotes them rather than adding
duplicates.

### FR-SFTP-001 · Copy between the panes
When the user presses `c` in a pane, the system shall copy the marked entries, or the entry under the cursor if none is marked, into the other pane's current directory, uploading from local to remote and downloading from remote to local, and copying directories recursively.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-017, D-018 (was `Later` "Transfer a single file")
- **Acceptance:**
  - A 100 MiB file uploaded and then downloaded again has the same SHA-256 as the original (sshtest SFTP subsystem, ARCH-TEST-004).
  - A directory tree with three levels, an empty directory and 200 small files is copied in both directions with the same relative paths and contents. Directories are created before their contents.
  - Each copied file keeps its modification time (to the second) and its permission bits `rwxrwxrwx` (`mode & 0777`; a download also drops the bits of sshelf's umask, as `cp` and OpenSSH's `sftp get` do, so a server cannot make a local file group- or world-writable, D-020); setuid, setgid and sticky bits are never set on the target. Directory modification times are not preserved.
  - Every copy goes through the transfer queue (FR-SFTP-014, FR-SFTP-015): `c` returns at once, and the panes stay usable while it runs. When a transfer finishes, the target pane is refreshed if it shows the target directory.
  - `c` on `..` does nothing. Symbolic links inside a copied directory are skipped and counted ("skipped 3 symlinks"); a marked symlink to a file is copied as the file it points to.
  - Special files (devices, FIFOs, sockets) are skipped with the same kind of note.

### FR-SFTP-002 · Open an SFTP tab from the host list
When the user presses `s` on a host in the host list, the system shall connect to that host through the normal connect flow and, once connected, open an SFTP tab for it after the last tab and focus it.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-017 (was `Later` "SFTP browser")
- **Acceptance:**
  - Dialling, host-key checks (TOFU, changed key), prompts and authentication happen exactly as for `enter` (FR-TAB-002, ARCH-SESS-002); no tab opens if any of them fails or is cancelled.
  - The connection opens no shell and no PTY: sshtest records one `subsystem sftp` request per SFTP channel and no `pty-req` or `shell`.
  - If the server refuses the SFTP subsystem, no tab opens, the connection is closed, and the status line says "<label>: the server does not offer SFTP".
  - The tab is labelled `<n> sftp: <label>` (FR-TAB-003). Pressing `s` twice on the same host opens two SFTP tabs with two connections.
  - `s` is listed in the host list's help and footer when there is room (FR-NAV-004).

### FR-SFTP-003 · Open SFTP from a session without logging in again
While a live session tab is focused, when the user presses `Ctrl+]` `s`, the system shall open an SFTP tab on that session's SSH connection, without dialling or authenticating again, or focus the SFTP tab that already uses that connection.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-018
- **Acceptance:**
  - sshtest records no new TCP connection and no auth attempt; it records a new `subsystem sftp` channel on the session's existing connection. No credential is read from the store (`MemStore` counts zero `Get`).
  - A second `Ctrl+]` `s` on the same session, or on another session tab of the same connection, focuses the existing SFTP tab instead of opening a second one. A session tab on a different connection to the same host gets its own SFTP tab.
  - If the server refuses the subsystem, no tab opens, the session keeps running, and the status bar shows "the server does not offer SFTP" for 5 s.

### FR-SFTP-004 · A shared connection outlives either tab
While a session tab and an SFTP tab share one SSH connection, the system shall keep that connection open until both tabs have ended or been closed.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-018
- **Acceptance:**
  - `exit` in the shell ends the session tab (`✕`, FR-CONN-005); the SFTP tab keeps listing, and a transfer that was running completes with the right checksum. sshtest records the session channel close and no connection close.
  - Closing the SFTP tab (FR-SFTP-025) leaves the session tab live and echoing input.
  - The connection is closed, once, after the last of the two closes (sshtest records exactly one connection close). `goleak` passes.
  - If the connection itself drops, both tabs end: the session tab shows its ended state and the SFTP tab pauses its transfers (FR-SFTP-018). Reconnecting either tab opens a new connection for that tab only.

### FR-SFTP-005 · Starting directories
When an SFTP tab opens, the system shall show the directory sshelf was started in on the local pane, or the user's home directory if that directory can no longer be read, and the remote user's home directory as the server reports it on the remote pane, with the remote pane focused.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-017
- **Rationale:** most people start sshelf from a shell, so the working directory is where they were about to put or pick files (as `sftp`, `scp` and Midnight Commander do). `~` jumps to the home directory in one key (FR-SFTP-007).
- **Acceptance:**
  - With sshelf started in a temp dir, the local pane shows that dir. After that dir is deleted, a new SFTP tab starts in `$HOME`.
  - The remote pane starts in the path the server's `realpath(".")` returns (sshtest started with home `/home/pi` shows `Remote: /home/pi`). If that fails, it starts in `/`.
  - Pane titles abbreviate the local home directory to `~`; a path wider than the title is cut from the left with `…` so the end stays visible. The remote path is shown as reported, sanitised (SEC-SFTP-008).

### FR-SFTP-006 · Listing columns and order
The system shall list each directory with a `..` row first (except at `/`), then directories, then all other entries, each group sorted by name, showing for every row its name, its size (files) or item count (directories), and, where the pane is at least 50 columns wide, its modification time.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-017, the approved layout
- **Acceptance:**
  - Names sort case-insensitively, ties by byte order. Directory names end with `/` and carry `▸`; symbolic links end with `@`, sort among the files, and show the size of the link entry.
  - Sizes use 1000-based units with at most one decimal below 10: `999 B`, `3 KB`, `1.2 MB`, `840 MB`, `12 GB`.
  - A directory's item count is filled in after the listing appears, for the rows on screen only, one count at a time per pane; it shows `…` until known, `1 item`, `12 items`, or `10k+ items` above 10,000. Leaving the directory cancels pending counts.
  - Modification times are local time: `Oct 30 14:02` within the last 12 months, `2025-03-01` otherwise.
  - A name wider than its column is cut with `…`. Names are displayed sanitised (SEC-SFTP-001, SEC-SFTP-008); invalid UTF-8 shows as `�` but operations use the exact bytes.
  - Goldens of both panes at 80×24 and 120×40 (the mtime column appears at 120 columns only).

### FR-SFTP-007 · Moving around
While an SFTP tab is focused, the system shall move the cursor with ↑/↓ or `k`/`j`, `pgup`/`pgdn` and `g`/`G`, switch the focused pane with `tab`, open the directory under the cursor with `enter`, go to the parent directory with `backspace` or `h`, and go to the pane's starting home directory with `~`.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-017
- **Acceptance:**
  - `enter` on a directory, on `..`, or on a symlink whose target is a directory opens it; the new path is the link's path (`/srv/www`, not its target), so `h` returns to where the user came from. `enter` on a file does nothing (the footer suggests `c` and `e`).
  - After `h`, the cursor is on the directory just left. Each pane keeps its own cursor, scroll position and path when focus moves away and back.
  - Opening a directory that cannot be read keeps the current listing and shows the error (FR-SFTP-024).
  - `~` goes to the local home directory in the local pane and to the starting remote directory (FR-SFTP-005) in the remote pane.
  - Every key has a root-model test; `?` shows these bindings in the help overlay, generated from `ui/keys` (ARCH-UI-004).

### FR-SFTP-008 · Hidden files and filter
While a pane is focused, when the user presses `.`, the system shall hide or show entries whose names start with `.` in both panes, and when the user presses `/`, the system shall filter the focused pane's current listing by the text typed, case-insensitively, until Esc clears it.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-017
- **Acceptance:**
  - Hidden entries are shown when a tab opens (the approved layout shows `.bashrc`); `.` toggles, and the pane titles show `(hidden off)` while they are hidden.
  - The filter is a substring match on the displayed name. While typing, the pane's bottom border shows `/ rep`; `enter` keeps the filter and returns keys to the list; Esc clears it. Changing directory clears it.
  - Marks on entries the filter hides are kept, and the pane's bottom border counts them ("3 marked", design §9.6).

### FR-SFTP-009 · Marking several entries
When the user presses `space` on an entry, the system shall mark or unmark it and move the cursor down one row, and file operations shall then act on every marked entry of the focused pane.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-017
- **Acceptance:**
  - A marked row shows `*` before its name (not colour alone, NFR-UX-003). `..` cannot be marked.
  - Marks belong to one pane and one directory: changing directory, or refreshing after the entry is gone, drops them.
  - `c`, `d` and drag act on the marked entries; `r`, `e` and `enter` act on the cursor row only. After a copy is queued, the marks are cleared.

### FR-SFTP-010 · Rename
When the user presses `r` on an entry and enters a new name, the system shall rename that entry within its directory.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-018
- **Acceptance:**
  - The modal "Rename report.pdf to:" opens with the current name filled in. Enter renames; Esc cancels.
  - A name that is empty, `.` or `..`, or contains `/`, NUL or a control character is refused in the modal with a reason.
  - If the new name already exists, nothing is renamed and the modal says "already exists". sshelf never overwrites by renaming.
  - The listing refreshes with the cursor on the renamed entry. A server error is shown (FR-SFTP-024).

### FR-SFTP-011 · Delete with confirmation
When the user presses `d`, the system shall ask for confirmation, stating how many entries will be deleted including the contents of directories, and delete the marked entries or the entry under the cursor only after `y`.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-018
- **Acceptance:**
  - One file: "Delete notes.txt?". A directory: sshelf first counts its contents in the background ("Counting photos/… esc cancel"), then asks "Delete photos/ and the 1,234 entries in it? This cannot be undone." Several: "Delete 3 items and the 1,234 entries in them?". Counting stops at 100,000 ("more than 100,000 entries").
  - The confirmation defaults to No (`n`, Enter and Esc keep everything).
  - Symbolic links are deleted as links; deleting never follows a link into its target, locally or remotely.
  - Deleting stops at the first error, shows it with the number already deleted, and refreshes the pane.
  - `d` on `..` does nothing.

### FR-SFTP-012 · Make a directory
When the user presses `m` and enters a name, the system shall create a directory with that name in the focused pane's current directory and put the cursor on it.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-018
- **Acceptance:**
  - The name rules of FR-SFTP-010 apply. An existing name is refused with "already exists".
  - The new directory has mode 0755 before the umask (local) or the server's default (remote).

### FR-SFTP-013 · Refresh
When the user presses `R`, the system shall list both panes' current directories again, keeping each cursor on the same name where it still exists.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-017
- **Acceptance:**
  - A file created by another process appears after `R`. sshelf does not watch directories; it refreshes only on `R` and after its own operations (rename, delete, mkdir, a finished transfer into the shown directory).

### FR-SFTP-014 · Progress line
While a transfer is running, the system shall show one line under the panes with the direction, name, bytes done and total, a 10-cell bar, the percentage and the current rate of the oldest running transfer, and the number of other running and queued transfers.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-017, the approved layout
- **Acceptance:**
  - Golden: ` ↓ backup.tar.gz  412/840 MB  █████░░░░░ 49%  12 MB/s  +1 running, 3 queued` (↓ download, ↑ upload). The percentage is always shown as text, so the bar is not the only cue (NFR-UX-003). When the line does not fit, the counts, then the bar, then the rate are dropped, and the name is cut with `…`.
  - The rate is a moving average over the last 5 s. The line is redrawn at most 4 times a second (ARCH-SFTP-003).
  - With no transfer running, the line shows the last status message (for example "copied 3 files, skipped 1") or is blank; paused transfers show `⏸ 3 transfers paused: connection lost · r reconnect` (FR-SFTP-018); a transfer waiting for an answer shows `? backup.tar.gz exists · t to answer`.

### FR-SFTP-015 · Transfers view: cancel and retry
When the user presses `t`, the system shall show the tab's transfers with, for each, its direction, name, state, bytes done and total, rate and estimated time left, and shall let the user cancel a queued, running, waiting or paused transfer and retry a failed or cancelled one.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-018
- **Acceptance:**
  - States are `queued`, `running`, `waiting` (an existing target needs an answer), `paused` (connection lost), `done`, `failed: <reason>`, `cancelled` and `skipped`, written as text (NFR-UX-003). ETA is `—` while the rate is 0.
  - Keys: ↑/↓ move, `x` cancels the selected transfer, `r` retries it from the start (or resumes it, FR-SFTP-017), `enter` on a `waiting` transfer reopens its question (FR-SFTP-016), `C` removes finished rows, `t` or Esc returns to the panes.
  - Cancelling a running download deletes its local `.sshelf-part` file; cancelling a running upload deletes its remote part file (best effort, an error is shown). Cancelling a directory transfer cancels everything still queued under it.
  - The list holds the 1,000 most recent transfers; older finished rows are dropped first.
  - Golden of the view at 80×24 and 120×40 with one transfer in each state.

### FR-SFTP-016 · Existing targets
If a file to be copied already exists in the target directory, then the system shall ask whether to overwrite it, skip it, or copy it under a new name, with an option to apply the same answer to the rest of that copy, and shall act only on the answer.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-018
- **Acceptance:**
  - The modal shows both sides' size and modification time: "backup.tar.gz already exists (remote 840 MB, Oct 29 10:12; local 812 MB, Oct 28 09:00). [o] Overwrite · [s] Skip · [n] Keep both · [a] apply to all · [esc] Skip". `a` toggles "apply to all" before the choice.
  - "Keep both" copies to `<stem> (1)<ext>`, then `(2)` and so on, using the first free name (`backup (1).tar.gz`).
  - "Apply to all" covers the remaining conflicts of the same `c` or drag only.
  - While the question is open the transfer is `waiting`; the other transfers keep running. Closing the modal with Esc skips.
  - A directory that already exists is merged without asking; only files ask. An existing entry of a different type (a file where a directory goes, or the reverse) fails that entry with "exists and is not a directory/file".
  - Overwrite replaces the target only when the new copy is complete (FR-SFTP-017, SEC-SFTP-003): until then the old file is intact.

### FR-SFTP-017 · Resume interrupted transfers
When an interrupted transfer is started again in the same sshelf run, the system shall continue it from the bytes already transferred where it is safe to do so, and otherwise start it over and say so.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-018, D-020
- **Acceptance:**
  - **Download.** It continues from the size of `<name>.sshelf-part` when the part is a regular file (not a symlink) no larger than the remote file, and the remote file's size and modification time still match what sshelf recorded when the download started.
  - **Upload.** Uploads also go to a part file, `<name>.sshelf-part` in the remote directory. Every 8 MiB sshelf records a checkpoint: the bytes the server has acknowledged and the part file's size and modification time as the server reports them. The upload continues from the last checkpoint when the remote part file exists, is a regular file, is no larger than the local file and no smaller than the checkpoint, its modification time matches the checkpoint's (or, if it is larger than the checkpoint because writes were in flight when the connection dropped, is not older), and the local file's size and modification time are unchanged since the upload started. sshelf first truncates the part file to the checkpoint (ARCH-SFTP-005).
  - Otherwise the transfer starts from 0 and the transfers view and the status line say "restarted: <reason>" (for example "remote file changed").
  - Interrupt a 64 MiB download and a 64 MiB upload after about 20 MiB (sshtest fault injection, ARCH-TEST-004), retry: each finishes with the right SHA-256, and sshtest records that at most 48 MiB were read or written the second time (the upload resumes from its last checkpoint, at 16 MiB).
  - Resume works within one sshelf run: the queue is kept in memory only. A `.sshelf-part` file left by an earlier run is never resumed; it is overwritten when that file is copied again.

### FR-SFTP-018 · Connection lost: pause and reconnect
If an SFTP tab's connection drops, then the system shall pause its running and queued transfers, keep the panes' last listings, and on `r` reconnect through the normal connect flow and resume the paused transfers.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-018
- **Acceptance:**
  - After sshtest drops the connection mid-transfer: the tab shows `✕`, the progress line shows `⏸ N transfers paused: connection lost · r reconnect`, and operations other than browsing the local pane, `t`, `r` and `x` are refused with "not connected".
  - `r` opens the Connecting flow (modals over the tab); on success the tab uses the new connection, both panes refresh, and paused transfers resume (FR-SFTP-017) in their original order. A failed or cancelled reconnect leaves the tab disconnected with the error shown.
  - `x` closes the disconnected tab after confirming when transfers are paused ("Close and drop 3 paused transfers?").
  - sshelf never reconnects by itself (D-007, design §8.7).

### FR-SFTP-019 · Edit a remote file locally
When the user presses `e` on a remote file, the system shall download it to a private temporary copy, suspend its own display, run the user's editor on that copy, and when the editor exits, upload the copy back if it changed, then delete the copy.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-018, D-019
- **Acceptance:**
  - The editor is the first non-empty of `$VISUAL`, `$EDITOR` and `vi`. A value with spaces is split into words (`code -w`); there is no shell. The copy's path is the last argument.
  - The copy keeps the remote file's name, in a new directory with mode 0700, with mode 0600 (SEC-SFTP-005).
  - If the file changed (its SHA-256 differs) and the remote has not changed (FR-SFTP-020), the copy is uploaded through a part file and renamed over the remote file, which keeps its permission bits; the status line says "saved notes.txt (3 KB)". If it did not change: "notes.txt: no changes".
  - If the editor exits with a non-zero status, nothing is uploaded: "editor exited with status 1; nothing uploaded".
  - If the upload fails, a modal offers `[r] Retry`, `[l] Save to <local pane dir>/<name>.local-edit` and `[x] Discard`; the copy is kept until one is chosen, then deleted.
  - The temporary copy and its directory are deleted in every case, including an editor that cannot be started ("editor not found: nano"). sshelf's crash handler deletes them too.
  - Sessions in other tabs keep reading output while the editor runs, and transfers keep running (ARCH-SFTP-006). When the editor exits, every tab is redrawn at the current terminal size.
  - `e` on a directory, on `..`, on a local entry, or on a file over 64 MiB does nothing but explain why in the status line. `e` on a symlink edits the file it points to (resolved with the server's `realpath`).
  - PTY E2E with a fake `$EDITOR` script that appends a line: the remote file holds the new line and no temp file remains.

### FR-SFTP-020 · Remote file changed while editing
If the remote file's size or modification time differs, when the editor exits, from what sshelf recorded at the download, then the system shall ask whether to overwrite the remote file, keep the remote file and discard the edit, or save the edit as `<name>.local-edit` next to the remote file.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-019
- **Acceptance:**
  - The modal "notes.txt changed on the server while you edited it. [o] Overwrite · [k] Keep remote · [s] Save mine as notes.txt.local-edit" defaults to nothing: Esc is the same as Keep remote, and Enter does nothing.
  - "Save mine" uploads to `notes.txt.local-edit` (or `notes.txt.local-edit (1)` if that exists) in the same remote directory.
  - The temporary copy is deleted after any answer.
  - If the remote file was deleted meanwhile, the question is the same with "was deleted on the server"; Overwrite recreates it.

### FR-SFTP-021 · Mouse in the SFTP tab
While an SFTP tab is focused and no modal is open, the system shall focus the pane and select the row under a left click, act as `enter` on a double-click, scroll a pane with the wheel, and focus the tab clicked in the tab bar.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-011, D-017
- **Acceptance:**
  - Mouse tracking is on (cell motion) while an SFTP tab is focused (ARCH-UI-006); the terminal's own text selection is not available there, as on the Hosts tab.
  - A click on a row focuses its pane and moves the cursor there; a click on a pane's border focuses that pane only. A double-click (ARCH-UI-007) on a directory opens it, and on a file does nothing.
  - Each wheel notch scrolls the pane under the pointer 3 rows, without changing focus.
  - A click on a tab in the tab bar focuses that tab, as on the Hosts tab (FR-NAV-017). A click on the progress line opens the transfers view (as `t`); a click on the footer does nothing.
  - Tests send synthetic `tea.MouseClickMsg` and `tea.MouseWheelMsg` through the root model with coordinates that include the header row.

### FR-SFTP-022 · Drag to copy
When the user presses the left button on an entry in one pane and releases it over the other pane, the system shall copy the marked entries, or the pressed entry if it is not marked, into the other pane's current directory, as `c` does.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-017
- **Acceptance:**
  - While the button is held after moving off the pressed row, the progress line shows the hint `drag: copy 3 items to Remote: /home/pi · release over the other pane`, and the pane under the pointer has its border highlighted.
  - Releasing over the same pane, the header, the progress line or the footer copies nothing and clears the hint. Releasing on a directory row of the other pane still copies into that pane's current directory (no drop into subdirectories).
  - A press and release on the same row is a click (FR-SFTP-021), not a drag.
  - Dragging `..` does nothing. Every drag has a key equivalent (`c`), so NFR-UX-001 holds.
  - Tests drive `tea.MouseClickMsg`, `tea.MouseMotionMsg` and `tea.MouseReleaseMsg` through the root model and check the hint golden and the queued transfers.

### FR-SFTP-023 · Layout at 80×24
While the terminal is at least 80 columns by 24 rows, the system shall show both panes side by side, each half the width, with at least 18 rows of entries, the progress line and the footer.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-017, NFR-UX-002
- **Rationale:** side by side keeps local on the left and remote on the right (D-017) and keeps dragging horizontal. At 80 columns each pane has 38 columns inside its border, enough for a 24-column name and the size; the mtime column appears from 50 inner columns (a terminal of 104 or more). Stacking the panes would halve the rows for little gain.
- **Acceptance:**
  - Goldens of the approved layout at 80×24 and 120×40, checked by the NFR-UX-002 helper (no line wider than the terminal; the last line is the footer).
  - With an odd width the extra column goes to the remote pane.

### FR-SFTP-024 · Errors and warnings
If an SFTP operation fails or skips entries, then the system shall say so on the progress line with the entry's name and the server's or the OS's reason, without closing the tab.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-018
- **Acceptance:**
  - Permission denied, no such file, disk full (local `ENOSPC`) and a server failure status each produce one line such as `✕ cannot open /root: permission denied`, with the `✕` prefix (NFR-UX-003). Server text is sanitised and cut (SEC-SFTP-008).
  - Entries skipped for unsafe names (SEC-SFTP-001) or listing limits (SEC-SFTP-004) are counted in one warning per listing: `⚠ 2 entries with unsafe names not shown`.
  - The message stays until the next operation or 10 s, whichever is later.

### FR-SFTP-025 · Close an SFTP tab
When the user presses `Ctrl+]` `q` on an SFTP tab and confirms, or `x` on a disconnected SFTP tab, the system shall cancel the tab's transfers, close its SFTP channels, release its connection, and close the tab.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-018
- **Acceptance:**
  - The question is "Close SFTP tab 2?", with "3 transfers will stop." when any are queued, running, waiting or paused; it defaults to No.
  - Cancelled transfers remove their part files as in FR-SFTP-015.
  - The connection closes only if no session tab still uses it (FR-SFTP-004). Focus moves to the tab on the left.

---

## 18. Multiple sessions (`FR-TAB`)

From M2, every session runs in its own tab inside sshelf's frame (D-009,
D-010). The frame is one header line holding the tab bar, the session area,
and one status line. The **Hosts tab** is always the first tab and holds every
sshelf screen (host list, Welcome, forms, identities, import). Session tabs
are numbered from 1 in the order they were opened. The **prefix key**,
`Ctrl+]`, followed by one more key, drives the tabs; every other key goes to
the focused session. There are no split panes, no detaching to the shell and
no sessions that survive an sshelf restart (D-007).

The header looks like this (tab 2 focused, tab 1 has new output):

```
sshelf │ Hosts │ 1 raspberrypi4 ● │[2 web-prod]│
```

### FR-TAB-001 · Switch between tabs
While several tabs are open, when the user presses the prefix key followed by a tab number, `n`, `p`, `h` or `0`, the system shall focus that session tab, the next tab, the previous tab or the Hosts tab, and show the focused session's current screen.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-010 (was `Later` "In-app session switcher"); v1: D-023
- **Acceptance:**
  - With 2 sessions open, switching away and back shows the same screen content in each, plus any output that arrived while the tab was in the background.
  - `Ctrl+]` `1` to `9` focus the 1st to 9th tab from the left, session or SFTP (D-023); a number with no tab does nothing. `Ctrl+]` `h` and `Ctrl+]` `0` focus the Hosts tab while the sessions keep running.
  - `Ctrl+]` `n` and `Ctrl+]` `p` cycle through all tabs, the Hosts tab included, wrapping at both ends.
  - With 10 sessions open, the first frame of the newly focused tab is rendered within 50 ms (NFR-PERF-006).

### FR-TAB-002 · Open a session in a new tab
When a connection started from the host list succeeds, the system shall open a new session tab after the last tab, number it by its position, and focus it.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-010; v1: D-023
- **Acceptance:**
  - Dialling, host-key checks, prompts and authentication still happen over the Hosts tab (Connecting, TOFU and prompt modals); the tab opens only when the session is established (ARCH-SESS-002 revision). A failed or cancelled connection opens no tab.
  - Connecting to `web-prod` twice opens two tabs, `1 web-prod` and `2 web-prod`, each with its own SSH connection.
  - A tab's number is its position from the left (D-023). Closing a tab renumbers the tabs to its right: after closing tab 1 of tabs 1 to 3, the others become 1 and 2, and the next session gets number 3. The same holds when closing a middle or the last tab.
  - The host list stays fully usable while sessions run: the user can add, edit, delete or connect to hosts.

### FR-TAB-003 · Tab bar
The system shall show a one-line header that starts with `sshelf`, then the `Hosts` tab, then one entry per session tab (`<number> <host label>`) and per SFTP tab (`<number> sftp: <host label>`) in opening order, separated by `│`, with the focused tab highlighted.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-009, D-010; M3: D-017; v1: D-023
- **Acceptance:**
  - The focused tab is shown in reverse video, and its padding spaces are replaced by `[` and `]` (`│[2 web-prod]│`), so focus is not shown by colour alone (NFR-UX-003).
  - Labels are sanitised (SEC-SESSION-003) and cut to 20 columns with `…`. A tab shows the host label only; a window title set by the remote (OSC 0 or 2) is never shown (SEC-SESSION-005).
  - If the tabs do not fit, the bar keeps the focused tab visible and replaces the hidden tabs with `‹+N` on the left and `+N›` on the right. At 80 columns, 10 tabs with 12-character labels fit on one line (golden).
  - The header is shown on every screen, the Hosts tab and the Welcome screen included, and replaces the MVP's `sshelf` title line.
  - Session and SFTP tabs are numbered together by position from the left, so `Ctrl+]` `1`–`9` address both. Closing any tab renumbers the tabs to its right (D-023); a click on a tab, the `Ctrl+]` `q` and SFTP close questions ("SFTP tab 2 stays connected.", "Close SFTP tab 2?") use the current numbers.
  - The text after the number (`sftp: raspberrypi4`) is sanitised and cut to 20 columns with `…`, as a session label is.
  - Golden at 80×24 of the approved layout's header: `sshelf │ Hosts │ 1 raspberrypi4 │[2 sftp: raspberrypi4]│`.
  - An SFTP tab shows `●` when a transfer finishes or fails while the tab is in the background, and `✕` while its connection is lost (FR-SFTP-018), with the rules of FR-TAB-004.

### FR-TAB-004 · Activity and end markers
While a session tab is not focused, when its session receives output, the system shall mark that tab with `●` until it is focused; and when a session ends, the system shall mark its tab with `✕` until it is reconnected or closed.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-010
- **Acceptance:**
  - Output in a background tab shows `1 raspberrypi4 ●`; focusing the tab removes the marker. Output in the focused tab never sets it.
  - An ended session shows `✕` instead of `●`, focused or not.
  - Replies the emulator itself sends to the remote (ARCH-SESS-016) do not set the marker; only bytes received from the remote do.

### FR-TAB-005 · Prefix key
While any tab is focused and no modal is open, when the user presses `Ctrl+]`, the system shall treat the next key as a tab command (`q` disconnect or close, `?` help, `h` or `0` Hosts tab, `1`–`9` tab, `n` next tab, `p` previous tab, `[` scrollback mode, `s` SFTP tab for this host, `Ctrl+]` send a literal `Ctrl+]`) and shall send neither key to the remote.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-009, D-010 (the preview approved by the product owner); M3: D-017, D-018
- **Acceptance:**
  - Each command has a unit test on the prefix state machine and a test through the root model.
  - `Ctrl+]` `Ctrl+]` sends exactly one byte, `0x1d`, to the remote.
  - After `Ctrl+]`, the status bar shows `Ctrl+] …` until the next key. Esc, or any key that is not a command, cancels the prefix and is not sent to the remote. There is no timeout.
  - `Ctrl+]` `?` opens a help overlay listing every prefix command, generated from `ui/keys` (ARCH-UI-004); `?` or Esc closes it.
  - On the Hosts tab the prefix key works on every screen, including while a text field is focused; `q` and `[` do nothing there.
  - `Ctrl+]` `s` on a live session tab opens or focuses the SFTP tab on that session's connection (FR-SFTP-003); on an ended session tab it opens an SFTP tab for the same host through the normal connect flow (FR-SFTP-002); on the Hosts tab and on an SFTP tab it does nothing.
  - On an SFTP tab, `Ctrl+]` `q` closes that tab (FR-SFTP-025) and `Ctrl+]` `[` does nothing. Every other prefix command works there as on a session tab.
  - The help overlay lists `s`.

### FR-TAB-006 · Keys and paste go to the focused session
While a live session tab is focused and neither the prefix key, an overlay nor scrollback mode is active, the system shall send every key press and paste to that session's remote, encoded according to the emulator's current modes.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-009
- **Acceptance:**
  - Arrow keys send `ESC [ A` in normal cursor mode and `ESC O A` after the remote enables application cursor keys (`ESC [ ? 1 h`).
  - Ctrl+C sends `0x03`, Ctrl+Z `0x1a`, Esc `0x1b`, Alt+x `ESC x`, Ctrl+Left `ESC [ 1 ; 5 D`, Shift+Up `ESC [ 1 ; 2 A` and F5 `ESC [ 1 5 ~`. Every row of the key table in design §9.4 has a test.
  - A paste is sent as `ESC [ 200 ~` … `ESC [ 201 ~` while the remote has bracketed paste on (`ESC [ ? 2004 h`), and as plain text otherwise. Pasted text reaches the remote unchanged.
  - `q`, `Ctrl+C`, `?`, `/` and every other sshelf binding reach the remote unchanged and do nothing in sshelf.

### FR-TAB-007 · Session status bar
While a session tab is focused, the system shall show a one-line status bar with the session state, `user@host:port`, the connected duration and the main prefix-key hints.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-009
- **Acceptance:**
  - A live session shows `● connected · ubuntu@10.0.0.10:22 · 01:02:03 · Ctrl+] ? help · Ctrl+] h hosts`. The duration updates once a second. When the line does not fit, the hints are dropped first, then the duration.
  - An ended session shows its reason instead (FR-CONN-005 revision); scrollback mode shows its own hints (FR-TAB-010).
  - The status bar and tab bar never show a secret; the canary harness checks both (SEC-LOG-001, T-042).

### FR-TAB-008 · Background sessions keep running
While a session tab is not focused, the system shall keep reading that session's output into its emulator and keep its connection open.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-010
- **Acceptance:**
  - A remote command that prints 10,000 lines while its tab is in the background completes (sshtest records its exit), and focusing the tab shows the end of the output.
  - The remote is slowed down only when the emulator cannot keep up, never because its tab is in the background (ARCH-SESS-014).

### FR-TAB-009 · Quit with open sessions
When the user quits sshelf (`q` on the host list or the Welcome screen, or Ctrl+C on the Hosts tab) while at least one session is live, an SFTP tab is connected, or a transfer is queued, running or paused, the system shall ask "Quit and close N tabs?", followed by "M transfers will stop." when M is not zero, and quit only on `y`.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-010; M3: D-017, D-018
- **Acceptance:**
  - N counts live session tabs and SFTP tabs that are connected or have paused transfers; M counts queued, running, paused and waiting transfers in every SFTP tab.
  - The confirmation defaults to No: `n`, Enter and Esc keep sshelf running with every session and transfer intact.
  - On `y`, every transfer is cancelled as in FR-SFTP-015 (partial files removed), then every connection is closed. With nothing live and no transfer, `q` quits at once.
  - SIGTERM and SIGHUP do the same without asking, restore the terminal and exit 0.

### FR-TAB-010 · Scrollback mode
While a session tab is focused, when the user presses the prefix key followed by `[`, the system shall show the session's scrollback and screen as one view that PgUp, PgDn, the arrow keys, `g` and `G` move, until the user presses Esc.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-010, tmux copy mode
- **Acceptance:**
  - The scrollback keeps the last 2,000 lines that scrolled off the main screen (NFR-SIZE-003). The alternate screen used by `vim` or `htop` adds nothing to it.
  - The status bar shows `scrollback 120/2000 · ↑↓ PgUp PgDn g G · esc back`. Keys are not sent to the remote; output keeps arriving, and the view stays on the same lines until the user moves it or those lines leave the scrollback.
  - Mouse tracking is off in scrollback mode, so the terminal's own selection can copy what is shown (D-012).
  - Esc returns to the live screen, with the remote's cursor shown again.

### FR-TAB-011 · Mouse in a session only on request
While a session tab is focused, the system shall enable mouse tracking only while that session's remote app has mouse reporting on, and shall then forward each mouse event inside the session area to the remote, with coordinates relative to the session area and encoded as the app requested.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-012
- **Acceptance:**
  - Until the remote enables mouse reporting, sshelf writes no mouse-tracking sequence while the session is focused, so the terminal's own selection works (PTY output scan, NFR-UX-001 revision).
  - After the remote sends `ESC [ ? 1000 h ESC [ ? 1006 h`, a left press at terminal cell x=10, y=5 (zero-based, header on row 0) reaches the remote as `ESC [ < 0 ; 11 ; 5 M`. After `ESC [ ? 1000 l`, tracking is off again in the next frame.
  - Motion events are forwarded only when the app asked for them (`?1002` or `?1003`).
  - Events on the header or status bar are neither forwarded nor acted on.
  - Focusing the Hosts tab, or a session that has not requested mouse reporting, applies that tab's rule in the next frame (ARCH-UI-006).

---

## 19. History (`FR-HIST`)

### FR-HIST-001 · Last connection time
When an interactive session to a host is established, the system shall record the time and increment the host's connection count.

- **Priority:** v1 · **Status:** Draft
- **Acceptance:**
  - The details pane shows "Last connected 3 h ago (42 times)". The data feeds FR-NAV-009 and FR-NAV-018.
  - The time itself is recorded by FR-NAV-018; this requirement adds the count and the details pane.

### FR-HIST-002 · Session transcript logging
Where session logging is enabled for a host, the system shall write the session's output to a log file named with the host label and start time.

- **Priority:** Later · **Status:** Draft · **Source:** Termius, SecureCRT
- **Acceptance:**
  - Logging is off by default and per-host opt-in, with a warning that logs may contain secrets.

---

## 20. Storage and settings (`FR-CFG`)

### FR-CFG-001 · Config directory
The system shall keep its hosts, identities (without secrets), settings and `known_hosts` file in the sshelf config directory defined in ADR-005 of `../architecture/design.md`, creating it on first launch.

- **Priority:** MVP · **Status:** Implemented · **Source:** D-003
- **Acceptance:**
  - sshelf starts with no existing config directory and shows the Welcome screen (FR-NAV-013).
  - The directory and files are created with the permissions set in SEC-FILE-001 (0700 directory, 0600 files).

### FR-CFG-002 · Durable writes
When the user confirms a change, the system shall write it to disk before reporting it as saved, so that a crash leaves either the old or the new file contents.

- **Priority:** MVP · **Status:** Implemented · **Source:** ARCH-DATA-005, SEC-FILE-003 (atomic write)
- **Acceptance:**
  - After "Saved" is shown, a SIGKILL and restart still shows the change.
  - Killing the process during a write never leaves a truncated file.

### FR-CFG-003 · Broken config file
If a config file cannot be parsed, then the system shall show the file, line and problem, and shall not overwrite that file until the user fixes it.

- **Priority:** MVP · **Status:** Implemented
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

- **Priority:** MVP · **Status:** Implemented · **Source:** D-001
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

The MVP questions are answered by D-001 to D-008 and D-101 to D-109:
encryption of host data, locking, label completion, a system-ssh backend,
ssh-agent priority, stored keys, the label grammar, a `--host` launch flag for
tmux users (D-107: not for now, revisit `Later`) and secrets on a second
machine (D-108: re-enter them; shown as "not set on this device").

The M2 questions are answered too (status at the M2 gate, T-043):

1. **Scrollback size.** 10,000 lines per session costs about 65 MB at 80×24
   and 130 MB at 200×50 with the chosen emulator (measured, ADR-013), so 10
   sessions could need more than 1 GB. **D-013: 2,000 lines**, about 30 MB
   per session at 120×40 (NFR-SIZE-003). A setting for it is v1 (FR-CFG-009).
2. **Wide characters at the right margin.** `x/vt` v0.1.0 drops a
   double-width character (CJK, emoji) that lands on the last column of a
   line (ADR-013 limitation 2). **D-015: worked around in sshelf's emulator
   wrapper** (T-031): the character wraps to the next line as in xterm, and
   `TestARCH_TEST_002_WideCharacterAtMargin` pins it. M2 ships without the
   known issue; the wrapper's workaround is removed once an upstream release
   fixes it (re-checked on every `x/vt` upgrade, ADR-013).
3. **Remote window titles.** **D-014:** tabs show the host label only, and a
   title set by the remote is ignored (FR-TAB-003, SEC-SESSION-005).

No question is open for M2.


### M3 questions

Decisions D-017 to D-020 settle the scope. The spec makes these choices,
which the product owner may still change:

1. **Upload resume point.** D-018's rule "continue from the remote size if
   its mtime matches what sshelf recorded" cannot hold literally after a
   dropped connection: every write the server applies changes the part
   file's mtime, and concurrent writes can leave a gap before the end.
   FR-SFTP-017 therefore continues from the last 8 MiB **checkpoint** the
   server acknowledged, after checking size and mtime against that
   checkpoint, and truncates anything after it. At most about 8 MiB is sent
   twice.
2. **Hidden files are shown by default**, because the approved layout shows
   `.bashrc`; `.` hides them (FR-SFTP-008).
3. **Where "save mine as `<name>.local-edit`" goes.** Next to the remote
   file, so the temporary copy can still be deleted as D-019 requires
   (FR-SFTP-020).
4. **Edit locally is limited to files up to 64 MiB** (FR-SFTP-019); larger
   files are copied and edited by hand.
5. **The local pane starts in sshelf's working directory**, falling back to
   `$HOME` (FR-SFTP-005).
6. **One SFTP tab per connection.** `Ctrl+]` `s` focuses the existing SFTP
   tab of that connection rather than opening a second one, so "the queue
   per connection" is also "the queue per SFTP tab" (FR-SFTP-003).
7. **Keystrokes during a transfer on the same connection** (raised by
   T-058). An upload keeps 64 requests in flight (ADR-016), and they sit on
   the TCP connection ahead of a session's keys, so on the CI runners a key
   took 8 to 17 ms at p99 against NFR-PERF-004's 5 ms. **D-021: up to 50 ms
   (p99) while a transfer shares the connection**, with full upload speed;
   5 ms still holds with no transfer on it (NFR-PERF-008).

Status at the M3 gate (T-059): question 7 is answered by D-021. No
product-owner objection to choices 1 to 6 is recorded; they ship as
specified, and the M3 acceptance run (`acceptance.md`) is where the product
owner confirms them.
