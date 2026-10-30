## Task

T-NNN: <title>

Requirements: <IDs>

## Checks

- [ ] `gofmt -l .` prints nothing
- [ ] `go vet ./...`
- [ ] `golangci-lint run ./...`
- [ ] `go test -race ./...`
- [ ] `go test -count=1 -run 'TestNFR_PERF_' ./...`
- [ ] `go run ./scripts/tracecheck`

## Perf

Fill in the rows the change can affect, from the CI logs or a local run
without `-race`.

| Check | Value | Limit |
|---|---|---|
| startup, 1,000 hosts (NFR-PERF-001) | ms | ≤ 150 ms |
| search, 5,000 hosts (NFR-PERF-002) | ms | ≤ 16 ms |
| emulator: throughput (NFR-PERF-005, perf-session job) | MB/s | ≥ 1 MB/s |
| emulator: heap per session, 120×40, full scrollback (NFR-SIZE-003) | MiB | ≤ 48 MiB |
