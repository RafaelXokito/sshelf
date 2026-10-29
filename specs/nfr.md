# sshelf non-functional requirements

Status: Draft · Owner: orchestrator · Last updated: 2026-10-29

This file is the single home of the non-functional requirements (`NFR-*`). It
replaces the "Proposed NFRs" sections that used to sit in
`product/requirements.md` §23 and `architecture/design.md` §11. Conventions are
in [`README.md`](README.md): EARS syntax, a priority of `MVP`, `v1` or `Later`,
and acceptance checks. [`decisions.md`](decisions.md) wins any conflict.

Every check below can run unattended in CI. Unless stated otherwise, "the CI
runner" means the GitHub-hosted `ubuntu-latest` (Linux amd64) runner, and
timing checks take the median of 5 runs to absorb runner noise. Test names
follow the `TestNFR_<AREA>_NNN_...` / `BenchmarkNFR_<AREA>_NNN_...` pattern.

Areas: `PERF` speed · `SIZE` memory and binary size · `BUILD` build and release
· `PORT` operating systems, terminals and servers · `UX` keyboard, colour and
small terminals · `PRIV` network behaviour · `REL` reliability · `QUAL` code
quality and CI.

---

## 1. Performance (`NFR-PERF`)

### NFR-PERF-001 · Startup time
When sshelf starts with a `config.json` holding 1,000 hosts, the system shall render the interactive host list within 150 ms, without reading any secret from the OS credential store.

- **Priority:** MVP
- **Check:** `TestNFR_PERF_001_Startup1k` (PTY E2E) launches the real binary against a 1,000-host fixture config directory and measures the time from `exec` to the first frame that contains the first label: median of 5 runs ≤ 150 ms on the CI runner. The credential-store availability probe runs asynchronously and does not delay the first frame. A unit test with `secrets.MemStore` asserts `Calls("get") == 0` after startup.

### NFR-PERF-002 · Search latency
When the user types a character into the search field with 5,000 hosts loaded, the system shall compute the new filtered and ranked list within 16 ms.

- **Priority:** MVP
- **Check:** `BenchmarkNFR_PERF_002_Filter5k` over a generated 5,000-host fixture with 1- to 6-character queries. `TestNFR_PERF_002_Filter5k` runs it through `testing.Benchmark` and fails if ns/op exceeds 16,000,000 on the CI runner.

### NFR-PERF-003 · Connect overhead
When the user presses Enter on a host, the system shall send the SSH client version string within 100 ms, excluding name resolution and credential-store time.

- **Priority:** MVP
- **Check:** `TestNFR_PERF_003_ConnectOverhead` drives the root model with a host at `127.0.0.1` and `secrets.MemStore`. The time from `ConnectRequestMsg` to the in-process test server recording the client version banner is ≤ 100 ms (median of 10 runs).

### NFR-PERF-004 · Session passthrough latency
While a session is attached, the system shall add no more than 2 ms at the 99th percentile to keystroke echo latency compared with writing directly to the SSH channel.

- **Priority:** v1
- **Check:** `BenchmarkNFR_PERF_004_EchoRTT` sends 1,000 single-byte keystrokes to an echoing in-process server once through `session.Run()` with pipe-backed terminals and once straight to the channel. The p99 difference is ≤ 2 ms. The baseline is in-process so that CI needs no OpenSSH.

---

## 2. Size (`NFR-SIZE`)

### NFR-SIZE-001 · Memory use
While sshelf is idle with 5,000 hosts loaded and no session open, the system shall use no more than 60 MB of resident memory.

- **Priority:** v1
- **Check:** `TestNFR_SIZE_001_IdleRSS` (Linux only) starts the binary in a PTY with a 5,000-host fixture, waits 1 s after the first frame and reads `VmRSS` from `/proc/<pid>/status`: ≤ 61,440 kB.

### NFR-SIZE-002 · Binary size
The system shall produce a release binary of no more than 20 MB for each supported target.

- **Priority:** MVP
- **Check:** The CI build step builds each target with `-trimpath -ldflags "-s -w"` and fails if any binary exceeds 20,971,520 bytes.

---

## 3. Build and release (`NFR-BUILD`)

### NFR-BUILD-001 · Static, cgo-free binaries
The system shall build with `CGO_ENABLED=0` for `linux/amd64`, `linux/arm64`, `darwin/amd64` and `darwin/arm64` from a single Linux runner.

- **Priority:** MVP · **Source:** D-008
- **Check:** The CI cross-compile job builds all 4 targets on `ubuntu-latest` with `CGO_ENABLED=0`. `go version -m` on each binary reports `CGO_ENABLED=0`, and `file` reports the Linux binaries as statically linked.

### NFR-BUILD-002 · Release artefacts
When a version tag is pushed, the system's release pipeline shall publish one archive per supported target, a SHA-256 checksums file, and binaries whose `--version` output names the tag, commit and build date.

- **Priority:** v1
- **Check:** CI runs `goreleaser release --snapshot --clean` on every PR touching the release config and asserts that 4 archives and `checksums.txt` exist, every checksum verifies, and the `linux/amd64` binary's `--version` output contains the snapshot version, the commit SHA and an RFC 3339 date.

---

## 4. Portability (`NFR-PORT`)

### NFR-PORT-001 · Operating systems
The system shall pass its full test suite, including the race detector, on Linux amd64 and macOS arm64.

- **Priority:** MVP · **Source:** README scope (Windows is `Later`)
- **Check:** The CI test job runs `go test -race ./...` on `ubuntu-latest` and `macos-latest`, and both must pass for a PR to merge.

### NFR-PORT-002 · Terminal types
The system shall start, render the host list, run a session and restore the terminal under each of the `TERM` values `xterm`, `xterm-256color`, `screen-256color`, `tmux-256color`, `xterm-kitty` and `linux`.

- **Priority:** MVP
- **Check:** `TestNFR_PORT_002_TermMatrix` (PTY E2E, table-driven over the `TERM` values) launches the binary, connects to the in-process test server, exits the remote shell, and asserts that the host list is redrawn and `stty -a` matches its value before launch. The list of named emulators (iTerm2, Terminal.app, kitty, WezTerm, Alacritty, Ghostty, GNOME Terminal, Konsole, xterm, tmux, screen) is a manual release checklist item, not a CI check.

### NFR-PORT-003 · SSH server interoperability
The system shall connect with password, keyboard-interactive and public-key authentication (Ed25519, ECDSA P-256, RSA with SHA-2 and encrypted OpenSSH-format keys) to OpenSSH 7.4, the latest OpenSSH release and Dropbear.

- **Priority:** v1
- **Check:** `TestNFR_PORT_003_Interop` (build tag `interop`, Linux CI job) starts one container per server version and runs each authentication method and key type against it. Every combination connects and runs `true` with exit status 0.

---

## 5. User experience (`NFR-UX`)

### NFR-UX-001 · Keyboard only
The system shall make every action reachable from the keyboard and shall never enable terminal mouse tracking.

- **Priority:** MVP · **Source:** FR-NAV-012 (mouse is `Later` and off by default)
- **Check:** `TestNFR_UX_001_EveryActionBound` asserts that every action of every screen in `ui/keys` has at least one key binding. `TestNFR_UX_001_NoMouseTracking` (PTY E2E) scans all output for `ESC[?1000h`, `ESC[?1002h`, `ESC[?1003h` and `ESC[?1006h` and finds none.

### NFR-UX-002 · Layout at the minimum size
While the terminal is at least 80 columns by 24 rows, the system shall render every screen and modal with no line wider than the terminal and with the key-hints footer visible.

- **Priority:** MVP · **Source:** design §10 (goldens at 80×24 and 120×40). Behaviour below 60×12 is FR-NAV-007 (v1).
- **Check:** Golden tests of every screen and modal at 80×24 and 120×40 run through a shared helper that fails if any rendered line exceeds the width (measured with `lipgloss.Width`) or if the last line is not the footer.

### NFR-UX-003 · Not colour alone
The system shall convey selection, focus, errors and warnings with a glyph or text marker in addition to any colour.

- **Priority:** MVP · **Source:** accessibility. `NO_COLOR` support is FR-CFG-005 (v1).
- **Check:** `TestNFR_UX_003_AsciiProfile` renders every golden with the ASCII colour profile and asserts that the selected row carries the selection glyph, the focused field carries the focus glyph, and errors and warnings carry their text prefix.

---

## 6. Privacy (`NFR-PRIV`)

### NFR-PRIV-001 · No unsolicited network traffic
The system shall make no network connection other than the SSH connections the user starts, and shall send no telemetry and perform no update checks.

- **Priority:** MVP
- **Check:** `TestNFR_PRIV_001_NoHTTPClient` in `internal/archtest` asserts that `go list -deps ./cmd/sshelf` contains no `net/http`, and that `net.Dial`/`net.Dialer` are referenced only from `internal/sshclient` (checked with `go/packages`).

---

## 7. Reliability (`NFR-REL`)

### NFR-REL-001 · Race-free and leak-free
The system shall have no data races and no leaked goroutines in its test suite.

- **Priority:** MVP
- **Check:** CI runs `go test -race ./...` (NFR-PORT-001). Every package that starts goroutines (`app`, `sshclient`, `session`, `secrets`, `ui`) calls `goleak.VerifyTestMain`, so a leaked goroutine fails the package.

---

## 8. Quality (`NFR-QUAL`)

### NFR-QUAL-001 · Tests traced to requirements
The system shall have, for every `MVP` requirement (`FR-*`, `SEC-*`, `ARCH-*`, `NFR-*`), at least one automated test whose name contains the requirement ID with `-` replaced by `_`, or an entry in the manual release checklist.

- **Priority:** MVP
- **Check:** `scripts/tracecheck` extracts the MVP IDs from the spec files and the test names from `go test -list . ./...`, plus the IDs in `docs/release-checklist.md`. CI fails on any MVP ID with neither.

### NFR-QUAL-002 · Static analysis
The system shall pass `gofmt`, `go vet`, `golangci-lint` (with at least `staticcheck`, `errcheck`, `gosec` and `govet` enabled) and `govulncheck` on every pull request.

- **Priority:** MVP · **Source:** SEC-SESSION-004 (govulncheck)
- **Check:** The CI lint job runs each tool and fails on any finding. `gofmt -l .` must print nothing.

### NFR-QUAL-003 · Test coverage
The system shall keep statement coverage at 80 % or more in `domain`, `store`, `secrets`, `paths`, `sshclient`, `sshconfig` and `search`, and at 70 % or more across the module.

- **Priority:** MVP
- **Check:** CI runs `go test -coverprofile` and a script reads `go tool cover -func` output and fails below either threshold.

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

## 10. Count

| Priority | Count |
|---|---|
| MVP | 15 |
| v1 | 4 |
| **Total** | **19** |
