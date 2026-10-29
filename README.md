# sshelf

A terminal UI for managing and connecting to your SSH hosts, inspired by Termius.

- Keep all your VMs in one place, with labels, groups and tags
- Passwords and keys stored in your OS keychain (macOS Keychain / Linux Secret Service) — no master password
- Connect to any host from a keyboard-driven list

> Status: early development.

## Build & run

```sh
go run ./cmd/sshelf
```

## Specs

sshelf is built spec-first. Requirements, design, decisions and the task plan
live in [`specs/`](specs/README.md); the MVP plan is [`specs/tasks.md`](specs/tasks.md).

## License

MIT
