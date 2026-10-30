# sshelf implementation plan

Status: Draft · Owner: delivery · Last updated: 2026-10-29

This is the ordered plan for the MVP: every `MVP` requirement in
`product/requirements.md` (`FR-*`), `security/requirements.md` (`SEC-*`),
`architecture/design.md` (`ARCH-*`) and [`nfr.md`](nfr.md) (`NFR-*`) is mapped
to at least one task in the [traceability matrix](#traceability-matrix).
[`decisions.md`](decisions.md) wins any conflict. The v1 milestones are
outlined at the end without task detail.

## How to read a task

- **One task is one PR** of about 0.5 to 2 days. The PR description lists the
  requirement IDs it covers (README workflow step 4).
- **Satisfies** lists the requirement IDs whose acceptance criteria the task's
  tests prove. "(partial: …)" means another task finishes that ID. The
  traceability matrix lists every task for each ID.
- **Tests** are the acceptance tests to write. They are named after the ID
  (`TestFR_KH_002_ChangedKeyBlocks`), so that the `scripts/tracecheck` gate in
  T-030 (NFR-QUAL-001) can find them. "Golden" means a golden file at
  80×24 and 120×40 rendered with the ASCII colour profile, stored as
  `internal/ui/testdata/<name>.<w>x<h>.golden` and checked by the shared
  `golden` helper (design §10). Implemented without `teatest`, because
  rendering the root model directly needs no extra dependency.
- **E2E** tests run the real binary in a PTY (`creack/pty`) with
  `XDG_CONFIG_HOME` pointing at a temp dir (`--config` is v1, FR-CLI-002) and
  `secrets.MemStore` injected through a test-only build tag, against the
  in-process server from T-001.
- Every task's **done when** also implies: CI green on Linux and macOS
  (NFR-PORT-001), lint clean (NFR-QUAL-002), `-race` and `goleak` clean
  (NFR-REL-001), archtest green (ARCH-PKG-001), and goldens updated.

## Milestones

| Milestone | Tasks | Outcome | Days |
|---|---|---|---|
| **M0 Walking skeleton** | T-000 … T-004 | Bubble Tea v2, CI, and a **demoable vertical slice**: pick a host from a hand-written `config.json` and get a shell on the in-process test server with a key file, then return to the list | 9.5 |
| **M1 Files and trust** | T-005 … T-008 | Durable `config.json`, file modes, TOFU, sshelf `known_hosts`, changed-key block | 6 |
| **M2 Secrets and auth** | T-009 … T-014 | OS credential store, stored keys and passwords, keyboard-interactive, prompts, error reporting | 9 |
| **M3 Managing hosts and identities** | T-015 … T-023 | Host form, full host list, Welcome, fuzzy search, identities, stored keys, hidden identities, no-store mode | 12 |
| **M4 Import** | T-024 … T-025 | `~/.ssh/config` import with report | 3.5 |
| **M5 Hardening and MVP gate** | T-026 … T-030 | CLI, crash safety, canary scan, NFR checks, traceability gate | 6 |
| **MVP total** | 31 tasks | | **46** |

---

## M0 Walking skeleton

### T-000 · Foundation: Bubble Tea v2, package layout, CI, domain types

- **Delivers:**
  - `go.mod` moves to `charm.land/bubbletea/v2` v2.1.1, `charm.land/bubbles/v2` v2.2.1 and `charm.land/lipgloss/v2` v2.0.6 (fallback bubbletea v2.1.0, ADR-007). The skeleton UI is ported (`View()` returns `tea.View`, `tea.KeyPressMsg`). No `github.com/charmbracelet/bubbletea` v1 remains.
  - The package tree from design §5.1, each with a `doc.go`. `internal/hosts` is replaced by `internal/domain` with the type sketches of design §7.1 and `domain.NewID()` (UUIDv7, `github.com/google/uuid`, ADR-008).
  - `internal/archtest` (ADR-009): `go list -deps` checks every forbidden edge in design §5.2 and bans `os/exec` and any clipboard package in sshelf packages, plus a grep check that `InsecureIgnoreHostKey` appears only in `_test.go` files.
  - GitHub Actions: lint job (`gofmt -l`, `go vet`, `golangci-lint` with staticcheck, errcheck, gosec, govet; `govulncheck ./...`), test job (`go test -race ./...` on `ubuntu-latest` and `macos-latest`), cross-compile job (4 targets, `CGO_ENABLED=0`, static check), and a coverage report that is not yet gated.
  - A `goleak.VerifyTestMain` helper used by packages that start goroutines.
  - **Manual exit check (ADR-007):** a throwaway program behind build tag `devcheck` (`go run -tags devcheck ./internal/session/devcheck`) runs a v2 `tea.Program` that `tea.Exec`s a stub `ExecCommand` which enters raw mode, prints received bytes as hex and returns on `q`.
- **Satisfies:** ARCH-PKG-001, ARCH-SESS-001 (partial: import ban), ARCH-SEC-002 (partial: import rule), SEC-MEM-008 (partial: AC1 `os/exec` ban), SEC-CLIP-005, ARCH-DATA-001 (partial: ID type), SEC-HOSTKEY-001 (partial: AC1 grep), SEC-SESSION-004 (partial: govulncheck), NFR-BUILD-001, NFR-PORT-001, NFR-QUAL-002, NFR-QUAL-003 (partial: report only), NFR-REL-001
- **Depends on:** none
- **Tests:** `TestARCH_PKG_001_ImportRules` (table over §5.2) and `TestARCH_PKG_001_ForbiddenImportFails` (a `testdata` module with a deliberate forbidden edge), `TestARCH_SESS_001_NoOsExec`, `TestSEC_MEM_008_NoOsExecImport`, `TestSEC_CLIP_005_NoClipboardPackage`, `TestARCH_SEC_002_SSHClientDoesNotImportSecrets`, `TestSEC_HOSTKEY_001_NoInsecureIgnoreHostKey`, `TestARCH_DATA_001_NewIDIsUUIDv7`. CI steps for NFR-BUILD-001 and NFR-QUAL-002.
- **Done when:** CI is green on both OSes; the cross-compile job produces 4 `CGO_ENABLED=0` binaries; the manual check is recorded in the PR for one macOS terminal (iTerm2 or Terminal.app) and one Linux terminal (GNOME Terminal or xterm): the stub receives raw bytes (Ctrl+C arrives as `03`), the alt screen is restored on return, the TUI redraws, the **first** keystroke after return reaches the TUI, and `stty -a` after quitting equals the value before launch. If v2.1.1 fails, the fallback is pinned and the reason noted.
- **Estimate:** 2 d

### T-001 · In-process SSH test server

- **Delivers:** `internal/testutil/sshtest` on `golang.org/x/crypto/ssh` (or `charmbracelet/ssh` if it pulls in no Bubble Tea v1): host keys per algorithm, password, public-key and scripted keyboard-interactive auth (per-prompt echo flags), session channels with `pty-req`, `window-change`, `env`, `shell` (a scripted shell with `echo`, `size` and `exit N`) and `exec`; records the client version, every auth attempt in order and every request; can drop the connection on demand. A dev runner `internal/testutil/sshtest/cmd/sshtestd` listens on loopback and prints its address, a `known_hosts` line and a generated demo key path, for demos.
- **Satisfies:** ARCH-TEST-001
- **Depends on:** T-000
- **Tests:** `TestARCH_TEST_001_AuthMethods`, `TestARCH_TEST_001_RecordsRequestsInOrder`, `TestARCH_TEST_001_WindowChangeRecorded`, `TestARCH_TEST_001_ExitStatus`, `TestARCH_TEST_001_LoopbackOnly`.
- **Done when:** the suite runs with `-race` on both OSes with no real keychain and no network beyond loopback.
- **Estimate:** 1.5 d

### T-002 · SSH client: dial, host-key verification, key-file auth

- **Delivers:** `sshclient.Connect(ctx, Target, CredentialSource, Prompter)`. `net.Dialer.DialContext` with `ConnectTimeout`, closing the connection on context cancel. `HostKeyCallback` from `knownhosts.New` over `<configDir>/known_hosts`, `~/.ssh/known_hosts` and `/etc/ssh/ssh_known_hosts` (missing files skipped, never written). An unknown key goes to `Prompter.HostKey`; until T-007 the default prompter rejects. `CredentialSource` and `Prompter` interfaces. In `app`, a key-file credential source: `~` expansion, the 0600 check, and `ssh.ParsePrivateKey`. Default `x/crypto/ssh` algorithms only.
- **Satisfies:** SEC-HOSTKEY-001, SEC-FILE-009, ARCH-SESS-001, ARCH-SEC-002, SEC-SESSION-004, FR-IDENT-003 (partial: key file), ARCH-CONN-005 (partial: context plumbing)
- **Depends on:** T-001
- **Tests:** `TestSEC_HOSTKEY_001_UnknownKeyCancelBeforeAuth`, `TestSEC_FILE_009_UnprotectedKeyRefused`, `TestSEC_FILE_009_Mode0600KeyUsed`, `TestFR_IDENT_003_TildeExpanded`, `TestFR_IDENT_003_KeyFileNotFound`, `TestSEC_SESSION_004_NoLegacyAlgorithms`, `TestARCH_SESS_001_ConnectWithoutOpenSSH` (empty `PATH`), `TestARCH_SEC_002_CredentialsOnlyViaInterface`.
- **Done when:** a key-file connection to sshtest with a pre-trusted key succeeds, and an unknown key fails with zero auth attempts recorded.
- **Estimate:** 2 d

### T-003 · Session: terminal handover

- **Delivers:** `session.Session` satisfying `tea.ExecCommand` structurally (design §8.4): a `Terminal` interface (raw mode, size, resize events) with a real implementation (`golang.org/x/term`, SIGWINCH) and a fake; `pty-req` with `$TERM` and the window size; stdin/stdout/stderr pumps; `cancelreader` on stdin; resize debounce of 30 ms; teardown that restores termios and writes the §8.6 reset sequence on every path; `term.SafeGo` (recovering goroutine runner). Returns the exit status or the disconnect cause.
- **Satisfies:** ARCH-SESS-003, ARCH-SESS-004, ARCH-SESS-005 (partial: remote exit and network error), FR-CONN-001 (partial: PTY), FR-CONN-004
- **Depends on:** T-001
- **Tests:** `TestARCH_SESS_003_BytesPassThroughUnchanged` (256 random bytes each way), `TestARCH_SESS_004_WindowChangeWithin100ms`, `TestARCH_SESS_004_BurstMaxTwoRequests`, `TestFR_CONN_004_ResizeReachesRemote`, `TestFR_CONN_001_PtyUsesTermAndSize`, `TestARCH_SESS_005_RestoreOnRemoteExit` (PTY, `stty -a` compare), `TestARCH_SESS_005_RestoreOnNetworkError` (PTY).
- **Done when:** the PTY tests pass on both OSes, and the stdin reader provably stops before `Run()` returns.
- **Estimate:** 2 d

### T-004 · Vertical slice: pick a host and connect with a key file  **(first demo)**

- **Delivers:** `internal/paths` config-dir resolution (ADR-005, no file creation yet); read-only `store.Load` of `config.json` into `domain.Data`; a minimal `domain.Resolve` (hostname, port, user, key path); `app.Service` with `ConnectCmd`; the root model with screen and modal stack (design §9.2); a plain host list (label and `user@hostname:port`, ↑/↓/`j`/`k`, Enter, `q`); a Connecting modal; `tea.Exec(session, …)`; `SessionEndedMsg` back to the list with the same host selected and "web-prod: exited (3)" or "web-prod: connection lost: <reason>" in the status bar. `cmd/sshelf` wiring.
- **Satisfies:** FR-CONN-001, FR-CONN-005, ARCH-SESS-002 (partial: no exec on auth failure), ARCH-UI-001 (partial: root model), ARCH-UI-002, FR-NAV-001 (partial: list line)
- **Depends on:** T-002, T-003
- **Tests:** `TestFR_CONN_005_ExitStatusShown`, `TestFR_CONN_005_ConnectionLostShown`, `TestFR_CONN_005_SameHostSelected`, `TestFR_CONN_005_TerminalRestored` (E2E), `TestARCH_SESS_002_AuthFailureNeverExecs`, `TestARCH_UI_002_NoIOInUpdate` (a fake `app.Service` that panics when called on the `Update` goroutine), `TestFR_CONN_001_E2EKeyFileSlice` (E2E: connect, run `exit 3`, assert the status text and restored `stty`).
- **Done when:** the demo works by hand: start `sshtestd`, paste its `known_hosts` line into `<configDir>/known_hosts`, write a `config.json` with one host and a key-file identity, run `sshelf`, press Enter, get the scripted shell, type `exit 3`, and see the list again with "exited (3)". The steps are in the PR description.
- **Estimate:** 2 d

---

## M1 Files and trust

### T-005 · Paths, directory creation and file modes

- **Delivers:** the full ADR-005 table in `internal/paths` (Linux, Linux with XDG, macOS, macOS with explicit XDG, an override directory for tests and v1 `--config`) plus the state dir; directory creation 0700 and file writes 0600 independent of the umask; a startup permission check that restores 0600/0700 and shows a one-line notice once; creation of the config dir on first launch.
- **Satisfies:** ARCH-PKG-002, FR-CFG-001 (partial: directory and modes), SEC-FILE-001, SEC-FILE-002
- **Depends on:** T-004
- **Tests:** `TestARCH_PKG_002_PathTable`, `TestARCH_PKG_002_PermissionsAfterFirstSave`, `TestSEC_FILE_001_ModesUnderUmask000`, `TestSEC_FILE_002_RestoresLooseModes`, `TestSEC_FILE_002_NoticeShownOnce`, `TestFR_CFG_001_FirstLaunchCreatesDir`.
- **Done when:** no other package builds a config or state path itself (archtest grep for `UserConfigDir`/`XDG_` outside `paths`).
- **Estimate:** 1 d

### T-006 · Store: atomic save, broken-file handling

- **Delivers:** `store.Save` per credential-storage §3.4 (temp file `O_EXCL` 0600, `Sync`, `Rename`, directory `fsync`, symlinks resolved) behind an `fs` interface with fault injection; `ErrCorrupt` with line and column, `ErrNewerVersion`; tolerant decoding with a warning for unknown setting keys; a 16 MiB read cap; the LoadError screen (path, line, problem; `q` quits; file untouched); "Saved" shown only after `Save` returns.
- **Satisfies:** FR-CFG-002, FR-CFG-003, SEC-FILE-003, SEC-CRED-001 (partial: AC2)
- **Depends on:** T-005
- **Tests:** `TestSEC_FILE_003_FaultAtEachStep`, `TestSEC_FILE_003_SymlinkPreserved`, `TestFR_CFG_002_SigkillAfterSaved` (subprocess), `TestFR_CFG_002_NoTruncatedFile`, `TestFR_CFG_003_SyntaxErrorScreen` (golden), `TestFR_CFG_003_FileByteForByteUnchanged`, `TestFR_CFG_003_UnknownSettingWarns`, `TestSEC_CRED_001_SchemaHasNoSecretFields` (reflection), `FuzzLoadConfig`.
- **Done when:** a round-trip golden of the §3.3 schema passes and the fuzz target runs 30 s in CI without a crash.
- **Estimate:** 1.5 d

### T-007 · TOFU prompt and sshelf `known_hosts`

- **Delivers:** the real `uiPrompter` bridge (design §8.3); the TOFU modal (host, port, key type, SHA256 fingerprint; `y` trust and save; Enter and Esc cancel); appending a `knownhosts.Line` to `<configDir>/known_hosts` with the T-006 atomic writer; `ErrHostKeyRejected`.
- **Satisfies:** FR-KH-001, FR-KH-004, SEC-HOSTKEY-002, SEC-HOSTKEY-004, ARCH-CONN-003
- **Depends on:** T-006
- **Tests:** `TestFR_KH_001_NoAuthBeforeChoice`, `TestFR_KH_001_EnterAndEscCancel`, `TestFR_KH_001_NoTrustOnceOption`, `TestSEC_HOSTKEY_004_FingerprintMatchesSSH`, `TestFR_KH_004_TrustSurvivesRestart`, `TestFR_KH_004_SSHKeygenFindsEntry` (skipped when `ssh-keygen` is absent), `TestSEC_HOSTKEY_002_OneParseableLine`, `TestARCH_CONN_003_TofuGolden`, `TestARCH_CONN_003_OpenSSHFileUntouched`.
- **Done when:** the slice from T-004 works against an untrusted `sshtestd` by accepting the prompt, and the second connection shows no prompt.
- **Estimate:** 2 d

### T-008 · Changed and revoked keys, OpenSSH trust, algorithm pinning

- **Delivers:** `ErrHostKeyMismatch` with the stale entry's file and line (from `knownhosts.KeyError`); the block screen with both fingerprints and removal instructions (`delete line N of <path>`, `ssh-keygen -R <host> -f <path>`) and no override; `@revoked` → "host key revoked" with no trust action; `HostKeyAlgorithms` pinned to the known key types; fixtures for hashed, `[host]:port` and comma-pattern entries.
- **Satisfies:** FR-KH-002, FR-KH-003, SEC-HOSTKEY-003, SEC-HOSTKEY-005, SEC-HOSTKEY-007, ARCH-CONN-004
- **Depends on:** T-007
- **Tests:** `TestFR_KH_002_ChangedKeyBlocks`, `TestFR_KH_002_ShowsPathAndLine`, `TestSEC_HOSTKEY_005_ZeroAuthAttempts`, `TestSEC_HOSTKEY_005_DeletedLineThenTofu`, `TestFR_KH_003_HashedEntryTrusted`, `TestFR_KH_003_PortAndPatternEntries`, `TestFR_KH_003_ChecksumUnchanged`, `TestFR_KH_003_RevokedRejected`, `TestSEC_HOSTKEY_003_RevokedBeatsSshelfTrust`, `TestSEC_HOSTKEY_007_PinnedToKnownAlgorithms`, `TestSEC_HOSTKEY_007_RSAOnlyServerFailsWithoutTofu`, `TestARCH_CONN_004_BlockGolden`.
- **Done when:** all host-key fixtures pass and `~/.ssh/known_hosts` fixtures are byte-identical after every test.
- **Estimate:** 1.5 d

---

## M2 Secrets and auth

### T-009 · Secrets core: `Secret`, `Ref`, `MemStore`

- **Delivers:** `internal/secrets` per credential-storage §5 except the OS adapter: `Secret` with `Destroy` and redacting `String`/`GoString`/`Format`/`MarshalJSON`/`LogValue`; `Ref`, `Account`, `ParseRef`; the 4 sentinel errors; `MaxSecretSize` enforced in `Set`; `MemStore` with `FailNext`, `Calls` and `Keys`.
- **Satisfies:** SEC-MEM-001, SEC-LOG-002, SEC-CRED-002 (partial: AC1), SEC-CRED-003 (partial: limit)
- **Depends on:** T-000
- **Tests:** `TestSEC_MEM_001_DestroyZeroesBacking`, `TestSEC_MEM_001_NoStringSecretAPI` (`go/types` scan of exported functions), `TestSEC_LOG_002_RedactedEveryFormatter`, `TestSEC_CRED_002_ItemNames`, `TestSEC_CRED_003_SizeBoundary`, `FuzzParseRef`.
- **Done when:** `secrets` has 100 % statement coverage outside the (absent) OS adapter.
- **Estimate:** 1 d

### T-010 · OS credential-store adapter and keyring CI

- **Delivers:** `NewOSStore` over `zalando/go-keyring` (exact version pinned), mapping errors to the sentinels, a goroutine with a 30 s context timeout per call, and `Available`; the startup probe wired asynchronously in `main` (it must not delay the first frame, NFR-PERF-001); CI jobs with build tag `integration_keyring` on a throwaway macOS keychain and under `dbus-run-session` with `gnome-keyring-daemon` on Linux, plus a Linux run without a session bus; a dependency-review note in the PR confirming go-keyring's `security -i` stdin path; `docs/release-checklist.md` started with the SEC-CRED-009 upgrade check.
- **Satisfies:** SEC-CRED-009 (partial: backend), SEC-MEM-008 (partial: AC2), SEC-CRED-003 (partial: OS boundary), SEC-CRED-005 (partial: adapter), FR-CRED-001 (partial: store)
- **Depends on:** T-009
- **Tests:** `TestSEC_CRED_003_OSStoreBoundary2800` (`integration_keyring`), `TestSEC_CRED_005_NoSessionBusIsUnavailable` (`integration_keyring`, Linux), `TestSEC_CRED_009_BackendIsGoKeyring` (archtest: no cgo keychain import), `TestOSStore_RoundTrip`, `TestOSStore_ErrorMapping` (injected keyring funcs), `TestOSStore_ContextTimeout`.
- **Done when:** both keyring jobs pass in CI and never touch a login keychain (service `sshelf-test-<rand>`).
- **Estimate:** 1.5 d

### T-011 · Debug logging

- **Delivers:** `log/slog` setup in `main`: off by default; when enabled, a 0600 file in the state dir (ADR-005) rotated at 5 MiB (one `.1` backup). Logging is enabled only by `SSHELF_DEBUG=1` (SEC-LOG-003).
- **Satisfies:** SEC-LOG-003
- **Depends on:** T-005, T-009
- **Tests:** `TestSEC_LOG_003_NoFileByDefault`, `TestSEC_LOG_003_EnabledBySSHELF_DEBUG`, `TestSEC_LOG_003_Mode0600`, `TestSEC_LOG_003_RotatesAt5MiB`.
- **Done when:** connect stages and auth method attempts are logged (needed by FR-CONN-002 and the T-028 canary harness).
- **Estimate:** 1 d

### T-012 · Authenticate with stored credentials

- **Delivers:** full `domain.Resolve` for MVP fields (username: host → identity → `$USER`, FR-IDENT-002; port 22; connect timeout 10 s); `app`'s `CredentialSource` on `secrets.Store` with lazy `Get` inside the auth callbacks after host-key verification; stored-key and stored-passphrase parsing; password callback; method order key → password/keyboard-interactive; methods the server does not offer skipped; `Destroy` and signer drop after auth.
- **Satisfies:** FR-CONN-002, FR-IDENT-002, ARCH-CONN-001, SEC-MEM-009, SEC-SESSION-001, FR-CRED-001 (partial: connect after restart)
- **Depends on:** T-004, T-010
- **Tests:** `TestFR_CONN_002_KeyWithoutPrompt`, `TestFR_CONN_002_KeyBeforePassword`, `TestFR_CONN_002_SkipsUnofferedMethods`, `TestFR_IDENT_002_UsernameOverride`, `TestARCH_CONN_001_AuthMethodTable`, `TestSEC_MEM_009_NoGetAtStartup`, `TestSEC_MEM_009_OneGetPerFieldPerConnect`, `TestSEC_MEM_009_BuffersZeroedAfterAuth`, `TestSEC_SESSION_001_NoGetOnRejectedHostKey`, `TestSEC_SESSION_001_PasswordNotSentToOtherHost`, `TestFR_CRED_001_ConnectsAfterRestartWithoutPrompt`.
- **Done when:** a password host and a stored-key host connect from a fresh process with `MemStore` preloaded.
- **Estimate:** 2 d

### T-013 · Prompts: keyboard-interactive, missing secrets, passphrases; sanitising

- **Delivers:** the keyboard-interactive bridge (auto-answer a single no-echo `/password/i` prompt with the stored password; every other prompt goes to the Prompt modal labelled "Prompt from <host>", echoing only when the server allows); prompts for a missing password or passphrase (`user@host` and "password" or "passphrase for <key>"); `ErrNotFound` shown as "not set on this device" and the typed value used once; Esc → `ErrAuthCancelled` and back to the list; 3 failures → "authentication failed"; `Sanitize` (C0 except `\t\n`, C1, ANSI) applied to all server-supplied text.
- **Satisfies:** FR-CONN-003, FR-CONN-008, SEC-SESSION-002, SEC-SESSION-003, ARCH-CONN-002, SEC-CRED-007, ARCH-SESS-002
- **Depends on:** T-007, T-012
- **Tests:** `TestFR_CONN_003_PromptShowsUserHostAndKind`, `TestFR_CONN_003_EscReturnsToList`, `TestFR_CONN_003_ThreeFailures`, `TestFR_CONN_008_KbdIntPasswordAutoAnswered`, `TestFR_CONN_008_OTPShownPasswordFilled`, `TestFR_CONN_008_EchoOnlyWhenServerSets`, `TestFR_CONN_008_NoStoredPasswordPrompts`, `TestSEC_SESSION_002_TwoPromptsNoAutoAnswer`, `TestSEC_SESSION_002_OSC52Escaped`, `FuzzSEC_SESSION_003_Sanitize`, `TestSEC_CRED_007_MissingItemPromptsOnce`, `TestARCH_CONN_002_CancelIsErrAuthCancelled`, `TestARCH_SESS_002_KbdIntModalGolden`.
- **Done when:** a password-plus-OTP sshtest scenario passes end to end and `FuzzSEC_SESSION_003_Sanitize` runs 30 s in CI.
- **Estimate:** 2 d

### T-014 · Connection failures, cancel, store denied

- **Delivers:** error classification (name resolution failed, connection refused, timed out, host key rejected, authentication failed) with one message each; the 10 s timeout; Connecting modal stages ("dialling → verifying → authenticating"); Esc cancels within 200 ms; `ErrDenied`/`ErrUnavailable` at connect abort with a message naming the store, no retry and no cached failure.
- **Satisfies:** FR-CONN-006, ARCH-CONN-005, SEC-CRED-006
- **Depends on:** T-012
- **Tests:** `TestFR_CONN_006_NameResolutionFailed`, `TestFR_CONN_006_ConnectionRefused`, `TestFR_CONN_006_TimedOut` (injected short timeout, black-hole listener), `TestFR_CONN_006_HostKeyRejected`, `TestFR_CONN_006_AuthFailed`, `TestARCH_CONN_005_CancelDuringDial`, `TestARCH_CONN_005_CancelDuringAuth`, `TestSEC_CRED_006_DeniedNoRetryNoCache`.
- **Done when:** each cause has its own golden error toast and `goleak` passes after every cancel.
- **Estimate:** 1.5 d

---

## M3 Managing hosts and identities

### T-015 · Host form: add, edit, validate

- **Delivers:** a shared form component (`tab`/`shift+tab`, `ctrl+s`, Esc with unsaved-change confirm, Ctrl+C cancels the form); the host form (label, hostname, port, username, identity picker); FR-HOST-003 validation shown next to the field; new hosts get a UUIDv7; save through `store`, then select the host.
- **Satisfies:** FR-HOST-001, FR-HOST-003, ARCH-DATA-001 (partial: IDs on create)
- **Depends on:** T-006, T-012
- **Tests:** `TestFR_HOST_001_AddSelectsHost`, `TestFR_HOST_001_PortChangeShownAndUsed`, `TestFR_HOST_001_EscDiscards`, `TestFR_HOST_001_SurvivesRestart`, `TestFR_HOST_003_LabelRules`, `TestFR_HOST_003_PortRules`, `TestFR_HOST_003_HostnameRules`, `TestARCH_DATA_001_NewHostHasUUIDv7`, form goldens.
- **Done when:** hosts can be created and edited without hand-editing `config.json`.
- **Estimate:** 2 d

### T-016 · Host list complete: keymap, footer, delete

- **Delivers:** `ui/keys` as the single keymap with help text, used to generate the footer; the full list rendering (sorted by label, port 22 omitted, `…` truncation, selection glyph); all FR-NAV-003 bindings (`i` and `I` are wired to their screens in T-019 and T-025); delete with a confirm modal that names the host and defaults to "No"; the shared golden helper that enforces line width and footer position.
- **Satisfies:** FR-NAV-001, FR-NAV-003, FR-NAV-004, FR-HOST-002 (partial: shared identity), ARCH-UI-004, NFR-UX-001 (partial: keymap), NFR-UX-002, NFR-UX-003
- **Depends on:** T-015
- **Tests:** `TestFR_NAV_001_SortedPort22OmittedTruncated`, `TestFR_NAV_003_MoveKeys`, `TestFR_NAV_003_FirstLast`, `TestFR_NAV_003_AddEditDelete`, `TestFR_NAV_003_SearchFieldTakesLetters`, `TestFR_NAV_003_CtrlCInFormCancels`, `TestFR_NAV_003_EscBackQOnlyFromList`, `TestFR_NAV_004_FooterPerView`, `TestFR_HOST_002_ConfirmNamesHostDefaultsNo`, `TestFR_HOST_002_SharedIdentityKept`, `TestARCH_UI_004_UniqueKeysWithHelp`, `TestNFR_UX_001_EveryActionBound`, `TestNFR_UX_002_LinesFitAndFooterVisible`, `TestNFR_UX_003_AsciiProfile`.
- **Done when:** every screen built so far passes the golden helper at 80×24 and 120×40.
- **Estimate:** 1.5 d

### T-017 · Welcome screen

- **Delivers:** the Welcome screen whenever `config.json` has no hosts, with `I` import, `a` add host and `q` quit; transitions to and from the host list.
- **Satisfies:** FR-NAV-013, FR-CFG-001 (partial: first launch)
- **Depends on:** T-016
- **Tests:** `TestFR_NAV_013_FirstLaunchShowsWelcome`, `TestFR_NAV_013_Keys`, `TestFR_NAV_013_LastHostDeletedShowsWelcome`, `TestFR_CFG_001_StartsWithoutConfigDir` (E2E).
- **Done when:** golden of the Welcome screen at both sizes.
- **Estimate:** 0.5 d

### T-018 · Fuzzy search

- **Delivers:** `internal/search` on `sahilm/fuzzy` over label, hostname and user, ranked by score; `/` focuses the search field, letters type into it, Esc clears and returns to the list, Enter connects to the selected (top) result.
- **Satisfies:** FR-NAV-002, NFR-PERF-002
- **Depends on:** T-016
- **Tests:** `TestFR_NAV_002_Matches` (`wbprd`, `10.0.1`, case-insensitive), `TestFR_NAV_002_EnterConnectsTopMatch`, `TestFR_NAV_002_EscClearsQuery`, `BenchmarkNFR_PERF_002_Filter5k`, `TestNFR_PERF_002_Filter5k`.
- **Done when:** the benchmark stays under 16 ms per operation on the CI runner.
- **Estimate:** 1 d

### T-019 · Identities view and identity form

- **Delivers:** the identities screen (`i`; hidden identities excluded); the identity form (unique name, username, password, key path, key passphrase; secret fields non-echoing and shown only as "set", "not set" or "not set on this device"); the create flow of credential-storage §7 (size check, `Set` per field, `has_*` flags, rollback of written items on partial failure, then config write); the edit flow (changed field → `Set`, unchanged never read back); changes apply to linked hosts on the next connect.
- **Satisfies:** FR-IDENT-001 (partial: create and edit), FR-IDENT-004 (partial: UI), SEC-LOG-005, FR-CRED-001, SEC-CRED-002 (partial: AC2), FR-NAV-003 (partial: `i`)
- **Depends on:** T-015, T-012
- **Tests:** `TestFR_IDENT_001_NameUniqueCaseInsensitive`, `TestFR_IDENT_001_PasswordChangeAppliesToLinkedHosts` (3 hosts, sshtest), `TestFR_IDENT_004_FormShowsSetNotSet`, `TestSEC_LOG_005_GoldenHasNoCanaryOrBullets`, `TestFR_CRED_001_NoMasterPasswordScreen`, `TestFR_CRED_001_ConfigHasNoSecret`, `TestSEC_CRED_002_RenameKeepsKeys`, `TestFR_IDENT_001_PartialSetFailureRollsBack`, `TestFR_NAV_003_IOpensIdentities`.
- **Done when:** an identity with all three secrets can be created, edited and used to connect.
- **Estimate:** 2 d

### T-020 · Identity delete and clearing secrets

- **Delivers:** delete with a confirmation stating the number of linked hosts; unlink hosts; write the config first, then `Delete` the three refs; a warning naming `sshelf` / `<uuid>/<field>` for any failed delete, never recorded in `config.json`; clearing a secret field deletes its item with the same warning.
- **Satisfies:** FR-IDENT-001, SEC-CRED-004, ARCH-DATA-004, ARCH-DATA-001
- **Depends on:** T-019
- **Tests:** `TestFR_IDENT_001_DeleteStatesLinkedCount`, `TestSEC_CRED_004_DeleteLeavesNoItems`, `TestSEC_CRED_004_DeleteFailureWarns`, `TestSEC_CRED_004_ClearedFieldDeleted`, `TestARCH_DATA_004_NoRefsAfterDelete`, `TestARCH_DATA_001_DeleteClearsReferences`, `TestARCH_DATA_001_RenameChangesNoHost`.
- **Done when:** the delete confirmation golden passes and no store item survives a successful delete.
- **Estimate:** 1 d

### T-021 · Stored keys: paste or import into the credential store

- **Delivers:** a paste field and an import-from-file action ("Store key in keychain", ADR-010) in the identity form; validation for Ed25519, ECDSA and RSA in OpenSSH, PEM and PKCS#8 (encrypted keys accepted, public part read for the fingerprint); key type and SHA256 fingerprint shown; keys over 2800 bytes rejected with the "reference the key file" hint; the original file left untouched (0600 check on import).
- **Satisfies:** FR-IDENT-003, SEC-CRED-003
- **Depends on:** T-019
- **Tests:** `TestFR_IDENT_003_FormatsAccepted` (3 types × 3 formats), `TestFR_IDENT_003_InvalidPasteRejected`, `TestFR_IDENT_003_ImportThenDeleteFileStillConnects`, `TestFR_IDENT_003_ShowsTypeAndFingerprint`, `TestFR_IDENT_003_Over2800Rejected`, `TestSEC_CRED_003_HintMentionsKeyFile`.
- **Done when:** an RSA-4096 key is refused with the hint and an Ed25519 key connects after its file is deleted.
- **Estimate:** 1.5 d

### T-022 · Credentials in the host form (hidden identity)

- **Delivers:** username plus password or key (pasted or file path) directly in the host form, saved as a hidden identity linked only to that host; editing updates it; choosing a shared identity deletes the hidden one and its secrets; deleting the host deletes its hidden identity and secrets.
- **Satisfies:** FR-HOST-010, FR-HOST-002
- **Depends on:** T-020, T-021
- **Tests:** `TestFR_HOST_010_FirstTimeUserConnectsWithPassword`, `TestFR_HOST_010_HiddenNotListed`, `TestFR_HOST_010_EditUpdatesHidden`, `TestFR_HOST_010_SwitchToSharedDeletesHidden`, `TestFR_HOST_002_HiddenIdentityDeletedWithSecrets`.
- **Done when:** a first-time user can add a password host and connect without opening the identities view.
- **Estimate:** 1.5 d

### T-023 · No credential store available

- **Delivers:** the one-line banner on the host list and identity editor; secret fields disabled with the D-101 message naming the missing store (Secret Service on Linux, Keychain on macOS); key-file identities still saved and connected; hosts whose stored secret cannot be read fall back to the T-013 prompt; nothing written anywhere.
- **Satisfies:** FR-CRED-002, SEC-CRED-005, ARCH-SEC-006
- **Depends on:** T-022
- **Tests:** `TestFR_CRED_002_SaveRefusedWithMessage`, `TestFR_CRED_002_KeyFileHostConnects`, `TestFR_CRED_002_UnreadableSecretFallsBackToPrompt`, `TestSEC_CRED_005_NoCanaryInAnyFile`, `TestARCH_SEC_006_DisabledFieldGolden`.
- **Done when:** with `MemStore` returning `ErrUnavailable` for `Available`, every M3 screen renders and the key-file slice from T-004 still works.
- **Estimate:** 1 d

---

## M4 Import

### T-024 · `~/.ssh/config` parser and import plan

- **Delivers:** `internal/sshconfig`, an in-house stdlib parser (implemented as its own parser instead of `kevinburke/ssh_config` because the library resolves `Include` against the real home directory, which blocks fixture tests, and evaluates `Match`; see ADR-010): concrete aliases (`Host a b` → 2 hosts), `Include` relative to `~/.ssh` with globs, wildcard blocks with first-value-wins, `%h`/`%p`/`%r`/`~` tokens; an import plan of hosts, identities deduplicated by user plus key files (named `user (keyfile)`), and unsupported directives per host. Nothing is ever executed.
- **Satisfies:** FR-IMPORT-002, FR-IMPORT-001 (partial: mapping)
- **Depends on:** T-000
- **Tests:** `TestFR_IMPORT_002_IncludeRelativeToSSHDir`, `TestFR_IMPORT_002_MatchesSSHG` (expected values generated once with `ssh -G` against the fixtures with `HOME` set to the fixture dir and committed; re-checked live when `ssh` is on `PATH`), `TestFR_IMPORT_001_MapsFields`, `TestFR_IMPORT_001_AliasWithoutHostName`, `TestFR_IMPORT_001_MultipleAliases`, `TestFR_IMPORT_001_SharedIdentityDedup`, `FuzzSSHConfigMapper`.
- **Done when:** the fixture set matches `ssh -G` field for field.
- **Estimate:** 2 d

### T-025 · Import apply and report

- **Delivers:** `I` from the host list and the Welcome screen; applying the plan (no preview, which is v1 FR-IMPORT-004), skipping aliases whose label exists (case-insensitive), referencing key files by path, sanitising imported strings; the report screen with created counts, skipped aliases and per-host "not imported" directives.
- **Satisfies:** FR-IMPORT-001, FR-IMPORT-003, FR-NAV-003 (partial: `I`)
- **Depends on:** T-024, T-017, T-013
- **Tests:** `TestFR_IMPORT_001_KeysReferencedNotCopied` (`MemStore` sees zero `Set`), `TestFR_IMPORT_003_SecondImportSkipsAll`, `TestFR_IMPORT_003_UnsupportedListed`, `TestFR_IMPORT_003_ReportGolden`, `TestFR_NAV_003_CapitalIStartsImport`.
- **Done when:** importing the fixture `~/.ssh/config` from the Welcome screen lands on a populated host list.
- **Estimate:** 1.5 d

---

## M5 Hardening and MVP gate

### T-026 · Command line and process hardening

- **Delivers:** stdlib `flag` (ADR-006): `--help`, `--version` (version, commit and date from `-ldflags`, falling back to `debug.ReadBuildInfo`), unknown argument → usage and exit 2, non-terminal stdin/stdout → "sshelf needs an interactive terminal" and exit 1; an unexported hardening function in `cmd/sshelf`, called first thing in `main` (`RLIMIT_CORE` 0; `PR_SET_DUMPABLE` 0 on Linux; SEC-MEM-003).
- **Satisfies:** FR-CLI-001, SEC-MEM-003
- **Depends on:** T-004
- **Tests:** `TestFR_CLI_001_Version`, `TestFR_CLI_001_Help`, `TestFR_CLI_001_UnknownArgExit2`, `TestFR_CLI_001_NotATerminalExit1`, `TestSEC_MEM_003_CoreLimitZero`, `TestSEC_MEM_003_NotDumpableOnLinux`.
- **Done when:** the release build stamps version, commit and date.
- **Estimate:** 1 d

### T-027 · Panic handling and crash reports

- **Delivers:** `main`'s `recover` (restore the startup `term.State`, write the §8.6 reset sequence, write a 0600 crash report without secrets to the state dir, exit 70); `term.SafeGo` used for every goroutine sshelf starts (archtest rule against bare `go` statements outside `term`); a panic in a session goroutine ends the session with `ErrInternal` and the app keeps running; fault injection behind build tag `sshelf_faultinject`.
- **Satisfies:** ARCH-SEC-004, ARCH-SESS-005
- **Depends on:** T-026, T-011
- **Tests:** `TestARCH_SEC_004_PanicInSessionGoroutine` (PTY), `TestARCH_SEC_004_PanicInUpdateExit70` (PTY, cooked mode afterwards), `TestARCH_SEC_004_CrashReportHasNoCanary`, `TestARCH_SEC_004_NoBareGoStatements`, `TestARCH_SESS_005_RestoreOnPanic`.
- **Done when:** both PTY panic tests pass on Linux and macOS.
- **Estimate:** 1.5 d

### T-028 · Canary harness

- **Delivers:** one end-to-end harness with `MemStore`, sshtest and debug logging on, running: create an identity, connect (success, wrong password, host-key mismatch), store denied, store unavailable. It scans the config dir, state dir, temp dir, stdout, stderr, the log, the crash report and `os.Environ()` for the canary password, key and passphrase. SEC-MEM-008 and SEC-CLIP-005 are enforced by the T-000 import-rule test, not by this harness.
- **Satisfies:** SEC-CRED-001, SEC-LOG-001, ARCH-SEC-001, FR-IDENT-004
- **Depends on:** T-014, T-023, T-027
- **Tests:** `TestSEC_CRED_001_CanaryNotInAnyFile`, `TestSEC_LOG_001_CanaryHarness`, `TestARCH_SEC_001_CanaryScan`, `TestFR_IDENT_004_DebugLogHasNoPassword`.
- **Done when:** zero canary hits across all flows on both OSes.
- **Estimate:** 1 d

### T-029 · NFR checks: performance, terminals, size, privacy

- **Delivers:** the CI-enforced checks for the remaining MVP NFRs: startup E2E with a 1,000-host fixture; connect overhead; the `TERM` matrix E2E; the mouse-tracking output scan; the per-target binary size step; the `net/http` and dialer archtest.
- **Satisfies:** NFR-PERF-001, NFR-PERF-003, NFR-PORT-002, NFR-UX-001, NFR-SIZE-002, NFR-PRIV-001
- **Depends on:** T-018, T-026
- **Tests:** `TestNFR_PERF_001_Startup1k`, `TestNFR_PERF_003_ConnectOverhead`, `TestNFR_PORT_002_TermMatrix`, `TestNFR_UX_001_NoMouseTracking`, `TestNFR_PRIV_001_NoHTTPClient`, CI step `NFR-SIZE-002`.
- **Done when:** all checks pass 5 consecutive CI runs without a flaky failure.
- **Estimate:** 1.5 d

### T-030 · MVP acceptance gate

- **Delivers:** the ARCH-UI-001 walkthrough (start, list, edit, save, connect, TOFU, stub session, back) with goldens, driven without `teatest`; `scripts/tracecheck` in CI (NFR-QUAL-001); `scripts/covercheck`, the coverage gate (NFR-QUAL-003); tests that pin the CI checks of NFR-PORT-001, NFR-QUAL-002 and NFR-REL-001; the manual acceptance checklist [`acceptance.md`](acceptance.md); `docs/release-checklist.md` completed with the manual items: SEC-CRED-009 (store a password with build N, install unsigned build N+1 on macOS, connect, no Keychain dialog), FR-CONN-001 (`vim`, `htop` and `tmux` against a real `sshd`), the named terminals of NFR-PORT-002, and a Linux keyring-unlock prompt for FR-CRED-001.
- **Satisfies:** ARCH-UI-001, NFR-QUAL-001, NFR-QUAL-003, SEC-CRED-009, FR-CONN-001, FR-CRED-001 (manual: OS unlock prompt)
- **Depends on:** T-000 … T-029
- **Tests:** `TestARCH_UI_001_Walkthrough`, `TestNFR_QUAL_001_EveryMVPIDTraced` (wraps `scripts/tracecheck`), `TestNFR_QUAL_003_*` (covercheck), `TestNFR_PORT_001_CIRacesOnLinuxAndMacOS`, `TestNFR_QUAL_002_CIRunsStaticAnalysis`, `TestNFR_REL_001_GoroutinePackagesCheckLeaks`; CI steps `NFR-QUAL-001` and `NFR-QUAL-003`.
- **Done when:** tracecheck reports zero untraced MVP IDs, coverage meets both thresholds, and the manual checklist is signed off on macOS and Linux.
- **Estimate:** 1 d

---

## v1 milestones (outline)

No task detail yet. Each milestone is planned as tasks once the MVP gate (T-030)
passes.

| Milestone | Scope (requirement IDs) |
|---|---|
| **v1-A Organise** | Groups, nesting, collapse and inheritance (FR-GROUP-001 … FR-GROUP-005); tags and tag filter (FR-TAG-001, FR-TAG-002); clone, notes, favourites, reachability, details pane (FR-HOST-004 … FR-HOST-009); history (FR-HIST-001); highlighting, help, too-small, recent, sort, group filter, multi-select (FR-NAV-005 … FR-NAV-011); wider search (ARCH-UI-003) |
| **v1-B Connection extras** | ssh-agent (FR-CONN-007, SEC-AGENT-001); keepalive and reconnect (FR-CONN-009, FR-CONN-010, ARCH-SESS-007, ARCH-SESS-008); agent forwarding (FR-CONN-011, SEC-AGENT-002, ARCH-SESS-009); startup command (FR-CONN-012); quick connect (FR-CONN-013); `~.` escape (FR-CONN-014, ARCH-SESS-006) |
| **v1-C Jump hosts and forwarding** | FR-JUMP-001 … FR-JUMP-004, SEC-HOSTKEY-010, ARCH-SESS-012; forwarding rules, tunnels view and safety (FR-FWD-001 … FR-FWD-007, ARCH-SESS-013) |
| **v1-D Snippets** | FR-SNIP-001 … FR-SNIP-006 |
| **v1-E Identities, trust and import** | Save typed password, key generation, deploy key (FR-IDENT-005 … FR-IDENT-007); known-hosts view and trust once (FR-KH-005, FR-KH-007); import preview, jumps and forwards on import, export, CSV (FR-IMPORT-004 … FR-IMPORT-007, ARCH-DATA-003); copy command adds a canary test under SEC-CLIP-005 |
| **v1-F Settings** | External-change guard (FR-CFG-004, SEC-FILE-005, ARCH-DATA-005); `NO_COLOR`, themes, key bindings, defaults, settings view (FR-CFG-005 … FR-CFG-009); `--config` (FR-CLI-002) |
| **v1-G Release** | NFR-PERF-004, NFR-SIZE-001, NFR-BUILD-002, NFR-PORT-003; goreleaser pipeline and first tagged release |

`Later` items (FR-CRED-003, FR-CONN-015 … FR-CONN-017, FR-KH-006, SEC-HOSTKEY-011, FR-SFTP-*,
FR-TAB-001, ARCH-SESS-010, ARCH-SESS-011, SEC-CRED-008 and the rest) are not
planned.

---

## Resolved points

Six points raised while planning were ruled on by the coordinator on 2026-10-29
and written into the specs:

1. Debug logging is switched on with `SSHELF_DEBUG=1` (SEC-LOG-003, design §5.1).
2. Core-dump hardening is an unexported function in `cmd/sshelf`, called first
   thing at startup; there is no `hardening` package (SEC-MEM-003, design §5.1).
3. First launch shows the Welcome screen (FR-CFG-001 now defers to FR-NAV-013).
4. The design §10 terminal test uses only MVP features: the default config
   directory or an `XDG_CONFIG_HOME`/`HOME` override, and `exit` to end the
   session. ARCH-SESS-005's `~.` case is tested in v1-B.
5. SEC-CLIP-005 and SEC-MEM-008 AC1 are MVP invariants checked by the import-rule
   test (no clipboard package, no `os/exec`), delivered in T-000.
6. SEC-HOSTKEY-003 now covers plain and hashed `~/.ssh/known_hosts` entries and
   `@revoked`; `@cert-authority` moved to SEC-HOSTKEY-011 (`Later`, FR-KH-006).

## Risks

- **Credential store on headless CI.** `gnome-keyring-daemon` under
  `dbus-run-session` and throwaway macOS keychains are fragile on hosted
  runners. Mitigation: all logic tests use `MemStore`; the `integration_keyring`
  jobs (T-010) are separate, allowed one retry, and the no-bus case is itself a
  test.
- **Raw terminal handover is hard to test.** `tea.Exec` on v2 has only been
  verified from source (design §2), and PTY tests are timing-sensitive,
  especially on macOS runners. Mitigation: the T-000 manual check with a
  v2.1.0 fallback, then PTY tests that poll for output with generous deadlines
  rather than sleeping.
- **Bubble Tea v2 ecosystem gaps.** `teatest` v2, `charmbracelet/ssh` or
  `wish/testsession` may still pull v1 modules or lag behind v2.1.x.
  Mitigation: T-001 builds sshtest directly on `x/crypto/ssh` if needed; T-000
  confirms a v2-compatible `teatest` before UI tasks start.
- **Noisy timing checks.** NFR-PERF-001/002/003 run on shared runners.
  Mitigation: medians of 5 to 10 runs, and a separate perf job that can be
  re-run without blocking unrelated PRs; persistent flakes reopen the
  threshold with the product owner, not the test.
- **go-keyring error mapping.** `ErrDenied` versus `ErrUnavailable` depends on
  parsing `security` exit codes and D-Bus errors, which vary by OS version and
  provider (GNOME Keyring, KWallet). Mitigation: a table of observed errors in
  `TestOSStore_ErrorMapping`, extended whenever a new one is seen.

---

## Manual-only requirements

An `MVP` requirement listed here needs no automated test: `scripts/tracecheck`
(NFR-QUAL-001) accepts it without one, but it must still be `Implemented`,
and [`acceptance.md`](acceptance.md) must have the steps that verify it. Add
a row only when no part of the requirement can be checked by a program.

| ID | Why it cannot be automated | Steps |
|---|---|---|

None at the MVP gate: every MVP requirement has at least one automated test.
These acceptance criteria are still checked by hand, in addition to their
tests:

- FR-CONN-001: `vim`, `htop` and `tmux` against a real `sshd`.
- FR-CONN-004: `tput cols; tput lines` on a real host after a resize.
- FR-CONN-005: no remote screen remnants after a full-screen program.
- SEC-CRED-009 AC1: no Keychain dialog after installing an unsigned new build.
- FR-CRED-001: the OS unlock prompt (macOS Keychain, Linux keyring).
- NFR-PORT-002: the named terminal emulators.
- ADR-007 (T-000): the `devcheck` raw-mode and `stty` round trip.

## Traceability matrix

Every `MVP` requirement and the tasks that cover it. The task that completes an
ID is listed last.

### Functional (`FR-*`)

| ID | Tasks |
|---|---|
| FR-CRED-001 | T-010, T-012, T-019, T-030 |
| FR-CRED-002 | T-023 |
| FR-HOST-001 | T-015 |
| FR-HOST-002 | T-016, T-022 |
| FR-HOST-003 | T-015 |
| FR-HOST-010 | T-022 |
| FR-IDENT-001 | T-019, T-020 |
| FR-IDENT-002 | T-012 |
| FR-IDENT-003 | T-002, T-021 |
| FR-IDENT-004 | T-019, T-028 |
| FR-CONN-001 | T-003, T-004, T-030 |
| FR-CONN-002 | T-012 |
| FR-CONN-003 | T-013 |
| FR-CONN-004 | T-003 |
| FR-CONN-005 | T-004 |
| FR-CONN-006 | T-014 |
| FR-CONN-008 | T-013 |
| FR-KH-001 | T-007 |
| FR-KH-002 | T-008 |
| FR-KH-003 | T-008 |
| FR-KH-004 | T-007 |
| FR-NAV-001 | T-004, T-016 |
| FR-NAV-002 | T-018 |
| FR-NAV-003 | T-016, T-019, T-025 |
| FR-NAV-004 | T-016 |
| FR-NAV-013 | T-017 |
| FR-IMPORT-001 | T-024, T-025 |
| FR-IMPORT-002 | T-024 |
| FR-IMPORT-003 | T-025 |
| FR-CFG-001 | T-005, T-017 |
| FR-CFG-002 | T-006 |
| FR-CFG-003 | T-006 |
| FR-CLI-001 | T-026 |

### Security (`SEC-*`)

| ID | Tasks |
|---|---|
| SEC-CRED-001 | T-006, T-028 |
| SEC-CRED-002 | T-009, T-019 |
| SEC-CRED-003 | T-009, T-010, T-021 |
| SEC-CRED-004 | T-020 |
| SEC-CRED-005 | T-010, T-023 |
| SEC-CRED-006 | T-014 |
| SEC-CRED-007 | T-013 |
| SEC-CRED-009 | T-010, T-030 |
| SEC-HOSTKEY-001 | T-000, T-002 |
| SEC-HOSTKEY-002 | T-007 |
| SEC-HOSTKEY-003 | T-008 |
| SEC-HOSTKEY-004 | T-007 |
| SEC-HOSTKEY-005 | T-008 |
| SEC-HOSTKEY-007 | T-008 |
| SEC-MEM-001 | T-009 |
| SEC-MEM-003 | T-026 |
| SEC-MEM-008 | T-000, T-010 |
| SEC-MEM-009 | T-012 |
| SEC-LOG-001 | T-028 |
| SEC-LOG-002 | T-009 |
| SEC-LOG-003 | T-011 |
| SEC-LOG-005 | T-019 |
| SEC-CLIP-005 | T-000 |
| SEC-FILE-001 | T-005 |
| SEC-FILE-002 | T-005 |
| SEC-FILE-003 | T-006 |
| SEC-FILE-009 | T-002 |
| SEC-SESSION-001 | T-012 |
| SEC-SESSION-002 | T-013 |
| SEC-SESSION-003 | T-013 |
| SEC-SESSION-004 | T-000, T-002 |

### Architecture (`ARCH-*`)

| ID | Tasks |
|---|---|
| ARCH-SESS-001 | T-000, T-002 |
| ARCH-SESS-002 | T-004, T-013 |
| ARCH-SESS-003 | T-003 |
| ARCH-SESS-004 | T-003 |
| ARCH-SESS-005 | T-003, T-027 |
| ARCH-CONN-001 | T-012 |
| ARCH-CONN-002 | T-013 |
| ARCH-CONN-003 | T-007 |
| ARCH-CONN-004 | T-008 |
| ARCH-CONN-005 | T-002, T-014 |
| ARCH-SEC-001 | T-028 |
| ARCH-SEC-002 | T-000, T-002 |
| ARCH-SEC-004 | T-027 |
| ARCH-SEC-006 | T-023 |
| ARCH-PKG-001 | T-000 |
| ARCH-PKG-002 | T-005 |
| ARCH-DATA-001 | T-000, T-015, T-020 |
| ARCH-DATA-004 | T-020 |
| ARCH-UI-001 | T-004, T-030 |
| ARCH-UI-002 | T-004 |
| ARCH-UI-004 | T-016 |
| ARCH-TEST-001 | T-001 |

### Non-functional (`NFR-*`)

| ID | Tasks |
|---|---|
| NFR-PERF-001 | T-029 |
| NFR-PERF-002 | T-018 |
| NFR-PERF-003 | T-029 |
| NFR-SIZE-002 | T-029 |
| NFR-BUILD-001 | T-000 |
| NFR-PORT-001 | T-000 |
| NFR-PORT-002 | T-029 |
| NFR-UX-001 | T-016, T-029 |
| NFR-UX-002 | T-016 |
| NFR-UX-003 | T-016 |
| NFR-PRIV-001 | T-029 |
| NFR-REL-001 | T-000 |
| NFR-QUAL-001 | T-030 |
| NFR-QUAL-002 | T-000 |
| NFR-QUAL-003 | T-000, T-030 |

Totals: 33 FR + 31 SEC + 22 ARCH + 15 NFR = **101 MVP requirements**, all
covered by T-000 … T-030.
