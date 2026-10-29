# Glossary

These terms are shared by all sshelf specs. Requirements use them with
exactly these meanings. Where a term has a Termius or OpenSSH equivalent with
a different name, the equivalent is given in brackets. Binding product
decisions are in [`decisions.md`](decisions.md).

| Term | Definition |
|------|------------|
| **Agent forwarding** | Making the local ssh-agent available to a remote host so that it can authenticate onward with the user's keys. Off by default (FR-CONN-011). |
| **Auto-start rule** | A forwarding rule that opens when an interactive session to its host starts and closes when the session ends (FR-FWD-002). |
| **Config directory** | The directory that holds sshelf's non-secret data: hosts, groups, identities without their secrets, snippets, settings and sshelf's `known_hosts` file. Its location is defined in ADR-005 (`architecture/design.md`): `$XDG_CONFIG_HOME/sshelf/` on Linux, `~/Library/Application Support/sshelf/` on macOS. Metadata is in a single `config.json` (D-106). It can be overridden with `--config <dir>` (v1). |
| **Connection count / last connected** | Per-host history used for the "Recent" section and for sorting (FR-HIST-001). |
| **Effective setting** | The value sshelf actually uses for a host field after inheritance: the host's own value, else the nearest ancestor group's value, else the global default (FR-GROUP-005). |
| **Escape sequence** | A key sequence typed at the start of a line during an interactive session that sshelf handles locally instead of sending it, following OpenSSH (for example `~.` to disconnect, FR-CONN-014). |
| **Fingerprint** | The SHA256 hash of a public key, shown as `SHA256:<base64>` in the same format as `ssh-keygen -lf`. |
| **Forwarding rule** [Termius: port forwarding rule] | A saved port-forwarding definition attached to a host: local (`-L`), remote (`-R`) or dynamic SOCKS5 (`-D`). |
| **Fuzzy search** | Matching a query against host fields when the query's characters appear in order but not necessarily contiguously (for example `wbprd` matches `web-prod`), with results ranked by match quality. |
| **Group** | A named container of hosts. Groups may be nested and may define defaults that their hosts inherit. A host belongs to at most one group. |
| **Host** | A saved SSH destination: a unique label, a hostname or IP address, a port, an optional username and an optional linked identity, plus (v1) a group, tags, notes, a jump chain, forwarding rules and a startup command. A host never holds secrets. |
| **Host key** | The public key an SSH server presents to prove its identity during the handshake. |
| **Identity** | A reusable, named set of credentials: a username plus an optional password and/or private key (a file path or a stored key) and key passphrase. An identity can be linked to many hosts. Its secret values live in the OS credential store. A *hidden* identity is created from credentials typed into the host form, belongs to that one host and is not listed in the identities view (FR-HOST-010). |
| **Import report** | The summary shown after an import: what was created, which aliases were skipped and which directives were not imported (FR-IMPORT-003). |
| **Inheritance** | The mechanism by which hosts take default values from their group and its ancestors when they do not set a field themselves. |
| **Interactive session** | An SSH connection with a PTY and a shell (or startup command) to which sshelf hands the whole terminal until it ends. |
| **Jump host / jump chain** [Termius: host chaining; OpenSSH: `ProxyJump`] | One or more saved hosts that sshelf connects through, in order, to reach a target host that is not directly reachable. |
| **Keepalive** | A periodic SSH request used to detect a dead connection (FR-CONN-009). |
| **Key file reference** | An identity's pointer to a private key file on disk, read at connection time. Contrast with a *stored key* (FR-IDENT-003). |
| **Known host / trusted key** | A host key that sshelf accepts for a given host and port without prompting. Trusted keys come from `~/.ssh/known_hosts` (read, never modified) or from sshelf's own `known_hosts` file in the config directory. |
| **Label** | The unique (case-insensitive), human-chosen name of a host, such as `web-prod` or `Web Prod EU`. Labels may contain spaces. |
| **Multi-host run** | Running one snippet non-interactively on several selected hosts at once, with per-host status and output (FR-SNIP-004). Not to be confused with typing broadcast, which sshelf does not offer. |
| **OS credential store** [also "keychain" in the design docs] | The operating system's secret storage: the macOS Keychain, or the Linux Secret Service (GNOME Keyring or KWallet), accessed through `zalando/go-keyring` (D-008). Access is gated by the user's OS login. sshelf stores all passwords, stored keys and passphrases here and has no password of its own. Details are in `SEC-CRED-*`. |
| **Quick connect** | Connecting to an ad hoc `user@host:port` without saving a host first (FR-CONN-013). |
| **Secret** | A password, private key or key passphrase. Secrets are only ever stored in the OS credential store, never in the config directory. |
| **Session** | A single SSH connection opened by sshelf: interactive, a non-interactive snippet run, tunnel-only, or file transfer. |
| **Snippet** | A saved, named command or script, optionally with `{{variables}}`, that can be run on one or many hosts. |
| **Startup command** | A command run as a host's remote command instead of the login shell, for example `tmux new -A -s main`. |
| **Stored key** | A private key whose contents the user pasted or imported into sshelf. It is kept in the OS credential store, so the original file is no longer needed (FR-IDENT-003). The original file is left untouched. Keys over 2800 bytes cannot be stored and must be key file references (D-008). |
| **Tag** | A short lowercase keyword attached to a host for filtering (`#db`). A host can have many tags. |
| **Tunnel-only mode** | Connecting to a host only to open its forwarding rules, without a remote shell (equivalent to `ssh -N`). |
| **TOFU** (trust on first use) | Accepting a server's host key the first time it is seen, after the user checks its fingerprint in the unknown-host-key prompt (FR-KH-001, SEC-HOSTKEY-004). |
| **TUI** | Terminal user interface. sshelf is TUI-only: a full-screen, keyboard-driven interface built with Bubble Tea, with no non-interactive subcommands. |
