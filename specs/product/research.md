# Product research: Termius and comparable tools

Status: Draft · Owner: product · Last updated: 2026-10-29

The product owner decisions in [`../decisions.md`](../decisions.md) are binding
and take precedence over this analysis.

This document records the competitor analysis that informed
[`requirements.md`](requirements.md). It is a snapshot. Feature claims come from
vendor documentation where available (see [Sources](#sources)). Claims taken
only from third-party reviews are marked "(unverified)".

## 1. Tools surveyed

| Tool | Kind | Platform | Licence | How it stores hosts and secrets |
|------|------|----------|---------|----------------------------------|
| **Termius** | GUI SSH client and host manager | macOS, Windows, Linux, iOS, Android | Proprietary, freemium | Vaults, end-to-end encrypted and synced through Termius cloud; credentials can be kept local-only |
| **Royal TSX / Royal TS** | GUI multi-protocol connection manager | macOS (TSX), Windows (TS) | Proprietary | "Documents": portable files that can be password-encrypted; credentials are reusable objects |
| **SecureCRT** | GUI terminal emulator with session manager | macOS, Windows, Linux | Proprietary | Session database, optionally encrypted with a configuration passphrase |
| **MobaXterm** | GUI all-in-one terminal (SSH, X11, RDP, SFTP) | Windows only | Proprietary, freemium | Session bookmarks; master password protects saved credentials |
| **Tabby** | Electron terminal with SSH profiles | macOS, Windows, Linux | MIT | Config file plus an optional "vault" encrypted with a passphrase; config sync through a GitHub Gist |
| **Warp** | Modern terminal (blocks, AI) | macOS, Linux, Windows | Proprietary | No host inventory; uses system `ssh` and `~/.ssh/config`; "Workflows" are parameterised snippets |
| **sshs** | TUI host picker (Rust) | macOS, Linux, Windows | MIT | Reads `~/.ssh/config` only; no secrets |
| **lazyssh** | TUI host manager (Go) | macOS, Linux | Apache-2.0 | Reads and writes `~/.ssh/config` with backups; no secrets |
| **storm** | CLI `~/.ssh/config` editor (Python) | macOS, Linux | MIT | Writes `~/.ssh/config`; project is archived |
| **assh** | `ssh` wrapper and config generator (Go) | macOS, Linux | MIT | YAML source that generates `~/.ssh/config`; no secrets |

## 2. Feature comparison

Legend: **Y** = supported, **P** = partial or limited, **-** = not supported,
**?** = not confirmed. The last column records how well the feature fits a TUI.
That column drove the priorities in `requirements.md`.

| Feature | Termius | Royal TSX | SecureCRT | MobaXterm | Tabby | Warp | sshs | lazyssh | storm | assh | TUI fit |
|---------|:-------:|:---------:|:---------:|:---------:|:-----:|:----:|:----:|:-------:|:-----:|:----:|:-------:|
| Saved hosts (label, address, user, port) | Y | Y | Y | Y | Y | - | Y (from config) | Y | Y | Y | Good |
| Groups / folders | Y (nested) | Y (nested) | Y | Y | Y | - | - | - | - | P (templates) | Good |
| Settings inherited from group | Y | Y | P | - | - | - | - | - | - | Y (templates) | Good |
| Tags | Y | ? | - | - | - | - | - | Y (as comments) | - | - | Good |
| Reusable identities / credentials | Y | Y | P | P | P | - | - | - | - | - | Good |
| Protected local secret store | Y | Y | Y | Y | Y (vault) | - | - | - | - | - | Good |
| Key generation in the app | Y | - | Y | Y | - | - | - | - | - | - | Good |
| FIDO2 / hardware-backed keys | Y | ? | Y | ? | ? | via ssh | via ssh | via ssh | via ssh | via ssh | Partial (needs agent) |
| ssh-agent use | Y (own agent) | Y | Y | Y | Y | via ssh | via ssh | via ssh | via ssh | via ssh | Good |
| Agent forwarding | Y | Y | Y | Y | Y | via ssh | via ssh | Y (config) | Y (config) | Y | Good |
| Fuzzy search | Y | Y | P | P | P | - | Y | Y | Y (CLI) | - | Good |
| Favourites / pinning | ? | Y | - | - | - | - | - | Y | - | - | Good |
| Snippets / saved commands | Y | Y (tasks) | Y (command manager) | Y (macros) | P (login scripts) | Y (workflows) | - | - | - | - | Good |
| Snippet variables / parameters | ? | Y | P | - | - | Y | - | - | - | - | Good |
| Run a command on many hosts | Y | P | Y (send to all) | Y (multi-exec) | - | - | - | - | - | - | Good (non-interactive) |
| Local / remote / dynamic forwarding | Y | Y | Y | Y | Y | via ssh | via ssh | Y (config) | Y (config) | Y | Good |
| Jump host / chaining | Y | Y | Y | Y | Y | via ssh | via ssh | Y | Y | Y (with fallback) | Good |
| HTTP / SOCKS proxy | Y | Y | Y | Y | Y | via ssh | via ssh | P | P | Y | Good |
| Known-hosts verification UX | Y | Y | Y | Y | Y | via ssh | via ssh | via ssh | via ssh | via ssh | Good |
| SFTP file browser | Y | Y | Y (also SecureFX) | Y (auto sidebar) | Y | P (file tree) | - | - | - | - | Partial (two-pane browser is feasible but large) |
| Tabs | Y | Y | Y | Y | Y | Y | - | - | - | - | Poor (needs an embedded terminal emulator) |
| Split panes | Y | ? | Y | Y | Y | Y | - | - | - | - | Poor (tmux does this better) |
| Broadcast typing to sessions | Y | ? | Y | Y | - | - | - | - | - | - | Poor |
| Remote-shell autocomplete | Y | - | - | - | - | Y | - | - | - | - | Poor (the shell already does this) |
| Cloud sync | Y (E2EE) | P (shared file) | - | - | Y (Gist) | Y | - | - | - | - | Out of scope (users sync the file) |
| Team sharing | Y | Y | - | - | - | Y | - | - | - | - | Out of scope |
| Themes | Y | Y | Y | Y | Y | Y | - | P | - | - | Good (UI chrome only) |
| Session logs / recording | Y | Y | Y | Y | P | Y | - | - | - | P (hooks) | Partial |
| Connection history / recent | Y | Y | P | Y | P | - | - | P (last seen) | - | - | Good |
| Mosh | Y | - | - | Y | - | - | - | - | - | - | Partial (delegate to `mosh`) |
| Telnet / serial | Y | Y | Y | Y | Y | - | - | - | - | - | Out of scope |
| RDP / VNC / X11 | - | Y | - | Y | P (X11 fwd) | - | - | - | - | - | Out of scope |
| Import `~/.ssh/config` | Y | Y | ? | ? | Y | n/a | n/a (native) | n/a (native) | n/a (native) | Y | Good |
| Import from other clients (PuTTY, SecureCRT, MobaXterm, CSV) | Y | Y | P | Y | - | - | - | - | - | - | Good |
| Generate / export `ssh_config` | - | - | - | - | - | - | - | n/a | n/a | Y | Good |
| Cloud inventory import (AWS, DO, Azure) | Y | Y (dynamic folders) | - | - | - | - | - | - | - | - | Partial |
| Connect / disconnect hooks | - | P | Y (scripts) | - | P | - | - | - | - | Y | Good |
| Reachability check (ping) | ? | Y | - | - | - | - | - | Y | - | - | Good |
| Copy the equivalent `ssh` command | - | - | - | - | - | - | - | Y | - | - | Good |
| Non-interactive CLI | - | P | P | - | - | - | P | - | Y | Y | Good, but out of scope (D-001) |
| AI command generation | Y | - | - | - | - | Y | - | - | - | - | Out of scope |

## 3. What each tool does well

- **Termius.** It has the most complete data model for a host manager.
  *Identities* (username plus password and/or key) are reusable objects linked
  to hosts and groups. *Groups* pass protocol options, credentials, jump hosts
  and ports down to their hosts, and a host can override them. *Host chaining*
  is a single field. *Snippets* can run on many hosts. Importing from
  `~/.ssh/config` maps `ProxyJump` to a host chain and ignores `ProxyCommand`.
  It also imports `known_hosts` and key files, and it reads CSV, PuTTY,
  MobaXterm and SecureCRT exports. Its weak points for our audience are a
  required account for sync, a paid tier, a heavy Electron GUI, and feature
  sprawl (AI, teams, Telnet/serial).
- **Royal TSX.** A *document* is one portable, password-encrypted file that
  users share or sync however they like. sshelf considered this model and
  rejected it, because it needs a master password (D-002). Credentials are
  reusable objects that can be inherited from the parent folder. Known gap:
  the username lives inside the credential, so the same key used as two
  different users needs two credentials. sshelf should allow the host to
  override the username.
- **SecureCRT.** Its *Command Manager* and button bar organise reusable
  commands into folders, and its scripting is strong. It also sends one command
  to all sessions and keeps detailed session logs.
- **MobaXterm.** Multi-exec runs one command in many sessions. The SFTP sidebar
  opens automatically on connect. The GUI tunnel manager shows each forwarding
  rule with a start/stop toggle.
- **Tabby.** It is an open-source, cross-platform precedent for a passphrase
  "vault" of secrets kept beside a plain config. It offers quick-connect
  (`user@host:port`) without saving and has automatic reconnection
  (unverified).
- **Warp.** *Workflows* are named commands with typed parameters and
  descriptions. This is the best model we found for snippet variables.
- **sshs.** It does one thing quickly: it reads `~/.ssh/config`, filters as you
  type, and runs `ssh`. Startup cost is close to zero. This is the bar for
  sshelf's "find and connect" loop.
- **lazyssh.** A Go TUI close to sshelf's stack. Useful ideas: pin favourites,
  tags, ping a host, copy the `ssh` command, and write the config file
  atomically with a backup. Limitation: the inventory is `~/.ssh/config`, so it
  cannot store passwords or reusable identities.
- **storm.** A scriptable CLI (`add`, `edit`, `delete`, `list`, `search`) over
  `~/.ssh/config`. sshelf deliberately does not follow it, because sshelf is
  TUI-only (D-001).
- **assh.** *Gateways* support chains and a list of fallbacks (if the first
  bastion is down, try the next). It has `OnConnect`/`OnDisconnect` hooks and
  templates with inheritance. It also generates a standard `ssh_config` so
  other tools (git, rsync, scp) keep working.

## 4. Fit for a TUI

Features that suit a keyboard-driven TUI:

- The inventory: hosts, groups, tags, identities, fuzzy search, favourites and
  recent hosts.
- Forms for editing hosts and identities.
- Prompts for known-hosts trust decisions and passwords.
- A list of forwarding rules with start/stop toggles.
- A snippet picker with parameter prompts.
- Running one command on many hosts with output collected per host. This is a
  batch job, not interactive broadcast.
- Import and export.

Features that suit a TUI poorly:

- **Tabs, split panes and typing broadcast.** These need a full terminal
  emulator (VT parser, scrollback, rendering) inside Bubble Tea. tmux, zellij
  and the user's terminal already do this well. sshelf hands the whole
  terminal to the session.
- **Remote-shell autocomplete and AI command generation.** The remote shell
  already completes commands, and sshelf would have to intercept keystrokes.
- **RDP, VNC, X11, Telnet and serial.** These protocols are outside the
  product's scope.
- **An SFTP two-pane browser.** It is feasible (Midnight Commander proves it)
  but large. It is deferred to `Later`, together with a simpler single-file
  transfer action.

## 5. Positioning: what sshelf should be

**sshelf is the fastest way to find and open the right SSH connection, with
the right credentials, from the terminal you are already in.** It combines
Termius's data model (identities, groups with inheritance, host chaining,
snippets, forwarding rules) with the speed of `sshs` and `lazyssh`. Host
details live in a plain config file. Secrets live in the OS credential store
(macOS Keychain or Linux Secret Service), so there is no extra password to
manage (D-002, D-003).

Principles:

1. **Keyboard-first and instant.** Opening sshelf, typing a few characters and
   pressing Enter connects you. Every action has a key.
2. **Local-first, no extra password.** Secrets go in the OS credential store,
   which the user's OS login already protects. There is no account, no server
   and no telemetry.
3. **Compatible with OpenSSH.** sshelf imports `~/.ssh/config` and
   `known_hosts`, uses the user's ssh-agent (v1), and can export an
   `ssh_config` (v1). Leaving sshelf costs nothing.
4. **The terminal multiplexer is someone else's job.** sshelf manages
   connections. tmux and the terminal emulator manage panes.
5. **TUI-only and simple.** There are no subcommands: you launch `sshelf`,
   pick a host and connect (D-001, D-007).

### What sshelf will deliberately NOT do

| Not doing | Why |
|-----------|-----|
| Hosted cloud sync, accounts, team vaults or sharing | Single-user and local-first. Users can sync the config directory themselves. Secrets stay in each machine's OS credential store. |
| A master password or an encrypted vault file | The OS credential store already protects secrets behind the user's login (D-002, D-003). |
| A non-interactive CLI (`sshelf connect`, `list`, `exec`) | sshelf is TUI-only to keep it simple (D-001). |
| Built-in tabs, split panes and keystroke broadcast in v1 | These need an embedded terminal emulator, and tmux, zellij and terminal emulators do it better. In-app multi-session is `Later` at most. |
| Remote-shell autocomplete and AI command generation | They duplicate the shell, risk leaking keystrokes, and bring cloud dependencies. |
| Telnet, serial, RDP, VNC, X11 server | sshelf is an SSH tool. |
| A GUI or mobile app | sshelf is a TUI. |
| Rewriting the user's `~/.ssh/config` in place | sshelf only reads it on import. Export writes a separate file that the user can `Include`. This avoids the corruption risk that lazyssh and storm must guard against. |
| Session recording on by default | Recordings capture secrets typed in sessions. Logging, if ever offered, is opt-in per host (`Later`). |
| Windows in v1 | macOS and Linux come first. Windows is `Later`. |

## Sources

- [Termius documentation](https://docs.termius.com), [Termius docs index](https://docs.termius.com/llms.txt), [Termius full docs text](https://docs.termius.com/llms-full.txt), [Termius glossary](https://termius.com/documentation/glossary)
- [Royal TS object references and inheritance](https://docs.royalapps.com/r2021/scripting/rjson/object-references-and-inheritance.html), [Royal TSX features](https://royalapps.com/ts/mac/features-all), [Royal support thread on credential usernames](https://support.royalapps.com/support/discussions/topics/17000010678)
- [G2: PuTTY vs MobaXterm vs SecureCRT discussion](https://www.g2.com/discussions/putty-vs-mobaxterm-vs-securecrt-for-it-teams-comparing-terminal-emulators-across-windows-linux-and-cross-platform-deployment-scenarios-which-handles-the-widest-range-without-switching-tools), [G2: MobaXterm vs Tabby](https://www.g2.com/compare/mobaxterm-vs-tabby-sh), [FitGap: Tabby](https://us.fitgap.com/products/tabby-sh), [AlternativeTo: SecureCRT](https://alternativeto.net/software/securecrt/?origin=eu)
- [Warp: Warpify SSH](https://docs.warp.dev/features/warpify/ssh), [Warp: legacy SSH wrapper](https://docs.warp.dev/terminal/warpify/ssh-legacy)
- [sshs README](https://github.com/quantumsheep/sshs/blob/master/README.md)
- [lazyssh README](https://cdn.jsdelivr.net/gh/adembc/lazyssh@main/README.md), [lazyssh on DEV Community](https://dev.to/adem_bc/lazyssh-a-terminal-based-ssh-manager-with-a-simple-ui-1ldh)
- [storm](https://www.github.com/emre/storm), [storm docs](https://storm.readthedocs.io/)
- [assh on pkg.go.dev](https://pkg.go.dev/moul.io/assh), [assh repository](https://github.com/moul/assh)
