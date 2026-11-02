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
   go run ./scripts/tracecheck   # every MVP, M2 and M3 ID Implemented and traced to a test
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
- Every goroutine starts through `term.SafeGo`. E2E binaries build with
  `-tags sshelf_faultinject` (in-memory credential store).
- Timing tests are named `TestNFR_PERF_*` and skip under `-race`; also run
  `go test -count=1 -run 'TestNFR_PERF_' ./...` and
  `~/go/bin/golangci-lint run ./...` (0 issues) before pushing.
- Golden files are `<name>.<w>x<h>.golden` at 80x24 and 120x40.
- If the spec is wrong or ambiguous, choose the reading most consistent with
  `specs/decisions.md` and D-007 (keep it simple), fix the spec text in the
  same commit, and mention it in the commit message.

## Landing work on main

Work on a branch, then:

1. `git fetch origin && git rebase origin/main`; re-run all checks.
2. `git push origin HEAD:main`; if rejected because main moved, repeat 1–2.
3. Wait for CI (`gh run watch <id> --exit-status`). If it fails because of
   your change, fix it and land again. If an unrelated test fails, rerun once;
   if it fails again, report it instead of chasing it. Never leave main red,
   never force-push.

Commit messages end with the project's attribution trailers when an AI agent
wrote the change.

## Releasing

Every release is backwards compatible (D-027): it reads everything an older
release wrote. Fields are only added, never renamed, retyped or removed;
`config.json` keeps fields it does not know. Breaking this needs a new
decision in `specs/decisions.md` first.

To cut release `vX.Y.Z` from a green `main`:

1. Write the release's compatibility fixture and commit it on `main` (land
   it as above):

   ```sh
   SSHELF_COMPAT_FIXTURE=vX.Y.Z go test -run TestD027_WriteFixture ./internal/compat
   git add testdata/compat/vX.Y.Z && git commit -m "release: vX.Y.Z compatibility fixture"
   ```

   Never edit an existing `testdata/compat/<tag>/` directory: the test loads
   every one, so each keeps proving that its release's files still load.
2. Run the manual checklist in `docs/release-checklist.md`.
3. Tag the commit and push the tag: `git tag vX.Y.Z && git push origin vX.Y.Z`.
   `.github/workflows/release.yml` refuses a tag without its fixture, builds
   `sshelf_<os>_<arch>` for darwin and linux on amd64 and arm64, writes
   `checksums.txt` and publishes the GitHub release.
