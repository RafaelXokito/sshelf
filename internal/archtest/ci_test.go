package archtest

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The NFR checks that CI, not a Go test, performs (race detector on both
// OSes, static analysis, the traceability and coverage gates). These
// tests pin the CI configuration so that a workflow edit cannot drop one
// of them silently.

func readRootFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(moduleRoot(t), name)) //nolint:gosec // a fixed file in this repo
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func wantAll(t *testing.T, file, text string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("%s lacks %q", file, w)
		}
	}
}

const ciFile = ".github/workflows/ci.yml"

// TestNFR_PORT_001_CIRacesOnLinuxAndMacOS: the test job runs the full
// suite with the race detector on ubuntu-latest and macos-latest.
func TestNFR_PORT_001_CIRacesOnLinuxAndMacOS(t *testing.T) {
	ci := readRootFile(t, ciFile)
	wantAll(t, ciFile, ci, "os: [ubuntu-latest, macos-latest]", "runs-on: ${{ matrix.os }}", "go test -race")
	if !regexp.MustCompile(`go test -race[^\n]* \./\.\.\.`).MatchString(ci) {
		t.Errorf("%s does not run go test -race over ./...", ciFile)
	}
}

// TestNFR_QUAL_002_CIRunsStaticAnalysis: gofmt, go vet, golangci-lint
// with staticcheck, errcheck, gosec and govet, and govulncheck.
func TestNFR_QUAL_002_CIRunsStaticAnalysis(t *testing.T) {
	ci := readRootFile(t, ciFile)
	wantAll(t, ciFile, ci, "gofmt -l .", "go vet ./...", "golangci/golangci-lint-action", "golang.org/x/vuln/cmd/govulncheck")
	lint := readRootFile(t, ".golangci.yml")
	wantAll(t, ".golangci.yml", lint, "- errcheck", "- gosec", "- govet", "- staticcheck")
}

// TestNFR_QUAL_001_CIRunsTracecheck: CI runs the traceability gate.
func TestNFR_QUAL_001_CIRunsTracecheck(t *testing.T) {
	wantAll(t, ciFile, readRootFile(t, ciFile), "go run ./scripts/tracecheck")
}

// TestNFR_QUAL_003_CIRunsCoverageGate: CI writes a cross-package profile
// and gates it.
func TestNFR_QUAL_003_CIRunsCoverageGate(t *testing.T) {
	wantAll(t, ciFile, readRootFile(t, ciFile), "-coverpkg=./... -coverprofile=coverage.out", "go run ./scripts/covercheck coverage.out")
}

// TestNFR_PERF_005_CIRunsPerfSessionJob: the emulator throughput and
// session memory checks run without -race in a job of their own, so it
// can be re-run alone (T-033), and the PR template records them.
func TestNFR_PERF_005_CIRunsPerfSessionJob(t *testing.T) {
	wantAll(t, ciFile, readRootFile(t, ciFile), "perf-session:", "-run 'TestNFR_PERF_005|TestNFR_SIZE_003' ./internal/session", "BenchmarkNFR_PERF_005_EmulatorWrite")
	const tmpl = ".github/pull_request_template.md"
	wantAll(t, tmpl, readRootFile(t, tmpl), "emulator: throughput", "emulator: heap per session")
}

// TestNFR_PERF_007_CIRunsPerfSFTPJob: the SFTP throughput,
// responsiveness, listing and memory checks run without -race in a job
// of their own, retried once (specs/nfr.md §1), and the PR template
// records them (T-058).
func TestNFR_PERF_007_CIRunsPerfSFTPJob(t *testing.T) {
	wantAll(t, ciFile, readRootFile(t, ciFile), "perf-sftp:",
		"-run 'TestNFR_PERF_00[789]_|TestNFR_SIZE_004_' -skip 'TestNFR_PERF_008_SessionKeys' ./internal/files ./internal/ui ./cmd/sshelf",
		"-run 'TestNFR_PERF_008_SessionKeysDuringTransfers' ./cmd/sshelf",
		"failed once on this runner; retrying", "BenchmarkNFR_PERF_00[79]_",
		"continue-on-error: ${{ matrix.os != 'ubuntu-latest' }}")
	const tmpl = ".github/pull_request_template.md"
	wantAll(t, tmpl, readRootFile(t, tmpl), "sftp: throughput down / up", "heap peak, two transfers")
}

// TestNFR_REL_001_GoroutinePackagesCheckLeaks: every package that starts
// goroutines, and every package NFR-REL-001 names, calls
// goleak.VerifyTestMain (through testutil.VerifyTestMain) in its tests.
func TestNFR_REL_001_GoroutinePackagesCheckLeaks(t *testing.T) {
	root := moduleRoot(t)
	starts := map[string]bool{} // package dir -> starts goroutines
	verifies := map[string]bool{}
	for _, p := range []string{"internal/app", "internal/sshclient", "internal/session", "internal/secrets", "internal/ui"} {
		starts[p] = true
	}
	goStmt := regexp.MustCompile(`(?m)^\s*go (func|\w)`)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		b, err := os.ReadFile(path) //nolint:gosec // walking this repo
		if err != nil {
			return err
		}
		src := string(b)
		if strings.HasSuffix(path, "_test.go") {
			// cmd/sshelf's TestMain needs its own setup and calls
			// goleak.Find after m.Run, which is the same check.
			if strings.Contains(src, "VerifyTestMain(") || strings.Contains(src, "goleak.Find(") {
				verifies[rel] = true
			}
			return nil
		}
		if strings.Contains(src, "SafeGo(") || goStmt.MatchString(src) {
			starts[rel] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for p := range starts {
		if !verifies[p] && !strings.HasPrefix(p, "internal/testutil") {
			missing = append(missing, p)
		}
	}
	slices.Sort(missing)
	for _, p := range missing {
		t.Errorf("%s starts goroutines but its tests do not call VerifyTestMain", p)
	}
	if !verifies["internal/ui"] {
		t.Error("the scan found no VerifyTestMain call in internal/ui")
	}
}
