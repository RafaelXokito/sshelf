# sshelf

A terminal UI for managing and connecting to your SSH hosts, inspired by Termius.

- Keep all your VMs in one place, with labels, groups and tags
- Credentials stored in a local vault encrypted with a master password (Argon2id + AES-GCM)
- Connect to any host from a keyboard-driven list

> Status: early development.

## Build & run

```sh
go run ./cmd/sshelf
```

## Roadmap

- [x] Project skeleton and host list UI
- [ ] Encrypted vault (master password, Argon2id + AES-GCM)
- [ ] Add / edit / delete hosts from the UI
- [ ] Interactive SSH sessions (password and key auth)
- [ ] Groups, tags and search
- [ ] Jump hosts and port forwarding
- [ ] Vault export / import

## License

MIT
