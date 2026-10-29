package archtest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// buildTags makes go list also see the files behind the devcheck tag, so the
// bans cover them too.
const buildTags = "devcheck"

var bubbleTea = []string{"charm.land/bubbletea/v2/...", "github.com/charmbracelet/bubbletea/..."}

// rule is one row of the design §5.2 table. Patterns starting with
// "internal/" are relative to the module; a trailing "/..." also matches
// subpackages.
type rule struct {
	pkg string
	// allow lists the internal packages pkg may import. nil means any.
	allow []string
	// forbid lists packages pkg must not import directly.
	forbid []string
}

var rules = []rule{
	{pkg: "internal/domain", allow: []string{}, forbid: []string{"golang.org/x/crypto/ssh/..."}},
	{pkg: "internal/store", allow: []string{"internal/domain"}, forbid: []string{"internal/ui/...", "internal/app", "internal/sshclient", "internal/session"}},
	{pkg: "internal/secrets", allow: []string{"internal/domain"}, forbid: []string{"internal/ui/...", "internal/app", "internal/sshclient", "internal/session"}},
	{pkg: "internal/sshclient", allow: []string{"internal/domain"}, forbid: append([]string{"internal/secrets", "internal/store", "internal/ui/...", "internal/app"}, bubbleTea...)},
	{pkg: "internal/session", allow: []string{"internal/sshclient", "internal/term"}, forbid: append([]string{"internal/secrets", "internal/store", "internal/ui/...", "internal/app"}, bubbleTea...)},
	{pkg: "internal/ui/...", allow: []string{"internal/app", "internal/domain", "internal/search", "internal/session", "internal/ui/..."}, forbid: []string{"internal/secrets", "internal/store", "internal/sshclient", "golang.org/x/crypto/ssh/..."}},
	{pkg: "internal/app", forbid: []string{"internal/ui/..."}},
}

// Every sshelf package: no child processes (ARCH-SESS-001, SEC-MEM-008) and
// no clipboard (SEC-CLIP-005).
var (
	osExec    = "os/exec"
	clipboard = []string{"clipboard", "osc52"} // substrings of known clipboard package paths
)

type pkg struct {
	ImportPath string
	Imports    []string
	Module     *struct{ Path string }
}

// load runs go list -deps in dir and returns the module path and the
// module's own packages.
func load(t *testing.T, dir string) (string, []pkg) {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", "-json", "-tags", buildTags, "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list in %s: %v\n%s", dir, err, stderr.String())
	}
	modCmd := exec.Command("go", "list", "-m")
	modCmd.Dir = dir
	modCmd.Env = cmd.Env
	modOut, err := modCmd.Output()
	if err != nil {
		t.Fatalf("go list -m in %s: %v", dir, err)
	}
	module := strings.TrimSpace(string(modOut))

	var pkgs []pkg
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p pkg
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if p.Module != nil && p.Module.Path == module {
			pkgs = append(pkgs, p)
		}
	}
	if len(pkgs) == 0 {
		t.Fatalf("go list in %s found no packages of %s", dir, module)
	}
	return module, pkgs
}

// match reports whether path matches pattern, resolving "internal/"
// patterns against module.
func match(module, pattern, path string) bool {
	if strings.HasPrefix(pattern, "internal/") {
		pattern = module + "/" + pattern
	}
	if base, ok := strings.CutSuffix(pattern, "/..."); ok {
		return path == base || strings.HasPrefix(path, base+"/")
	}
	return path == pattern
}

func matchAny(module string, patterns []string, path string) bool {
	return slices.ContainsFunc(patterns, func(p string) bool { return match(module, p, path) })
}

// violations returns every forbidden edge among pkgs.
func violations(module string, pkgs []pkg) []string {
	var out []string
	rel := func(p string) string { return strings.TrimPrefix(p, module+"/") }
	for _, p := range pkgs {
		for _, imp := range p.Imports {
			if imp == osExec {
				out = append(out, fmt.Sprintf("%s imports %s", rel(p.ImportPath), imp))
			}
			for _, c := range clipboard {
				if strings.Contains(imp, c) {
					out = append(out, fmt.Sprintf("%s imports clipboard package %s", rel(p.ImportPath), imp))
				}
			}
			for _, r := range rules {
				if !match(module, r.pkg, p.ImportPath) {
					continue
				}
				internal := strings.HasPrefix(imp, module+"/internal/")
				switch {
				case matchAny(module, r.forbid, imp):
					out = append(out, fmt.Sprintf("%s imports %s", rel(p.ImportPath), rel(imp)))
				case internal && r.allow != nil && !matchAny(module, r.allow, imp):
					out = append(out, fmt.Sprintf("%s imports %s, which is not in its allowed list", rel(p.ImportPath), rel(imp)))
				}
			}
		}
	}
	return out
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not found: %v", err)
	}
	return root
}

func TestARCH_PKG_001_ImportRules(t *testing.T) {
	module, pkgs := load(t, moduleRoot(t))
	for _, v := range violations(module, pkgs) {
		t.Error(v)
	}
}

func TestARCH_PKG_001_ForbiddenImportFails(t *testing.T) {
	module, pkgs := load(t, filepath.Join("testdata", "forbidden"))
	got := violations(module, pkgs)
	want := "internal/domain imports internal/store, which is not in its allowed list"
	if !slices.Contains(got, want) {
		t.Fatalf("violations = %q, want it to contain %q", got, want)
	}
}

// osExecImporters returns the sshelf packages that import os/exec.
func osExecImporters(t *testing.T) []string {
	t.Helper()
	_, pkgs := load(t, moduleRoot(t))
	var bad []string
	for _, p := range pkgs {
		if slices.Contains(p.Imports, osExec) {
			bad = append(bad, p.ImportPath)
		}
	}
	return bad
}

func TestARCH_SESS_001_NoOsExec(t *testing.T) {
	if bad := osExecImporters(t); len(bad) > 0 {
		t.Fatalf("packages importing os/exec: %v", bad)
	}
}

func TestSEC_MEM_008_NoOsExecImport(t *testing.T) {
	if bad := osExecImporters(t); len(bad) > 0 {
		t.Fatalf("packages importing os/exec: %v", bad)
	}
}

func TestSEC_CLIP_005_NoClipboardPackage(t *testing.T) {
	_, pkgs := load(t, moduleRoot(t))
	for _, p := range pkgs {
		for _, imp := range p.Imports {
			for _, c := range clipboard {
				if strings.Contains(imp, c) {
					t.Errorf("%s imports clipboard package %s", p.ImportPath, imp)
				}
			}
		}
	}
}

func TestARCH_SEC_002_SSHClientDoesNotImportSecrets(t *testing.T) {
	module, pkgs := load(t, moduleRoot(t))
	i := slices.IndexFunc(pkgs, func(p pkg) bool { return p.ImportPath == module+"/internal/sshclient" })
	if i < 0 {
		t.Fatal("internal/sshclient not found")
	}
	for _, imp := range pkgs[i].Imports {
		if match(module, "internal/secrets", imp) || match(module, "internal/store", imp) {
			t.Errorf("internal/sshclient imports %s", imp)
		}
	}
}

func TestSEC_HOSTKEY_001_NoInsecureIgnoreHostKey(t *testing.T) {
	needle := []byte("Insecure" + "IgnoreHostKey")
	root := moduleRoot(t)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path) //nolint:gosec // paths come from walking the module
		if err != nil {
			return err
		}
		if bytes.Contains(b, needle) {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s uses %s outside a _test.go file", rel, needle)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
