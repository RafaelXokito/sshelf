# sshelf

A terminal UI for managing and connecting to your SSH hosts, inspired by Termius.

- Keep your hosts in one keyboard-driven list, with fuzzy search
- Passwords and private keys stored in your OS credential store (macOS
  Keychain / Linux Secret Service), never on disk, and no master password
- Its own SSH client: host-key trust on first use, a hard block on changed
  keys, keyboard-interactive and one-time codes
- Several sessions at once as tabs, rendered inside sshelf's frame, with
  scrollback; `vim`, `htop`, `less` and `tmux` work in them
- Mouse on sshelf's screens, without taking your terminal's text selection
  away in a plain shell
- One-time import of `~/.ssh/config`

> **Status: M2 (framed sessions, tabs, mouse) feature-complete, in
> acceptance.** Every MVP and M2 requirement is implemented and traced to
> tests (`go run ./scripts/tracecheck`); the manual acceptance run on macOS
> and Linux ([`specs/acceptance.md`](specs/acceptance.md)) is pending
> sign-off. Groups, tags, ssh-agent, jump hosts, port forwarding
> and snippets are planned for v1 ([`specs/tasks.md`](specs/tasks.md)).
> Windows is not supported yet.

## Build & run

Needs Go (see `go.mod` for the version). No cgo.

```sh
go build -o sshelf ./cmd/sshelf
./sshelf            # opens the host list; needs an interactive terminal
./sshelf --version
```

To try it without a server, run the demo SSH server in another terminal; it
prints a known_hosts line and a demo key:

```sh
go run ./internal/testutil/sshtest/cmd/sshtestd -port 2201 -password demo
```

## Usage

The first launch shows a Welcome screen: press `I` to import
`~/.ssh/config` or `a` to add a host. The footer always shows the keys that
apply.

| Where | Keys |
|---|---|
| Host list | `↑`/`k` `↓`/`j` move · `g`/`G` first/last · `enter` connect · `/` search · `a` add · `e` edit · `d` delete · `i` identities · `I` import · `q` quit |
| Search | type to filter (fuzzy) · `↑`/`↓` move · `enter` connect · `esc` clear |
| Host and identity forms | `tab`/`shift+tab` next/previous field · `←`/`→` change a choice · `ctrl+s` save · `esc` cancel · `ctrl+x` clear a stored secret · `ctrl+o` on Key file: store the key in the keychain |
| Identities | `a` add · `e`/`enter` edit · `d` delete · `esc` back |
| Connecting / prompts | `enter` submit · `tab` next field · `esc` cancel the attempt |
| Unknown host key | `y` trust and save · `enter`/`esc` cancel |
| Confirmations | `y` yes · `n`/`enter`/`esc` no |
| Anywhere on sshelf's screens | `ctrl+c` quits (cancels a form) |

#### Tabs

Every connection opens in its own tab inside sshelf's frame: a header line
with the tab bar (`sshelf │ Hosts │[1 web-prod]│ 2 db-staging ● │`, the
focused tab in brackets), the remote screen, and a status bar
(`● connected · user@host:port · 00:12:34 · Ctrl+] ? help · Ctrl+] h hosts`).
`vim`, `htop`, `less` and `tmux` run inside the frame; the remote sees
`TERM=xterm-256color` and the size of the area between the two lines. The
host list stays reachable on the **Hosts** tab, and sessions keep running in
background tabs (`●` marks new output, `✕` an ended session). The same host
can be open in several tabs.

While a session is focused every key goes to the remote, `ctrl+c`, `q` and
`esc` included. sshelf's commands use the prefix `ctrl+]`: press it, let go,
then press one of these keys.

| `ctrl+]` then | Does |
|---|---|
| `1` … `9` | focus that tab |
| `h` or `0` | focus the Hosts tab (the sessions keep running) |
| `n` / `p` | next / previous tab |
| `[` | scrollback mode |
| `q` | disconnect this session (asks first) |
| `?` | list these commands |
| `ctrl+]` | send a literal `ctrl+]` to the remote |
| `esc`, or any other key | cancel; nothing is sent |

When a session ends (remote `exit`, dropped connection, `ctrl+]` `q`), its
tab keeps the last screen and the status bar shows why, for example
`✕ exited (3) · r reconnect · x close`: `r` reconnects in the same tab and
`x` closes it. Quitting sshelf with live sessions asks
"Quit and close N sessions?" first.

#### Scrollback

`ctrl+]` `[` shows the session's history: the last 2,000 lines plus the
screen. `↑`/`↓` line, `PgUp`/`PgDn` page, `g`/`G` top/bottom, `esc` back to
the live screen. The view stays on the same lines while new output arrives.
Mouse tracking is off in scrollback mode, so you can select and copy with
your terminal as usual.

#### Mouse

On sshelf's own screens you can click to select a host, double-click to
connect, use the wheel to scroll, click the search field, click form fields,
the `‹ ›` choice arrows and the `[ Save ]` / `[ Cancel ]` buttons, click
modal buttons, and click tabs in the header. Every mouse action also has a
key.

Inside a session sshelf leaves the mouse to your terminal, so its normal
text selection and copy work in a plain shell. Only when the remote program
asks for mouse reporting (for example `vim` with `set mouse=a`, `htop`, or
`tmux` with `set -g mouse on`) are clicks, drags and the wheel sent to it;
clicks on the header and status bar are then ignored, so switch tabs with
the keyboard. Remote programs cannot set your terminal's title or clipboard:
their output only reaches sshelf's emulator, never your terminal directly.

### Where data and secrets live

| What | macOS | Linux |
|---|---|---|
| Hosts and identities (`config.json`, mode 0600, no secrets) | `~/Library/Application Support/sshelf/` | `$XDG_CONFIG_HOME/sshelf/` (default `~/.config/sshelf/`) |
| Trusted host keys (sshelf's own `known_hosts`) | same directory | same directory |
| Passwords, private keys, key passphrases | login Keychain, service `sshelf`, account `<identity-uuid>/<field>` | Secret Service (GNOME Keyring, KWallet), same service and account |
| Debug log (only with `SSHELF_DEBUG=1`) and crash reports | `~/Library/Logs/sshelf/` | `$XDG_STATE_HOME/sshelf/` (default `~/.local/state/sshelf/`) |

On macOS an explicitly set `XDG_CONFIG_HOME` is honoured too. sshelf reads
`~/.ssh/known_hosts` but never writes it, and never changes `~/.ssh/config`.
Secrets do not sync between machines; a synced config shows them as "not set
on this device". Without a credential store sshelf still connects hosts that
use key files and explains why it cannot save passwords or keys.

## Specs

sshelf is built spec-first. Requirements, design, decisions and the task plan
live in [`specs/`](specs/README.md); the MVP plan is [`specs/tasks.md`](specs/tasks.md).
See [`CONTRIBUTING.md`](CONTRIBUTING.md) before changing anything.

## License

MIT
