// Command tracecheck is the NFR-QUAL-001 gate. It reads every MVP, M2
// and M3 requirement (FR-*, SEC-*, ARCH-*, NFR-*) from the spec files and fails
// when one of them
//
//   - is not Implemented (or Verified),
//   - is missing from its milestone's traceability matrix in
//     specs/tasks.md ("Traceability matrix" for MVP, "M2 traceability
//     matrix" for M2, "M3 traceability matrix" for M3), or
//   - is referenced by no *_test.go file (as FR-X-NNN or FR_X_NNN, the
//     form test names use), unless specs/tasks.md lists it under its
//     milestone's manual-only table ("Manual-only requirements", "M2
//     manual-only requirements" or "M3 manual-only requirements").
//
// It also fails when a matrix or manual-only table lists an ID that is not
// a requirement of that milestone, and when a spec file still holds an
// "M2 revision" or "M3 revision" block (specs/README.md, "Revisions":
// each milestone's gate folds every revision into its requirement).
//
// Run it from the module root: go run ./scripts/tracecheck
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
)

// specFiles are the files that define requirements, relative to the
// module root.
var specFiles = []string{
	"specs/product/requirements.md",
	"specs/security/requirements.md",
	"specs/architecture/design.md",
	"specs/nfr.md",
}

const tasksFile = "specs/tasks.md"

// milestones are the priorities the gate checks, and the specs/tasks.md
// sections (level-2 headings) that hold each one's traceability matrix
// and manual-only table.
var milestones = []struct {
	Priority, Matrix, Manual string
}{
	{"MVP", "Traceability matrix", "Manual-only requirements"},
	{"M2", "M2 traceability matrix", "M2 manual-only requirements"},
	{"M3", "M3 traceability matrix", "M3 manual-only requirements"},
}

var (
	// idHeading matches the three heading styles the spec files use:
	// "### FR-HOST-001 · …", "**SEC-CRED-001: …**" and "**ARCH-SEC-001**: …".
	idHeading = regexp.MustCompile(`^(?:###\s+|\*\*)((?:FR|SEC|ARCH|NFR)-[A-Z]+-\d{3})\b`)
	priority  = regexp.MustCompile(`Priority:?\**:?\s*(\w+)`)
	status    = regexp.MustCompile(`Status:?\**:?\s*(\w+)`)
	tableRow  = regexp.MustCompile(`^\|\s*((?:FR|SEC|ARCH|NFR)-[A-Z]+-\d{3})\s*\|`)
	anyID     = regexp.MustCompile(`(?:FR|SEC|ARCH|NFR)[-_][A-Z]+[-_]\d{3}`)
	// revision matches the first line of a revision block
	// ("- **M2 revision** · Priority: M2 · …", or M3).
	revision = regexp.MustCompile(`^\s*-\s+\*\*(M2|M3) revision\*\*`)
)

// Requirement is one requirement as the spec files define it.
type Requirement struct {
	ID       string
	File     string
	Priority string
	Status   string
}

// Result is what Check found.
type Result struct {
	MVP      []Requirement
	M2       []Requirement
	M3       []Requirement
	Manual   []string // MVP, M2 and M3 IDs listed as manual-only
	Problems []string
}

// Check runs the gate over the module rooted at fsys.
func Check(fsys fs.FS) (Result, error) {
	var res Result
	reqs := map[string]Requirement{}
	for _, f := range specFiles {
		found, revs, err := parseSpec(fsys, f)
		if err != nil {
			return res, err
		}
		for _, rv := range revs {
			res.Problems = append(res.Problems, fmt.Sprintf("%s:%d holds an %s revision block; fold it into its requirement", f, rv.Line, rv.Milestone))
		}
		for _, r := range found {
			if prev, dup := reqs[r.ID]; dup {
				res.Problems = append(res.Problems, fmt.Sprintf("%s is defined twice (%s and %s)", r.ID, prev.File, r.File))
				continue
			}
			reqs[r.ID] = r
		}
	}

	sections, err := parseTasks(fsys)
	if err != nil {
		return res, err
	}
	refs, err := testRefs(fsys)
	if err != nil {
		return res, err
	}

	for _, ms := range milestones {
		matrix, manual := sections[ms.Matrix], sections[ms.Manual]
		for _, id := range sortedKeys(reqs) {
			r := reqs[id]
			if r.Priority != ms.Priority {
				continue
			}
			switch ms.Priority {
			case "MVP":
				res.MVP = append(res.MVP, r)
			case "M2":
				res.M2 = append(res.M2, r)
			default:
				res.M3 = append(res.M3, r)
			}
			switch r.Status {
			case "Implemented", "Verified":
			case "":
				res.Problems = append(res.Problems, fmt.Sprintf("%s (%s) has no Status", id, r.File))
			default:
				res.Problems = append(res.Problems, fmt.Sprintf("%s (%s) is %s, not Implemented", id, r.File, r.Status))
			}
			if !matrix[id] {
				res.Problems = append(res.Problems, fmt.Sprintf("%s is missing from the %q section of %s", id, ms.Matrix, tasksFile))
			}
			switch {
			case manual[id]:
				res.Manual = append(res.Manual, id)
			case !refs[id]:
				res.Problems = append(res.Problems, fmt.Sprintf("%s is referenced by no *_test.go file and is not manual-only", id))
			}
		}
		for _, id := range sortedKeys(matrix) {
			if r, ok := reqs[id]; !ok || r.Priority != ms.Priority {
				res.Problems = append(res.Problems, fmt.Sprintf("%s is in the %q section but is not an %s requirement", id, ms.Matrix, ms.Priority))
			}
		}
		for _, id := range sortedKeys(manual) {
			if r, ok := reqs[id]; !ok || r.Priority != ms.Priority {
				res.Problems = append(res.Problems, fmt.Sprintf("%s is listed in %q but is not an %s requirement", id, ms.Manual, ms.Priority))
			}
		}
	}
	slices.Sort(res.Manual)
	return res, nil
}

// revisionBlock is a revision block left in a spec file.
type revisionBlock struct {
	Line      int
	Milestone string // "M2" or "M3"
}

// parseSpec returns the requirements defined in name, and any revision
// blocks. A requirement's priority and status are on the first line after
// its heading that names a priority.
func parseSpec(fsys fs.FS, name string) (reqs []Requirement, revisions []revisionBlock, err error) {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, nil, err
	}
	var cur *Requirement
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if m := revision.FindStringSubmatch(line); m != nil {
			revisions = append(revisions, revisionBlock{Line: n, Milestone: m[1]})
			continue
		}
		if m := idHeading.FindStringSubmatch(line); m != nil {
			reqs = append(reqs, Requirement{ID: m[1], File: name})
			cur = &reqs[len(reqs)-1]
			continue
		}
		if cur == nil || cur.Priority != "" || !strings.Contains(line, "Priority") {
			continue
		}
		if m := priority.FindStringSubmatch(line); m != nil {
			cur.Priority = m[1]
		}
		if m := status.FindStringSubmatch(line); m != nil {
			cur.Status = m[1]
		}
	}
	return reqs, revisions, sc.Err()
}

// parseTasks returns, for each level-2 section of specs/tasks.md that
// a milestone names, the IDs in the first column of its tables.
func parseTasks(fsys fs.FS) (map[string]map[string]bool, error) {
	b, err := fs.ReadFile(fsys, tasksFile)
	if err != nil {
		return nil, err
	}
	sections := map[string]map[string]bool{}
	for _, ms := range milestones {
		sections[ms.Matrix], sections[ms.Manual] = map[string]bool{}, map[string]bool{}
	}
	var ids map[string]bool
	for line := range strings.SplitSeq(string(b), "\n") {
		if strings.HasPrefix(line, "## ") {
			ids = sections[strings.TrimSpace(strings.TrimPrefix(line, "## "))]
			continue
		}
		if m := tableRow.FindStringSubmatch(line); m != nil && ids != nil {
			ids[m[1]] = true
		}
	}
	return sections, nil
}

// testRefs returns every requirement ID that a *_test.go file mentions,
// in either the FR-X-NNN or the FR_X_NNN form, normalised to dashes.
// testdata and hidden directories are skipped. (This command's own
// fixtures use a TEST area that no real requirement has.)
func testRefs(fsys fs.FS) (map[string]bool, error) {
	refs := map[string]bool{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if p != "." && (name == "testdata" || strings.HasPrefix(name, ".")) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path.Base(p), "_test.go") {
			return nil
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		for _, m := range anyID.FindAllString(string(b), -1) {
			refs[strings.ReplaceAll(m, "_", "-")] = true
		}
		return nil
	})
	return refs, err
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func main() {
	root := flag.String("root", ".", "module root")
	flag.Parse()
	res, err := Check(os.DirFS(*root))
	if err != nil {
		fmt.Fprintln(os.Stderr, "tracecheck:", err)
		os.Exit(2)
	}
	for _, p := range res.Problems {
		fmt.Println("FAIL", p)
	}
	fmt.Printf("tracecheck: %d MVP, %d M2 and %d M3 requirements, %d manual-only, %d problems\n", len(res.MVP), len(res.M2), len(res.M3), len(res.Manual), len(res.Problems))
	if len(res.Problems) > 0 {
		os.Exit(1)
	}
}
