package main

import (
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// TestNFR_QUAL_001_EveryMVPAndM2IDTraced runs the gate over this module:
// every MVP and M2 requirement is Implemented, in its milestone's
// traceability matrix, and referenced by a test or listed as manual-only,
// and no M2 revision block is left.
func TestNFR_QUAL_001_EveryMVPAndM2IDTraced(t *testing.T) {
	res, err := Check(os.DirFS("../.."))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range res.Problems {
		t.Error(p)
	}
	// A parser that finds nothing would pass trivially. The plan counts 92
	// MVP requirements and 46 M2 ones once the 9 revisions are folded
	// (specs/tasks.md, the two traceability matrices).
	if n := len(res.MVP); n < 92 {
		t.Errorf("found %d MVP requirements, want at least 92", n)
	}
	if n := len(res.M2); n < 46 {
		t.Errorf("found %d M2 requirements, want at least 46", n)
	}
}

// fixture is a module with one requirement per heading style, and an M2
// requirement traced by a test and one listed as manual-only.
func fixture() fstest.MapFS {
	return fstest.MapFS{
		"specs/product/requirements.md": {Data: []byte(`
### FR-TEST-001 · Traced
- **Priority:** MVP · **Status:** Implemented

### FR-TEST-002 · Later
- **Priority:** Later · **Status:** Draft

### FR-TEST-003 · M2
- **Priority:** M2 · **Status:** Implemented
`)},
		"specs/security/requirements.md": {Data: []byte(`
**SEC-TEST-001: Manual.**
The system shall.
Priority: MVP · Status: Implemented · Threats: T-1

**SEC-TEST-002: Manual in M2.**
The system shall.
Priority: M2 · Status: Implemented · Threats: T-1
`)},
		"specs/architecture/design.md": {Data: []byte(`
**ARCH-TEST-001**: The system shall.
- Priority: MVP · Status: Implemented
`)},
		"specs/nfr.md": {Data: []byte(`
### NFR-TEST-001 · Check
- **Priority:** MVP · **Status:** Implemented
`)},
		"specs/tasks.md": {Data: []byte(`
## Manual-only requirements

| ID | Why |
|---|---|
| SEC-TEST-001 | needs a human |

## Traceability matrix

| ID | Tasks |
|---|---|
| FR-TEST-001 | T-1 |
| SEC-TEST-001 | T-1 |
| ARCH-TEST-001 | T-1 |
| NFR-TEST-001 | T-1 |

## M2 manual-only requirements

| ID | Why |
|---|---|
| SEC-TEST-002 | needs a human |

## M2 traceability matrix

| ID | Tasks |
|---|---|
| FR-TEST-003 | T-2 |
| SEC-TEST-002 | T-2 |
`)},
		"a/a_test.go":          {Data: []byte("func TestFR_TEST_001_X(t *testing.T) {}\n// ARCH-TEST-001\n")},
		"b/b_test.go":          {Data: []byte("func TestNFR_TEST_001(t *testing.T) {}\n")},
		"c/c_test.go":          {Data: []byte("func TestFR_TEST_003_M2(t *testing.T) {}\n")},
		"b/testdata/x_test.go": {Data: []byte("// FR-TEST-002\n")},
	}
}

// clone copies every file of f, so that edits do not leak between cases.
func clone(f fstest.MapFS) fstest.MapFS {
	for k, v := range f {
		c := *v
		f[k] = &c
	}
	return f
}

// replace replaces the first from by to in file name of f; from must be
// there.
func replace(t *testing.T, f fstest.MapFS, name, from, to string) {
	t.Helper()
	s := string(f[name].Data)
	if !strings.Contains(s, from) {
		t.Fatalf("%s lacks %q", name, from)
	}
	f[name].Data = []byte(strings.Replace(s, from, to, 1))
}

func TestNFR_QUAL_001_FixturePasses(t *testing.T) {
	res, err := Check(fixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) != 0 || len(res.MVP) != 4 || len(res.M2) != 2 || !slices.Equal(res.Manual, []string{"SEC-TEST-001", "SEC-TEST-002"}) {
		t.Fatalf("result %+v", res)
	}
}

// TestNFR_QUAL_001_M2MatrixRead: the M2 requirements are counted, and the
// M2 matrix and the M2 manual-only table are what make them pass.
func TestNFR_QUAL_001_M2MatrixRead(t *testing.T) {
	res, err := Check(fixture())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range res.M2 {
		ids = append(ids, r.ID)
	}
	if !slices.Equal(ids, []string{"FR-TEST-003", "SEC-TEST-002"}) || len(res.Problems) != 0 {
		t.Fatalf("M2 %v, problems %q", ids, res.Problems)
	}

	f := clone(fixture())
	replace(t, f, "specs/tasks.md", "## M2 traceability matrix", "## Some other section")
	replace(t, f, "specs/tasks.md", "## M2 manual-only requirements", "## Another section")
	res, err = Check(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`FR-TEST-003 is missing from the "M2 traceability matrix" section`,
		"SEC-TEST-002 is referenced by no *_test.go file and is not manual-only",
	} {
		if !slices.ContainsFunc(res.Problems, func(p string) bool { return strings.Contains(p, want) }) {
			t.Errorf("without the M2 tables, problems %q lack %q", res.Problems, want)
		}
	}
}

// TestNFR_QUAL_001_RevisionBlockFails: an M2 revision block left in any
// spec file fails the gate, with its file and line.
func TestNFR_QUAL_001_RevisionBlockFails(t *testing.T) {
	f := clone(fixture())
	f["specs/nfr.md"].Data = append(f["specs/nfr.md"].Data, []byte("- **M2 revision** · Priority: M2 · Status: Implemented · Source: D-1\n  - *Revised:* The system shall.\n")...)
	res, err := Check(f)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(res.Problems, "specs/nfr.md:4 holds an M2 revision block; fold it into its requirement") {
		t.Fatalf("problems %q", res.Problems)
	}
	// The block's own priority line does not change NFR-TEST-001's.
	if len(res.MVP) != 4 || len(res.M2) != 2 {
		t.Fatalf("MVP %v, M2 %v", res.MVP, res.M2)
	}
}

func TestNFR_QUAL_001_FailsEachRule(t *testing.T) {
	const (
		fr, sec, nfr, tasks = "specs/product/requirements.md", "specs/security/requirements.md", "specs/nfr.md", "specs/tasks.md"
	)
	for _, tc := range []struct {
		name string
		edit func(*testing.T, fstest.MapFS)
		want string
	}{
		{"draft", func(_ *testing.T, f fstest.MapFS) {
			f[nfr].Data = []byte("### NFR-TEST-001 · Check\n- **Priority:** MVP · **Status:** Draft\n")
		}, "NFR-TEST-001 (specs/nfr.md) is Draft, not Implemented"},
		{"M2 draft", func(t *testing.T, f fstest.MapFS) {
			replace(t, f, fr, "**Priority:** M2 · **Status:** Implemented", "**Priority:** M2 · **Status:** Draft")
		}, "FR-TEST-003 (specs/product/requirements.md) is Draft, not Implemented"},
		{"no status", func(_ *testing.T, f fstest.MapFS) {
			f[nfr].Data = []byte("### NFR-TEST-001 · Check\n- **Priority:** MVP\n")
		}, "NFR-TEST-001 (specs/nfr.md) has no Status"},
		{"untraced", func(_ *testing.T, f fstest.MapFS) { delete(f, "b/b_test.go") },
			"NFR-TEST-001 is referenced by no *_test.go file"},
		{"M2 untraced", func(_ *testing.T, f fstest.MapFS) { delete(f, "c/c_test.go") },
			"FR-TEST-003 is referenced by no *_test.go file"},
		{"testdata does not count", func(t *testing.T, f fstest.MapFS) {
			replace(t, f, fr, "- **Priority:** Later · **Status:** Draft", "- **Priority:** MVP · **Status:** Implemented")
			replace(t, f, tasks, "| NFR-TEST-001 | T-1 |\n", "| NFR-TEST-001 | T-1 |\n| FR-TEST-002 | T-1 |\n")
		}, "FR-TEST-002 is referenced by no *_test.go file"},
		{"not in matrix", func(t *testing.T, f fstest.MapFS) {
			replace(t, f, tasks, "| ARCH-TEST-001 | T-1 |\n", "")
		}, `ARCH-TEST-001 is missing from the "Traceability matrix" section`},
		{"M2 matrix lists a v1 ID", func(_ *testing.T, f fstest.MapFS) {
			f[tasks].Data = append(f[tasks].Data, []byte("| FR-TEST-002 | T-1 |\n")...)
		}, `FR-TEST-002 is in the "M2 traceability matrix" section but is not an M2 requirement`},
		{"MVP matrix lists an M2 ID", func(t *testing.T, f fstest.MapFS) {
			replace(t, f, tasks, "| NFR-TEST-001 | T-1 |\n", "| NFR-TEST-001 | T-1 |\n| FR-TEST-003 | T-1 |\n")
		}, `FR-TEST-003 is in the "Traceability matrix" section but is not an MVP requirement`},
		{"M2 manual-only lists an MVP ID", func(t *testing.T, f fstest.MapFS) {
			replace(t, f, tasks, "| SEC-TEST-002 | needs a human |\n", "| SEC-TEST-002 | needs a human |\n| FR-TEST-001 | no |\n")
		}, `FR-TEST-001 is listed in "M2 manual-only requirements" but is not an M2 requirement`},
		{"manual-only must still be implemented", func(_ *testing.T, f fstest.MapFS) {
			f[sec].Data = []byte("**SEC-TEST-001: M.**\nPriority: MVP · Status: Draft\n")
		}, "SEC-TEST-001 (specs/security/requirements.md) is Draft"},
		{"M2 manual-only must still be implemented", func(t *testing.T, f fstest.MapFS) {
			replace(t, f, sec, "Priority: M2 · Status: Implemented", "Priority: M2 · Status: Draft")
		}, "SEC-TEST-002 (specs/security/requirements.md) is Draft"},
		{"duplicate", func(_ *testing.T, f fstest.MapFS) {
			f[nfr].Data = append(f[nfr].Data, []byte("### FR-TEST-001 · Again\n- **Priority:** MVP · **Status:** Implemented\n")...)
		}, "FR-TEST-001 is defined twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := clone(fixture())
			tc.edit(t, f)
			res, err := Check(f)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(res.Problems, func(p string) bool { return strings.Contains(p, tc.want) }) {
				t.Fatalf("problems %q lack %q", res.Problems, tc.want)
			}
		})
	}
}
