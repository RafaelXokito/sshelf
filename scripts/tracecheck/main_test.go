package main

import (
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// TestNFR_QUAL_001_EveryMVPIDTraced runs the gate over this module: every
// MVP requirement is Implemented, in the traceability matrix, and
// referenced by a test or listed as manual-only.
func TestNFR_QUAL_001_EveryMVPIDTraced(t *testing.T) {
	res, err := Check(os.DirFS("../.."))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range res.Problems {
		t.Error(p)
	}
	// A parser that finds nothing would pass trivially; the plan counts
	// 101 MVP requirements (specs/tasks.md, traceability matrix).
	if n := len(res.MVP); n < 101 {
		t.Errorf("found %d MVP requirements, want at least 101", n)
	}
}

// fixture is a module with one requirement per heading style.
func fixture() fstest.MapFS {
	return fstest.MapFS{
		"specs/product/requirements.md": {Data: []byte(`
### FR-TEST-001 · Traced
- **Priority:** MVP · **Status:** Implemented

### FR-TEST-002 · Later
- **Priority:** Later · **Status:** Draft
`)},
		"specs/security/requirements.md": {Data: []byte(`
**SEC-TEST-001: Manual.**
The system shall.
Priority: MVP · Status: Implemented · Threats: T-1
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
`)},
		"a/a_test.go":          {Data: []byte("func TestFR_TEST_001_X(t *testing.T) {}\n// ARCH-TEST-001\n")},
		"b/b_test.go":          {Data: []byte("func TestNFR_TEST_001(t *testing.T) {}\n")},
		"b/testdata/x_test.go": {Data: []byte("// FR-TEST-002\n")},
	}
}

func TestNFR_QUAL_001_FixturePasses(t *testing.T) {
	res, err := Check(fixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) != 0 || len(res.MVP) != 4 || !slices.Equal(res.Manual, []string{"SEC-TEST-001"}) {
		t.Fatalf("result %+v", res)
	}
}

func TestNFR_QUAL_001_FailsEachRule(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(fstest.MapFS)
		want string
	}{
		{"draft", func(f fstest.MapFS) {
			f["specs/nfr.md"].Data = []byte("### NFR-TEST-001 · Check\n- **Priority:** MVP · **Status:** Draft\n")
		}, "NFR-TEST-001 (specs/nfr.md) is Draft, not Implemented"},
		{"no status", func(f fstest.MapFS) {
			f["specs/nfr.md"].Data = []byte("### NFR-TEST-001 · Check\n- **Priority:** MVP\n")
		}, "NFR-TEST-001 (specs/nfr.md) has no Status"},
		{"untraced", func(f fstest.MapFS) { delete(f, "b/b_test.go") },
			"NFR-TEST-001 is referenced by no *_test.go file"},
		{"testdata does not count", func(f fstest.MapFS) {
			f["specs/product/requirements.md"].Data = []byte("### FR-TEST-001 · T\n- **Priority:** MVP · **Status:** Implemented\n### FR-TEST-002 · L\n- **Priority:** MVP · **Status:** Implemented\n")
			f["specs/tasks.md"].Data = append(f["specs/tasks.md"].Data, []byte("| FR-TEST-002 | T-1 |\n")...)
		}, "FR-TEST-002 is referenced by no *_test.go file"},
		{"not in matrix", func(f fstest.MapFS) {
			f["specs/tasks.md"].Data = []byte(strings.Replace(string(f["specs/tasks.md"].Data), "| ARCH-TEST-001 | T-1 |\n", "", 1))
		}, "ARCH-TEST-001 is missing from the traceability matrix"},
		{"matrix lists a v1 ID", func(f fstest.MapFS) {
			f["specs/tasks.md"].Data = append(f["specs/tasks.md"].Data, []byte("| FR-TEST-002 | T-1 |\n")...)
		}, "FR-TEST-002 is in the traceability matrix but is not an MVP requirement"},
		{"manual-only must still be implemented", func(f fstest.MapFS) {
			f["specs/security/requirements.md"].Data = []byte("**SEC-TEST-001: M.**\nPriority: MVP · Status: Draft\n")
		}, "SEC-TEST-001 (specs/security/requirements.md) is Draft"},
		{"duplicate", func(f fstest.MapFS) {
			f["specs/nfr.md"].Data = append(f["specs/nfr.md"].Data, []byte("### FR-TEST-001 · Again\n- **Priority:** MVP · **Status:** Implemented\n")...)
		}, "FR-TEST-001 is defined twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture()
			for k, v := range f { // the edits must not leak between cases
				c := *v
				f[k] = &c
			}
			tc.edit(f)
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
