# sshelf

A terminal UI for managing and connecting to your SSH hosts, inspired by Termius.

- Keep your hosts in one keyboard-driven list, with fuzzy search
- Passwords and private keys stored in your OS credential store (macOS
  Keychain / Linux Secret Service), never on disk, and no master password
- Its own SSH client: host-key trust on first use, a hard block on changed
  keys, keyboard-interactive and one-time codes, full-screen programs
- One-time import of `~/.ssh/config`

> **Status: MVP feature-complete, in acceptance.** Every MVP requirement is
> implemented and traced to tests (`go run ./scripts/tracecheck`); the manual
> acceptance run on macOS and Linux ([`specs/acceptance.md`](specs/acceptance.md))
> is pending sign-off. Groups, tags, ssh-agent, jump hosts, port forwarding
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
| Anywhere | `ctrl+c` quits (cancels a form) |

In a session the whole terminal belongs to the remote shell; `exit` returns
to the list with the exit status in the status line.

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
