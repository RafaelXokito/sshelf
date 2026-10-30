// Command tracecheck is the NFR-QUAL-001 gate. It reads every MVP
// requirement (FR-*, SEC-*, ARCH-*, NFR-*) from the spec files and fails
// when one of them
//
//   - is not Implemented (or Verified),
//   - is missing from the traceability matrix in specs/tasks.md, or
//   - is referenced by no *_test.go file (as FR-X-NNN or FR_X_NNN, the
//     form test names use), unless specs/tasks.md lists it under
//     "Manual-only requirements".
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

var (
	// idHeading matches the three heading styles the spec files use:
	// "### FR-HOST-001 · …", "**SEC-CRED-001: …**" and "**ARCH-SEC-001**: …".
	idHeading = regexp.MustCompile(`^(?:###\s+|\*\*)((?:FR|SEC|ARCH|NFR)-[A-Z]+-\d{3})\b`)
	priority  = regexp.MustCompile(`Priority:?\**:?\s*(\w+)`)
	status    = regexp.MustCompile(`Status:?\**:?\s*(\w+)`)
	tableRow  = regexp.MustCompile(`^\|\s*((?:FR|SEC|ARCH|NFR)-[A-Z]+-\d{3})\s*\|`)
	anyID     = regexp.MustCompile(`(?:FR|SEC|ARCH|NFR)[-_][A-Z]+[-_]\d{3}`)
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
	Manual   []string // MVP IDs listed as manual-only
	Problems []string
}

// Check runs the gate over the module rooted at fsys.
func Check(fsys fs.FS) (Result, error) {
	var res Result
	reqs := map[string]Requirement{}
	for _, f := range specFiles {
		found, err := parseSpec(fsys, f)
		if err != nil {
			return res, err
		}
		for _, r := range found {
			if prev, dup := reqs[r.ID]; dup {
				res.Problems = append(res.Problems, fmt.Sprintf("%s is defined twice (%s and %s)", r.ID, prev.File, r.File))
				continue
			}
			reqs[r.ID] = r
		}
	}

	matrix, manual, err := parseTasks(fsys)
	if err != nil {
		return res, err
	}
	refs, err := testRefs(fsys)
	if err != nil {
		return res, err
	}

	for _, id := range sortedKeys(reqs) {
		r := reqs[id]
		if r.Priority != "MVP" {
			continue
		}
		res.MVP = append(res.MVP, r)
		switch r.Status {
		case "Implemented", "Verified":
		case "":
			res.Problems = append(res.Problems, fmt.Sprintf("%s (%s) has no Status", id, r.File))
		default:
			res.Problems = append(res.Problems, fmt.Sprintf("%s (%s) is %s, not Implemented", id, r.File, r.Status))
		}
		if !matrix[id] {
			res.Problems = append(res.Problems, fmt.Sprintf("%s is missing from the traceability matrix in %s", id, tasksFile))
		}
		switch {
		case manual[id]:
			res.Manual = append(res.Manual, id)
		case !refs[id]:
			res.Problems = append(res.Problems, fmt.Sprintf("%s is referenced by no *_test.go file and is not manual-only", id))
		}
	}
	for _, id := range sortedKeys(matrix) {
		if r, ok := reqs[id]; !ok || r.Priority != "MVP" {
			res.Problems = append(res.Problems, fmt.Sprintf("%s is in the traceability matrix but is not an MVP requirement", id))
		}
	}
	for _, id := range sortedKeys(manual) {
		if r, ok := reqs[id]; !ok || r.Priority != "MVP" {
			res.Problems = append(res.Problems, fmt.Sprintf("%s is listed as manual-only but is not an MVP requirement", id))
		}
	}
	return res, nil
}

// parseSpec returns the requirements defined in name. A requirement's
// priority and status are on the first line after its heading that
// names a priority.
func parseSpec(fsys fs.FS, name string) ([]Requirement, error) {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, err
	}
	var out []Requirement
	var cur *Requirement
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		line := sc.Text()
		if m := idHeading.FindStringSubmatch(line); m != nil {
			out = append(out, Requirement{ID: m[1], File: name})
			cur = &out[len(out)-1]
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
	return out, sc.Err()
}

// parseTasks returns the IDs in the traceability matrix and in the
// "Manual-only requirements" table of specs/tasks.md.
func parseTasks(fsys fs.FS) (matrix, manual map[string]bool, err error) {
	b, err := fs.ReadFile(fsys, tasksFile)
	if err != nil {
		return nil, nil, err
	}
	matrix, manual = map[string]bool{}, map[string]bool{}
	var section string
	for line := range strings.SplitSeq(string(b), "\n") {
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		m := tableRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		switch section {
		case "Traceability matrix":
			matrix[m[1]] = true
		case "Manual-only requirements":
			manual[m[1]] = true
		}
	}
	return matrix, manual, nil
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
	fmt.Printf("tracecheck: %d MVP requirements, %d manual-only, %d problems\n", len(res.MVP), len(res.Manual), len(res.Problems))
	if len(res.Problems) > 0 {
		os.Exit(1)
	}
}
