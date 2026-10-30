// Command covercheck is the NFR-QUAL-003 gate. It reads a coverage
// profile written by
//
//	go test -coverpkg=./... -coverprofile=coverage.out ./...
//
// and fails when statement coverage is below 80 % in any of the core
// packages or below 70 % across the module. -coverpkg=./... counts a
// statement as covered when any package's tests run it, so the screens
// and modals that internal/ui's tests drive through the root model count.
// The test helpers under internal/testutil are not part of sshelf and are
// left out of the module total.
//
// Usage: go run ./scripts/covercheck coverage.out
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
)

const module = "github.com/RafaelXokito/sshelf/"

// corePackages must each reach coreMin (NFR-QUAL-003).
var corePackages = []string{
	"internal/domain", "internal/store", "internal/secrets", "internal/paths",
	"internal/sshclient", "internal/sshconfig", "internal/search",
	"internal/files", // M3 (T-059)
}

const (
	coreMin   = 80.0
	moduleMin = 70.0
)

// excluded reports whether pkg is left out of the module total.
func excluded(pkg string) bool {
	return pkg == "internal/testutil" || strings.HasPrefix(pkg, "internal/testutil/")
}

// Coverage is the statement count of one package.
type Coverage struct{ Covered, Total int }

// Percent is the share of covered statements; 0 for no statements.
func (c Coverage) Percent() float64 {
	if c.Total == 0 {
		return 0
	}
	return 100 * float64(c.Covered) / float64(c.Total)
}

// Parse reads a coverage profile and returns the statements per package
// (relative to the module). A block listed more than once (each test
// binary reports every package under -coverpkg) is covered if any listing
// has a count above zero.
func Parse(r io.Reader) (map[string]Coverage, error) {
	type block struct {
		stmts   int
		covered bool
	}
	blocks := map[string]*block{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if first {
			first = false
			if strings.HasPrefix(line, "mode:") {
				continue
			}
		}
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		// file.go:12.34,15.2 3 1
		f := strings.Fields(line)
		if len(f) != 3 || !strings.Contains(f[0], ":") {
			return nil, fmt.Errorf("bad profile line %q", line)
		}
		stmts, err1 := strconv.Atoi(f[1])
		count, err2 := strconv.Atoi(f[2])
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("bad profile line %q", line)
		}
		b := blocks[f[0]]
		if b == nil {
			b = &block{stmts: stmts}
			blocks[f[0]] = b
		}
		b.covered = b.covered || count > 0
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	out := map[string]Coverage{}
	for pos, b := range blocks {
		file := pos[:strings.LastIndex(pos, ":")]
		pkg := strings.TrimPrefix(path.Dir(file), module)
		c := out[pkg]
		c.Total += b.stmts
		if b.covered {
			c.Covered += b.stmts
		}
		out[pkg] = c
	}
	return out, nil
}

// Gate returns the report lines and the failures for pkgs.
func Gate(pkgs map[string]Coverage) (report, failures []string) {
	for _, p := range corePackages {
		c, ok := pkgs[p]
		if !ok {
			failures = append(failures, fmt.Sprintf("%s: not in the profile", p))
			continue
		}
		report = append(report, fmt.Sprintf("%-22s %5.1f%% (min %.0f%%)", p, c.Percent(), coreMin))
		if c.Percent() < coreMin {
			failures = append(failures, fmt.Sprintf("%s: %.1f%% < %.0f%%", p, c.Percent(), coreMin))
		}
	}
	var total Coverage
	for p, c := range pkgs {
		if !excluded(p) {
			total.Covered += c.Covered
			total.Total += c.Total
		}
	}
	report = append(report, fmt.Sprintf("%-22s %5.1f%% (min %.0f%%)", "module", total.Percent(), moduleMin))
	if total.Percent() < moduleMin {
		failures = append(failures, fmt.Sprintf("module: %.1f%% < %.0f%%", total.Percent(), moduleMin))
	}
	return report, failures
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: covercheck coverage.out")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1]) //nolint:gosec // the profile the developer or CI names
	if err != nil {
		fmt.Fprintln(os.Stderr, "covercheck:", err)
		os.Exit(2)
	}
	pkgs, err := Parse(f)
	_ = f.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "covercheck:", err)
		os.Exit(2)
	}
	report, failures := Gate(pkgs)
	for _, l := range report {
		fmt.Println(l)
	}
	for _, l := range failures {
		fmt.Println("FAIL", l)
	}
	if len(failures) > 0 {
		os.Exit(1)
	}
}
