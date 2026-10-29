# Contributing to sshelf

sshelf is built spec-first. Read [`specs/README.md`](specs/README.md) before
changing anything; [`specs/decisions.md`](specs/decisions.md) is binding and
wins any conflict.

## Working on a task

1. Pick a task from [`specs/tasks.md`](specs/tasks.md). Read every requirement
   it cites (FR/SEC/ARCH/NFR) and the relevant sections of
   `specs/architecture/design.md` and `specs/security/credential-storage.md`.
2. Implement exactly what the task and its requirements say. If a requirement
   is ambiguous, wrong, or conflicts with another, do not guess silently:
   pick the reading most consistent with `decisions.md` and D-007 (keep it
   simple), and record the question in the task's PR/commit description.
3. Write the acceptance tests listed in the task, named after requirement IDs
   (`TestFR_KH_002_ChangedKeyBlocks`).
4. Before committing, all of these must pass:

   ```sh
   gofmt -l .            # prints nothing
   go vet ./...
   go test -race ./...
   go build ./cmd/sshelf
   ```

   plus the linter configured in CI, if available locally.
5. Update each covered requirement's `Status:` from `Draft` to `Implemented`
   in the spec files. Change nothing else in the specs unless the task says so.
6. Commit with a message starting `T-NNN: <title>` and listing the requirement
   IDs covered.

## Rules

- Never write secrets to disk, logs, or test output.
- Tests must not touch the real OS keychain, `~/.ssh`, or the user's config
  directory. Use `secrets.MemStore`, temp dirs and the in-process test SSH
  server. Real-keychain tests sit behind the build tag defined in the design and
  run only in CI.
- Tests must not need network access beyond `127.0.0.1`.
- Respect the package dependency rules in `specs/architecture/design.md`; the
  import-rule test enforces them.
