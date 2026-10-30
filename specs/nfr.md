# sshelf non-functional requirements

Status: Draft · Owner: orchestrator · Last updated: 2026-10-30 (M3)

This file is the single home of the non-functional requirements (`NFR-*`). It
replaces the "Proposed NFRs" sections that used to sit in
`product/requirements.md` §23 and `architecture/design.md` §11. Conventions are
in [`README.md`](README.md): EARS syntax, a priority of `MVP`, `M2`, `M3`, `v1` or
`Later`, and acceptance checks. [`decisions.md`](decisions.md) wins any conflict.

Every check below can run unattended in CI. Unless stated otherwise, "the CI
runner" means the GitHub-hosted `ubuntu-latest` (Linux amd64) runner, and
timing checks take the median of 5 runs to absorb runner noise. Test names
follow the `TestNFR_<AREA>_NNN_...` / `BenchmarkNFR_<AREA>_NNN_...` pattern.

Each `MVP` requirement shows its status next to its priority; every other
requirement is `Draft`. Checks that are CI steps rather than Go tests
(NFR-SIZE-002, NFR-BUILD-001, NFR-PORT-001, NFR-REL-001, NFR-QUAL-002) are
`Implemented` once the step runs in `.github/workflows/ci.yml`.

Areas: `PERF` speed · `SIZE` memory and binary size · `BUILD` build and release
· `PORT` operating systems, terminals and servers · `UX` keyboard, colour and
small terminals · `PRIV` network behaviour · `REL` reliability · `QUAL` code
quality and CI.

---

## 1. Performance (`NFR-PERF`)

Timing checks run without `-race`. Because hosted CI runners are shared and
occasionally stall, a CI timing check that fails may be retried **once** in
the same job; the first failure is kept as a visible warning, and a check that
fails twice fails the build. Limits are never raised to absorb runner noise.

### NFR-PERF-001 · Startup time
When sshelf starts with a `config.json` holding 1,000 hosts, the system shall render the interactive host list within 150 ms, without reading any secret from the OS credential store.

- **Priority:** MVP · **Status:** Implemented
- **Check:** `TestNFR_PERF_001_Startup1k` (PTY E2E) launches the real binary against a 1,000-host fixture config directory and measures the time from `exec` to the first frame that contains the first label: median of 5 runs ≤ 150 ms on the CI runner. The credential-store availability probe runs asynchronously and does not delay the first frame. A unit test with `secrets.MemStore` asserts `Calls("get") == 0` after startup.

### NFR-PERF-002 · Search latency
When the user types a character into the search field with 5,000 hosts loaded, the system shall compute the new filtered and ranked list within 16 ms.

- **Priority:** MVP · **Status:** Implemented
- **Check:** `BenchmarkNFR_PERF_002_Filter5k` over a generated 5,000-host fixture with 1- to 6-character queries. `TestNFR_PERF_002_Filter5k` runs it through `testing.Benchmark` and fails if ns/op exceeds 16,000,000 on the CI runner.

### NFR-PERF-003 · Connect overhead
When the user presses Enter on a host, the system shall send the SSH client version string within 100 ms, excluding name resolution and credential-store time.

- **Priority:** MVP · **Status:** Implemented
- **Check:** `TestNFR_PERF_003_ConnectOverhead` drives the root model with a host at `127.0.0.1` and `secrets.MemStore`. The time from `ConnectRequestMsg` to the in-process test server recording the client version banner is ≤ 100 ms (median of 10 runs).

### NFR-PERF-004 · Keystroke overhead
While a session tab is focused, when the root model receives a key press, the system shall write the encoded key to the session's SSH channel within 5 ms at the 99th percentile.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-009 (was v1 "Session passthrough latency", measured against the MVP's raw passthrough)
- **Check:** `TestNFR_PERF_004_KeystrokeOverhead` drives the root model directly (as `tea.Program` would) with a real session connected to the in-process server, sends 1,000 `tea.KeyPressMsg` values for printable keys and arrows, and timestamps each from the call to `Update` until sshtest's channel reader has received the bytes. p99 ≤ 5 ms on the CI runner. A second run while another session streams output at 1 MB/s must meet the same bound (the input path never waits for output processing, ARCH-SESS-014).

### NFR-PERF-005 · Output throughput
When a session receives a burst of output, the system shall process at least 1 MB/s of it into the emulator while keeping the UI responsive and the memory held for unprocessed output bounded.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-009, ADR-013 (x/vt v0.1.0 processes 2 to 5 MB/s on an Apple M1 Pro)
- **Check:** `TestNFR_PERF_005_TenMegabytes` connects one session at 120×40 to sshtest's `flood 10M` command (10 MiB of 100-column text lines, half of them with SGR colours) and asserts on the CI runner: (1) the last line is on the emulator's screen within 10 s; (2) while the flood runs, `View()` is produced at least every 100 ms and Ctrl+C reaches sshtest within 100 ms; (3) the heap never exceeds NFR-SIZE-003's per-session bound plus 8 MiB during the flood: sshelf reads the channel only as fast as the emulator processes it, so x/crypto/ssh's channel window (2 MiB) slows the server instead of sshelf buffering (ARCH-SESS-014); (4) the session's input queue never exceeds its 1 MiB cap. `BenchmarkNFR_PERF_005_EmulatorWrite` reports MB/s for the emulator alone, for tracking regressions.

### NFR-PERF-006 · Ten concurrent sessions
While 10 sessions are open, each receiving output at 100 KB/s, the system shall render a newly focused tab's first frame within 50 ms and keep keystroke overhead within 20 ms at the 99th percentile.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-010
- **Check:** `TestNFR_PERF_006_TenSessions` (PTY E2E, Linux) runs the real binary against sshtest, opens 10 sessions to `stream 100K` shells, then: switching with `Ctrl+]` `n` produces the new tab's status bar in the PTY output within 50 ms (median of 10 switches); 200 keystrokes to the focused session have a p99 overhead ≤ 20 ms measured as in NFR-PERF-004; RSS stays within NFR-SIZE-003's bound times 10 plus NFR-SIZE-001's base.

### NFR-PERF-007 · Transfer throughput
When a 256 MiB file is downloaded or uploaded over loopback, the system shall transfer it at an average of at least 50 MB/s.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-018
- **Check:** `TestNFR_PERF_007_Throughput` (no `-race`, the `perf-sftp` CI job) starts sshtest with its SFTP subsystem over a temp dir (ARCH-TEST-004), opens a real SFTP browser on a connection to it, and times a 256 MiB file of random bytes from enqueue to `done`, once as a download and once as an upload: the median of 3 runs of each is ≥ 50 MB/s (≤ 5.4 s) on the CI runner, and both copies have the source's SHA-256. The measured MB/s are recorded in the PR template's perf section. `BenchmarkNFR_PERF_007_Download` and `BenchmarkNFR_PERF_007_Upload` report MB/s for tracking. The figure is a floor for sshelf's own overhead (buffers, part files, progress); real links are limited by the network and the server.

### NFR-PERF-008 · Responsive UI during transfers
While two transfers run, the system shall handle every key press and draw its frame within 100 ms, and, while a transfer shares a session's connection, shall deliver that session's keystrokes within 50 ms at the 99th percentile (D-021).

- **Priority:** M3 · **Status:** Implemented · **Source:** D-017, D-018, D-021
- **Check:** `TestNFR_PERF_008_FrameGaps` (`perf-sftp` CI job) drives the root model as `tea.Program` would (one goroutine calls `Update` and `View`; commands run on their own goroutines; their messages and the keys share one queue) with a real SFTP browser while a 256 MiB download and a 256 MiB upload run against sshtest, each queued again as soon as it finishes (over loopback one takes well under a second, and the keys take 10 s): 200 cursor-move keys sent to the SFTP tab, 50 ms apart, each produce a `View()` showing the moved cursor within 100 ms (p100), and no single `Update` or `View` call takes more than 20 ms. The progress line is redrawn at most 4 times a second (ARCH-SFTP-003). A second run, `TestNFR_PERF_008_SessionKeysDuringTransfers`, sends 1,000 keys to a session tab on the same connection while the transfers run: p99 ≤ 50 ms (D-021). The upload keeps 64 requests in flight (ADR-016), which sit on the TCP connection ahead of each key; NFR-PERF-004's 5 ms bound still applies with no transfer on the connection.
- **Note:** before D-021 the session-key part was measured against 5 ms and failed on the CI runners (p99 8 to 17 ms on Linux and macOS, 2 to 4 ms on an Apple M1 Pro): the upload's 64 × 32 KiB requests in flight are ahead of each key, so a key waits about 2 MiB ÷ upload rate, and the in-process sshtest competes for the runner's cores. D-021 keeps full upload speed and sets the bound to 50 ms. On `macos-latest` (3 vCPUs) the other parts are over their bounds in some runs (one `View` call of 19 to 30 ms against 20 ms, whose CPU cost is 1 to 2 ms; key to frame 18 to 30 ms against 100 ms). As for every timing check here, the bounds hold on the CI runner, `ubuntu-latest`; the `perf-sftp` job runs the checks on macOS too and reports them there without failing the build.

### NFR-PERF-009 · Large listings
When a directory with 10,000 entries is opened, the system shall show its first screen within 200 ms of receiving the listing, and shall apply each filter keystroke within 50 ms.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-017
- **Check:** `TestNFR_PERF_009_TenThousandEntries` (`perf-sftp` CI job) feeds a 10,000-entry listing message (mixed directories, files and symlinks, names up to 60 characters) to the root model and measures from `Update` receiving it to the `View()` that shows its first rows (sorting, hidden-file filtering and rendering of the visible rows only): median of 5 ≤ 200 ms on the CI runner. Typing `/` and 5 characters measures each filter step: each ≤ 50 ms. `BenchmarkNFR_PERF_009_SortAndRender` tracks it. A PTY variant, `TestNFR_PERF_009_E2ETenThousandEntries`, lists a 10,000-entry sshtest directory and checks that the first frame with the listing appears within 1 s of pressing `enter` (network included).

---

## 2. Size (`NFR-SIZE`)

### NFR-SIZE-001 · Memory use
While sshelf is idle with 5,000 hosts loaded and no session open, the system shall use no more than 60 MB of resident memory.

- **Priority:** v1
- **Check:** `TestNFR_SIZE_001_IdleRSS` (Linux only) starts the binary in a PTY with a 5,000-host fixture, waits 1 s after the first frame and reads `VmRSS` from `/proc/<pid>/status`: ≤ 61,440 kB.

### NFR-SIZE-003 · Memory per session
While a session at 120×40 has a full scrollback of 100-column lines, the system shall use no more than 48 MB of heap for that session, and shall cap the scrollback at 2,000 lines.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-010, ADR-013, open question 1 in `product/requirements.md` §22
- **Check:** `TestNFR_SIZE_003_SessionHeap` measures `runtime.MemStats.HeapAlloc` (after `runtime.GC()`) before and after one session at 120×40 receives 5,000 lines of 100 columns with SGR colours: the difference is ≤ 48 MiB and `ScrollbackLen()` is 2,000. With x/vt v0.1.0 the measured cost is about 31 MB. Closing the session and running `runtime.GC()` returns the heap to within 2 MiB of the starting value.

### NFR-SIZE-004 · Transfer memory
While files are transferred, the system shall use an amount of heap memory that does not depend on the files' sizes, at most 16 MiB above the idle SFTP tab for the two transfers that run at once.

- **Priority:** M3 · **Status:** Implemented · **Source:** D-018 ("streamed with bounded memory")
- **Check:** `TestNFR_SIZE_004_TransferHeap` (`perf-sftp` CI job) samples `runtime/metrics` `/memory/classes/heap/objects:bytes` and `/gc/heap/live:bytes` every 50 ms (after a `runtime.GC()` baseline with the SFTP tab open and one small transfer done each way) while a download and an upload run at the same time, first with 256 MiB files and then with 1 GiB files (sparse files on the local side; sshtest's in-memory zero file on the remote side): the peak growth of `objects` (garbage not yet collected included) is ≤ 16 MiB in both, and the two peaks of the live heap differ by less than 4 MiB. The size-independence is read from the live heap because the garbage a 50 ms sample catches depends on where the collector is in its cycle, and the longer transfer takes four times the samples, so `objects` peaks differ by up to 4 MiB with no size-dependent memory. Closing the tab and running `runtime.GC()` returns the heap to within 2 MiB of the baseline.

### NFR-SIZE-002 · Binary size
The system shall produce a release binary of no more than 20 MB for each supported target.

- **Priority:** MVP · **Status:** Implemented
- **Check:** The CI build step builds each target with `-trimpath -ldflags "-s -w"` and fails if any binary exceeds 20,971,520 bytes.

---

## 3. Build and release (`NFR-BUILD`)

### NFR-BUILD-001 · Static, cgo-free binaries
The system shall build with `CGO_ENABLED=0` for `linux/amd64`, `linux/arm64`, `darwin/amd64` and `darwin/arm64` from a single Linux runner.

- **Priority:** MVP · **Status:** Implemented · **Source:** D-008
- **Check:** The CI cross-compile job builds all 4 targets on `ubuntu-latest` with `CGO_ENABLED=0`. `go version -m` on each binary reports `CGO_ENABLED=0`, and `file` reports the Linux binaries as statically linked.

### NFR-BUILD-002 · Release artefacts
When a version tag is pushed, the system's release pipeline shall publish one archive per supported target, a SHA-256 checksums file, and binaries whose `--version` output names the tag, commit and build date.

- **Priority:** v1
- **Check:** CI runs `goreleaser release --snapshot --clean` on every PR touching the release config and asserts that 4 archives and `checksums.txt` exist, every checksum verifies, and the `linux/amd64` binary's `--version` output contains the snapshot version, the commit SHA and an RFC 3339 date.

---

## 4. Portability (`NFR-PORT`)

### NFR-PORT-001 · Operating systems
The system shall pass its full test suite, including the race detector, on Linux amd64 and macOS arm64.

- **Priority:** MVP · **Status:** Implemented · **Source:** README scope (Windows is `Later`)
- **Check:** The CI test job runs `go test -race ./...` on `ubuntu-latest` and `macos-latest`, and both must pass for a PR to merge.

### NFR-PORT-002 · Terminal types
The system shall start, render the host list, run a session and restore the terminal under each of the `TERM` values `xterm`, `xterm-256color`, `screen-256color`, `tmux-256color`, `xterm-kitty` and `linux`.

- **Priority:** MVP · **Status:** Implemented
- **Check:** `TestNFR_PORT_002_TermMatrix` (PTY E2E, table-driven over the `TERM` values) launches the binary, connects to the in-process test server, exits the remote shell, and asserts that the host list is redrawn and `stty -a` matches its value before launch. The list of named emulators (iTerm2, Terminal.app, kitty, WezTerm, Alacritty, Ghostty, GNOME Terminal, Konsole, xterm, tmux, screen) is a manual release checklist item, not a CI check.

### NFR-PORT-003 · SSH server interoperability
The system shall connect with password, keyboard-interactive and public-key authentication (Ed25519, ECDSA P-256, RSA with SHA-2 and encrypted OpenSSH-format keys) to OpenSSH 7.4, the latest OpenSSH release and Dropbear.

- **Priority:** v1
- **Check:** `TestNFR_PORT_003_Interop` (build tag `interop`, Linux CI job) starts one container per server version and runs each authentication method and key type against it. Every combination connects and runs `true` with exit status 0.

---

## 5. User experience (`NFR-UX`)

### NFR-UX-001 · Keyboard first, mouse only where asked
The system shall make every action reachable from the keyboard, and shall enable cell-motion mouse tracking only while an sshelf screen is focused or the focused session's remote app has requested mouse reporting.

- **Priority:** M2 · **Status:** Implemented · **Source:** D-011, D-012 (the MVP never enabled mouse tracking)
- **Check:**
  - `TestNFR_UX_001_EveryActionBound`: every action of every screen in `ui/keys` has at least one key binding, and every mouse action (FR-NAV-012, FR-NAV-014 … FR-NAV-017) has a key binding that does the same, including the form buttons (FR-NAV-015).
  - `TestNFR_UX_001_MouseModePolicy` (unit, root model): `View().MouseMode` is `MouseModeCellMotion` on the Hosts tab and on every sshelf screen and modal; `MouseModeNone` on a focused session whose remote has not requested mouse reporting, in scrollback mode and on an ended session; and the mode the remote asked for (cell motion for `?1000`/`?1002`, all motion for `?1003`) otherwise.
  - `TestNFR_UX_001_NoMouseTrackingInPlainSession` (PTY E2E): from the frame after a session tab is focused until the remote enables mouse reporting, the output contains no `ESC[?1000h`, `ESC[?1002h` or `ESC[?1003h`, and it contains `ESC[?1002l` (or `?1000l`) when the session is focused after the Hosts tab.
  - On exit, every mouse mode is turned off (Bubble Tea's restore, design §8.6).

### NFR-UX-002 · Layout at the minimum size
While the terminal is at least 80 columns by 24 rows, the system shall render every screen and modal with no line wider than the terminal and with the key-hints footer visible.

- **Priority:** MVP · **Status:** Implemented · **Source:** design §10 (goldens at 80×24 and 120×40). Behaviour below 60×12 is FR-NAV-007 (v1).
- **Check:** Golden tests of every screen and modal at 80×24 and 120×40 run through a shared helper that fails if any rendered line exceeds the width (measured with `lipgloss.Width`) or if the last line is not the footer.

### NFR-UX-003 · Not colour alone
The system shall convey selection, focus, errors and warnings with a glyph or text marker in addition to any colour.

- **Priority:** MVP · **Status:** Implemented · **Source:** accessibility. `NO_COLOR` support is FR-CFG-005 (v1).
- **Check:** `TestNFR_UX_003_AsciiProfile` renders every golden with the ASCII colour profile and asserts that the selected row carries the selection glyph, the focused field carries the focus glyph, and errors and warnings carry their text prefix.

---

## 6. Privacy (`NFR-PRIV`)

### NFR-PRIV-001 · No unsolicited network traffic
The system shall make no network connection other than the SSH connections the user starts, and shall send no telemetry and perform no update checks.

- **Priority:** MVP · **Status:** Implemented
- **Check:** `TestNFR_PRIV_001_NoHTTPClient` in `internal/archtest` asserts that `go list -deps ./cmd/sshelf` contains no `net/http`, and that `net.Dial`/`net.Dialer` are referenced only from `internal/sshclient` (checked with `go/packages`).

---

## 7. Reliability (`NFR-REL`)

### NFR-REL-001 · Race-free and leak-free
The system shall have no data races and no leaked goroutines in its test suite.

- **Priority:** MVP · **Status:** Implemented
- **Check:** CI runs `go test -race ./...` (NFR-PORT-001). Every package that starts goroutines (`app`, `sshclient`, `session`, `secrets`, `ui`) calls `goleak.VerifyTestMain`, so a leaked goroutine fails the package.

---

## 8. Quality (`NFR-QUAL`)

### NFR-QUAL-001 · Tests traced to requirements
The system shall have, for every `MVP`, `M2` and `M3` requirement (`FR-*`, `SEC-*`, `ARCH-*`, `NFR-*`), status `Implemented` and at least one automated test that references the requirement ID (usually in its name, with `-` replaced by `_`), unless `tasks.md` lists the requirement as manual-only.

- **Priority:** MVP · **Status:** Implemented · **Source:** extended to `M2` by the M2 gate (T-043) and to `M3` by the M3 gate (T-059)
- **Check:** `go run ./scripts/tracecheck` (a CI step, wrapped by `TestNFR_QUAL_001_EveryMilestoneIDTraced`) reads the MVP, M2 and M3 IDs and their status from the spec files and searches every `*_test.go` file (outside `testdata`) for `FR-X-NNN` or `FR_X_NNN`. It fails on an MVP, M2 or M3 ID that is not `Implemented`, is missing from its milestone's traceability matrix ("Traceability matrix", "M2 traceability matrix" or "M3 traceability matrix"), or is referenced by no test and not listed in its milestone's manual-only table ("Manual-only requirements", "M2 manual-only requirements" or "M3 manual-only requirements") in `tasks.md`. It also fails on a matrix or manual-only row that is not a requirement of that milestone, and on any `M2 revision` or `M3 revision` block left in a spec file. Manual-only steps are in `specs/acceptance.md`.

### NFR-QUAL-002 · Static analysis
The system shall pass `gofmt`, `go vet`, `golangci-lint` (with at least `staticcheck`, `errcheck`, `gosec` and `govet` enabled) and `govulncheck` on every pull request.

- **Priority:** MVP · **Status:** Implemented · **Source:** SEC-SESSION-004 (govulncheck)
- **Check:** The CI lint job runs each tool and fails on any finding. `gofmt -l .` must print nothing.

### NFR-QUAL-003 · Test coverage
The system shall keep statement coverage at 80 % or more in `domain`, `store`, `secrets`, `paths`, `sshclient`, `sshconfig`, `search` and `files` (added by the M3 gate, T-059), and at 70 % or more across the module.

- **Priority:** MVP · **Status:** Implemented
- **Check:** CI runs `go test -race -coverpkg=./... -coverprofile=coverage.out ./...` and `go run ./scripts/covercheck coverage.out`, which fails below either threshold. `-coverpkg` counts a statement as covered when any package's tests run it (the screens and modals are tested through the root model in `internal/ui`). The test helpers under `internal/testutil` are left out of the module total.

---

## 9. Consolidation notes

How the provisional NFRs from the two superseded sections map to this file.
Provisional IDs were never approved and are not kept.

| Provisional (source) | Now | Resolution |
|---|---|---|
| NFR-PERF-001 (req), NFR-PERF-001 (design) | NFR-PERF-001 | Both said 150 ms for 1,000 hosts. "2020-or-later laptop" replaced by the CI runner. "No keychain access" narrowed to "no secret read", because the startup availability probe (credential-storage §7) is required. |
| NFR-PERF-002 (req: 30 ms), NFR-PERF-002 (design: 16 ms) | NFR-PERF-002 | **16 ms**, matching ARCH-UI-003 and one 60 Hz frame. |
| NFR-PERF-003 (req: connect overhead) | NFR-PERF-003 | Kept, measured against the in-process server. |
| NFR-PERF-003 (design: passthrough vs OpenSSH) | NFR-PERF-004 | Baseline changed from OpenSSH to a direct channel write so CI needs no OpenSSH. Moved to v1. |
| NFR-PERF-004 (req: 60 MB at 5,000 hosts), NFR-RES-001 (design: 30 MB idle, 5 MB per background session) | NFR-SIZE-001 | **60 MB with 5,000 hosts**, a defined load. The per-session figure is dropped because background sessions are `Later`. v1. |
| NFR-DIST-001 (design) | NFR-SIZE-002, NFR-BUILD-001, NFR-BUILD-002 | Split into size, static build and release artefacts. |
| NFR-PORT-001 (req: single binary, 4 targets) | NFR-BUILD-001, NFR-PORT-001 | Merged. |
| NFR-PORT-002 (req), NFR-PORT-001 (design: terminals, 80×24, colours, `NO_COLOR`) | NFR-PORT-002, NFR-UX-002, NFR-UX-003 | Terminal names became a CI `TERM` matrix plus a manual checklist. The minimum layout size is 80×24; below 60×12 is FR-NAV-007. `NO_COLOR` is already FR-CFG-005 and is not duplicated. |
| NFR-PORT-002 (design: OpenSSH ≥ 7.4, Dropbear, keys) | NFR-PORT-003 | Kept as a v1 container-based check. MVP interop is covered by the in-process server (ARCH-TEST-001). |
| NFR-UX-001 (req), NFR-UX-002 (req: no mouse, account or network) | NFR-UX-001, NFR-PRIV-001 | Merged. "No account" is implied by D-002/D-003 and needs no check. |
| NFR-PRIV-001 (req and design) | NFR-PRIV-001 | Merged. |
| NFR-REL-001 (design: terminal restore) | none | Duplicate of ARCH-SESS-005 and ARCH-SEC-004, which already require the PTY suite. NFR-REL-001 now covers races and goroutine leaks. |
| NFR-QUAL-001 (req: every MVP requirement), NFR-QUAL-001 (design: every ARCH and FR) | NFR-QUAL-001 | Union of prefixes, limited to MVP. NFR-QUAL-002 and NFR-QUAL-003 are new. |
| M2 (2026-10-30) | NFR-PERF-004 (reworded), NFR-PERF-005, NFR-PERF-006, NFR-SIZE-003, NFR-UX-001 (revision) | Framed sessions (D-009) replace raw passthrough, so the latency check measures sshelf's own input path. The per-session memory figure dropped earlier comes back, measured against the chosen emulator (ADR-013). The M2 gate (T-043) extends NFR-QUAL-001's tracecheck to `M2`. |
| M3 (2026-10-30) | NFR-PERF-007, NFR-PERF-008, NFR-PERF-009, NFR-SIZE-004 | New for the SFTP tab (D-017, D-018). The existing checks apply to the SFTP tab without a revision: NFR-UX-001 (every mouse action, drag included, has a key; the SFTP tab is an sshelf screen with cell-motion tracking), NFR-UX-002 and NFR-UX-003 (SFTP goldens at 80×24 and 120×40), NFR-PRIV-001 (no new network use), NFR-REL-001 (`internal/files` calls `goleak.VerifyTestMain`), NFR-QUAL-003 (`internal/files` joins the 80 % packages). The M3 gate (T-059) extends NFR-QUAL-001's tracecheck to `M3`. |

## 10. Count

| Priority | Count |
|---|---|
| MVP | 14 |
| M2 | 5 (NFR-PERF-004 moved from v1, NFR-PERF-005, NFR-PERF-006, NFR-SIZE-003, NFR-UX-001 revised from MVP by T-043) |
| M3 | 4 (NFR-PERF-007, NFR-PERF-008, NFR-PERF-009, NFR-SIZE-004) |
| v1 | 3 |
| **Total** | **26** |
