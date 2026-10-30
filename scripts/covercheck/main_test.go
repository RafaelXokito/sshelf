package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// profile builds a coverage profile line by line: pkg, statements,
// count.
func profile(lines ...string) string {
	return "mode: atomic\n" + strings.Join(lines, "\n") + "\n"
}

func line(pkg string, n, stmts, count int) string {
	return fmt.Sprintf("%s%s/f.go:%d.1,%d.2 %d %d", module, pkg, n, n+1, stmts, count)
}

// allCore returns lines that put every core package at pct percent (of
// 10 statements).
func allCore(pct int) []string {
	var out []string
	for _, p := range corePackages {
		out = append(out, line(p, 1, pct/10, 1), line(p, 2, 10-pct/10, 0))
	}
	return out
}

func gate(t *testing.T, prof string) []string {
	t.Helper()
	pkgs, err := Parse(strings.NewReader(prof))
	if err != nil {
		t.Fatal(err)
	}
	_, failures := Gate(pkgs)
	return failures
}

func TestNFR_QUAL_003_PassesAtThresholds(t *testing.T) {
	if f := gate(t, profile(allCore(80)...)); len(f) != 0 {
		t.Fatalf("failures %q", f)
	}
}

func TestNFR_QUAL_003_CorePackageBelow80Fails(t *testing.T) {
	lines := allCore(90)
	lines[0], lines[1] = line("internal/domain", 1, 7, 1), line("internal/domain", 2, 3, 0)
	f := gate(t, profile(lines...))
	if !slices.ContainsFunc(f, func(s string) bool { return strings.HasPrefix(s, "internal/domain: 70.0%") }) {
		t.Fatalf("failures %q", f)
	}
}

func TestNFR_QUAL_003_ModuleBelow70Fails(t *testing.T) {
	lines := append(allCore(100), line("internal/ui", 1, 1000, 0))
	f := gate(t, profile(lines...))
	if !slices.ContainsFunc(f, func(s string) bool { return strings.HasPrefix(s, "module:") }) {
		t.Fatalf("failures %q", f)
	}
}

func TestNFR_QUAL_003_MissingCorePackageFails(t *testing.T) {
	f := gate(t, profile(allCore(100)[2:]...))
	if !slices.Contains(f, "internal/domain: not in the profile") {
		t.Fatalf("failures %q", f)
	}
}

// With -coverpkg each test binary lists every block; a block is covered
// if any binary ran it, and its statements count once.
func TestNFR_QUAL_003_DuplicateBlocksMerge(t *testing.T) {
	pkgs, err := Parse(strings.NewReader(profile(
		line("internal/ui", 1, 4, 0), line("internal/ui", 1, 4, 3), line("internal/ui", 2, 6, 0),
	)))
	if err != nil {
		t.Fatal(err)
	}
	if c := pkgs["internal/ui"]; c != (Coverage{Covered: 4, Total: 10}) {
		t.Fatalf("coverage %+v", c)
	}
}

func TestNFR_QUAL_003_TestHelpersExcluded(t *testing.T) {
	lines := append(allCore(100), line("internal/testutil/sshtest", 1, 10000, 0))
	if f := gate(t, profile(lines...)); len(f) != 0 {
		t.Fatalf("failures %q", f)
	}
}

func TestNFR_QUAL_003_BadLineRejected(t *testing.T) {
	if _, err := Parse(strings.NewReader("mode: set\nnot a profile line\n")); err == nil {
		t.Fatal("no error")
	}
}
