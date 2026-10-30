# Glossary

These terms are shared by all sshelf specs. Requirements use them with
exactly these meanings. Where a term has a Termius or OpenSSH equivalent with
a different name, the equivalent is given in brackets. Binding product
decisions are in [`decisions.md`](decisions.md).

| Term | Definition |
|------|------------|
| **Activity marker** | The `●` shown on a background tab that received output since it was last focused (M2, FR-TAB-004). An ended session shows `✕` instead. |
| **Agent forwarding** | Making the local ssh-agent available to a remote host so that it can authenticate onward with the user's keys. Off by default (FR-CONN-011). |
| **Auto-start rule** | A forwarding rule that opens when an interactive session to its host starts and closes when the session ends (FR-FWD-002). |
| **Checkpoint** | For an upload, the bytes the server has acknowledged plus the remote part file's size and modification time, recorded every 8 MiB; an interrupted upload resumes from it (M3, FR-SFTP-017, ADR-018). |
| **Config directory** | The directory that holds sshelf's non-secret data: hosts, groups, identities without their secrets, snippets, settings and sshelf's `known_hosts` file. Its location is defined in ADR-005 (`architecture/design.md`): `$XDG_CONFIG_HOME/sshelf/` on Linux, `~/Library/Application Support/sshelf/` on macOS. Metadata is in a single `config.json` (D-106). It can be overridden with `--config <dir>` (v1). |
| **Conflict** [existing target] | A file being copied whose name already exists in the target directory. sshelf asks: overwrite, skip, or keep both (`<stem> (1)<ext>`), optionally for the rest of the batch (M3, FR-SFTP-016). |
| **Connection count / last connected** | Per-host history used for the "Recent" section and for sorting (FR-HIST-001). |
| **Double-click** | Two left mouse presses on the same row within 400 ms (M2, ARCH-UI-007). |
| **Drag** | Pressing the left button on an entry in one pane of an SFTP tab and releasing it over the other pane, which copies like `c` (M3, FR-SFTP-022). |
| **Edit locally** | `e` on a remote file: sshelf downloads it to a private temp copy, suspends its UI, runs `$VISUAL`, `$EDITOR` or `vi` on it, uploads it if it changed, and deletes the copy (M3, D-019, FR-SFTP-019). The only place sshelf starts a local program. |
| **Effective setting** | The value sshelf actually uses for a host field after inheritance: the host's own value, else the nearest ancestor group's value, else the global default (FR-GROUP-005). |
| **Emulator** [terminal emulator, VT emulator] | The in-process program that interprets a session's remote output (`xterm-256color` control sequences) into a screen of cells, a cursor, modes and scrollback, and encodes keys and mouse events for the remote. sshelf uses `charmbracelet/x/vt` (M2, ADR-013). Not to be confused with the user's own terminal (iTerm2, GNOME Terminal…), which only ever shows what sshelf renders. |
| **Ended tab** | A session tab whose session has ended. It keeps the last screen and offers `r` reconnect and `x` close (M2, FR-CONN-005 revision). |
| **Escape sequence** | MVP and v1 plans: a key sequence typed at the start of a line that sshelf would handle locally, following OpenSSH (`~.`). Dropped in M2 in favour of the *prefix key*; `~.` is sent to the remote like any text. Unrelated to ANSI escape sequences, which are terminal control codes. |
| **Fingerprint** | The SHA256 hash of a public key, shown as `SHA256:<base64>` in the same format as `ssh-keygen -lf`. |
| **Focused tab** | The one tab whose screen is drawn and which receives keys. Every other tab is a *background tab*. |
| **Forwarding rule** [Termius: port forwarding rule] | A saved port-forwarding definition attached to a host: local (`-L`), remote (`-R`) or dynamic SOCKS5 (`-D`). |
| **Frame** | sshelf's fixed layout around every screen and session from M2: a one-line header with the tab bar, the content area, and a one-line status bar (D-009, ARCH-UI-005). |
| **Fuzzy search** | Matching a query against host fields when the query's characters appear in order but not necessarily contiguously (for example `wbprd` matches `web-prod`), with results ranked by match quality. |
| **Group** | A named container of hosts. Groups may be nested and may define defaults that their hosts inherit. A host belongs to at most one group. |
| **Host** | A saved SSH destination: a unique label, a hostname or IP address, a port, an optional username and an optional linked identity, plus (v1) a group, tags, notes, a jump chain, forwarding rules and a startup command. A host never holds secrets. |
| **Host key** | The public key an SSH server presents to prove its identity during the handshake. |
| **Hosts tab** | The first tab, always present, which holds sshelf's own screens: the Welcome screen, the host list, forms, identities and import (M2). |
| **Identity** | A reusable, named set of credentials: a username plus an optional password and/or private key (a file path or a stored key) and key passphrase. An identity can be linked to many hosts. Its secret values live in the OS credential store. A *hidden* identity is created from credentials typed into the host form, belongs to that one host and is not listed in the identities view (FR-HOST-010). |
| **Import report** | The summary shown after an import: what was created, which aliases were skipped and which directives were not imported (FR-IMPORT-003). |
| **Inheritance** | The mechanism by which hosts take default values from their group and its ancestors when they do not set a field themselves. |
| **Interactive session** | An SSH connection with a PTY and a shell (or startup command). In the MVP sshelf hands it the whole terminal until it ends; from M2 it runs in a session tab inside the frame, rendered through the emulator. |
| **Jump host / jump chain** [Termius: host chaining; OpenSSH: `ProxyJump`] | One or more saved hosts that sshelf connects through, in order, to reach a target host that is not directly reachable. |
| **Keepalive** | A periodic SSH request used to detect a dead connection (FR-CONN-009). |
| **Key file reference** | An identity's pointer to a private key file on disk, read at connection time. Contrast with a *stored key* (FR-IDENT-003). |
| **Known host / trusted key** | A host key that sshelf accepts for a given host and port without prompting. Trusted keys come from `~/.ssh/known_hosts` (read, never modified) or from sshelf's own `known_hosts` file in the config directory. |
| **Label** | The unique (case-insensitive), human-chosen name of a host, such as `web-prod` or `Web Prod EU`. Labels may contain spaces. |
| **Lease** [connection reference] | One tab's hold on a shared SSH connection (`sshclient.Lease`). The connection closes when its last lease is released (M3, ADR-017). |
| **Mark** | `space` on an entry of an SFTP pane selects it for the next copy or delete; marked rows show `*` (M3, FR-SFTP-009). |
| **Mouse reporting** | A mode a remote program (vim, htop, tmux) turns on with `ESC[?1000h` and similar to receive mouse events. In M2 sshelf forwards mouse events to a session only while its program has mouse reporting on (D-012, FR-TAB-011). |
| **Multi-host run** | Running one snippet non-interactively on several selected hosts at once, with per-host status and output (FR-SNIP-004). Not to be confused with typing broadcast, which sshelf does not offer. |
| **OS credential store** [also "keychain" in the design docs] | The operating system's secret storage: the macOS Keychain, or the Linux Secret Service (GNOME Keyring or KWallet), accessed through `zalando/go-keyring` (D-008). Access is gated by the user's OS login. sshelf stores all passwords, stored keys and passphrases here and has no password of its own. Details are in `SEC-CRED-*`. |
| **Pane** | One half of an SFTP tab: the **local pane** (left) shows the local file system, the **remote pane** (right) the host's. The **focused pane** has the `>` cursor and receives keys (M3, D-017). |
| **Part file** | `<name>.sshelf-part`, the file a transfer writes before it is complete; it is renamed to the final name only at the end, and a download resumes from its size (M3, D-020, SEC-SFTP-003). |
| **Paused transfer** | A queued or running transfer whose connection dropped. It keeps its part file and records until the tab reconnects (`r`) and it resumes, or it is cancelled (M3, FR-SFTP-018). |
| **Prefix key** | `Ctrl+]`. In M2 the key after it is a tab command (`q` disconnect, `?` help, `h`/`0` Hosts tab, `1`–`9` tab, `n`/`p` next/previous, `[` scrollback mode, `Ctrl+]` a literal `Ctrl+]`; M3 adds `s` SFTP tab) instead of input for the remote (FR-TAB-005). |
| **Progress line** | The line under an SFTP tab's panes: the oldest running transfer's direction, name, bytes, bar, percentage and rate, or the last status message (M3, FR-SFTP-014). |
| **Quick connect** | Connecting to an ad hoc `user@host:port` without saving a host first (FR-CONN-013). |
| **Scrollback** | The lines that scrolled off the top of a session's main screen, kept by the emulator up to a cap (2,000 lines, NFR-SIZE-003). The alternate screen used by full-screen programs has none. |
| **Scrollback mode** | A view of a session's scrollback and screen that the user moves with the keyboard, entered with `Ctrl+]` `[` and left with Esc. Input to the remote pauses; output keeps arriving (M2, FR-TAB-010). |
| **Secret** | A password, private key or key passphrase. Secrets are only ever stored in the OS credential store, never in the config directory. |
| **Session** | A single SSH connection opened by sshelf: interactive, a non-interactive snippet run, tunnel-only, or file transfer. From M3 an interactive session and an SFTP tab may share one connection (see *Shared connection*). |
| **Session area** | The rows of the frame between the header and the status bar: the terminal width by its height minus 2. It is the size of every session's emulator and remote PTY (M2). |
| **Session tab** [Termius: tab] | A tab that shows one interactive session, numbered by its position from the left, from 1, and labelled with the host label (`2 web-prod`). The same host can be open in several tabs (M2, D-010). |
| **SFTP tab** | A tab with a local and a remote pane for one host, labelled `<n> sftp: <label>` (M3, D-017). Opened with `s` on the host list (new connection) or `Ctrl+]` `s` on a session tab (that session's connection, no second login). One per connection. |
| **Shared connection** | An SSH connection used by both a session tab and its SFTP tab. Ending either tab leaves the other working (M3, D-018, FR-SFTP-004). |
| **Snippet** | A saved, named command or script, optionally with `{{variables}}`, that can be run on one or many hosts. |
| **Startup command** | A command run as a host's remote command instead of the login shell, for example `tmux new -A -s main`. |
| **Status bar** | The last line of the frame on a session tab: state, `user@host:port`, connected duration and prefix-key hints (FR-TAB-007). On the Hosts tab the MVP status line and key-hints footer remain. |
| **Stored key** | A private key whose contents the user pasted or imported into sshelf. It is kept in the OS credential store, so the original file is no longer needed (FR-IDENT-003). The original file is left untouched. Keys over 2800 bytes cannot be stored and must be key file references (D-008). |
| **Tab bar** | The header line of the frame: `sshelf │ Hosts │ 1 raspberrypi4 ● │[2 web-prod]│`, with the focused tab highlighted (FR-TAB-003). From M3 it also lists SFTP tabs (`3 sftp: web-prod`). |
| **Tag** | A short lowercase keyword attached to a host for filtering (`#db`). A host can have many tags. |
| **TOFU** (trust on first use) | Accepting a server's host key the first time it is seen, after the user checks its fingerprint in the unknown-host-key prompt (FR-KH-001, SEC-HOSTKEY-004). |
| **Transfer** | One file or directory being copied between the panes, with a state: queued, running, waiting, paused, done, failed, cancelled or skipped (M3, FR-SFTP-015). |
| **Transfer queue** | The per-connection list of transfers, run two at a time; shown with `t` as the **transfers view** (M3, ADR-018). |
| **TUI** | Terminal user interface. sshelf is TUI-only: a full-screen, keyboard-driven interface built with Bubble Tea, with no non-interactive subcommands. |
| **Tunnel-only mode** | Connecting to a host only to open its forwarding rules, without a remote shell (equivalent to `ssh -N`). |
