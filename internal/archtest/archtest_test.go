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

// buildTags makes go list also see the files behind the
// sshelf_faultinject tag, so the bans cover them too.
const buildTags = "sshelf_faultinject"

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
	{pkg: "internal/secrets", allow: []string{"internal/domain", "internal/term"}, forbid: []string{"internal/ui/...", "internal/app", "internal/sshclient", "internal/session"}},
	// sshclient starts the Conn waiter through term.SafeGo (ADR-017).
	{pkg: "internal/sshclient", allow: []string{"internal/domain", "internal/term"}, forbid: append([]string{"internal/secrets", "internal/store", "internal/ui/...", "internal/app"}, bubbleTea...)},
	// The session never touches the local terminal: no raw mode, no
	// cancelable stdin (ARCH-SESS-003 revision).
	{pkg: "internal/session", allow: []string{"internal/domain", "internal/sshclient", "internal/term"}, forbid: append([]string{"internal/secrets", "internal/store", "internal/ui/...", "internal/app", "golang.org/x/term", "github.com/muesli/cancelreader"}, bubbleTea...)},
	// The SFTP tab's back end (M3, ADR-016, ADR-018).
	{pkg: "internal/files", allow: []string{"internal/domain", "internal/sshclient", "internal/term"}, forbid: append([]string{"internal/secrets", "internal/store", "internal/ui/...", "internal/app", "internal/session"}, bubbleTea...)},
	// The editor runner imports no internal package (ADR-019).
	{pkg: "internal/ui/editor", allow: []string{}},
	{pkg: "internal/ui/...", allow: []string{"internal/app", "internal/domain", "internal/search", "internal/session", "internal/ui/..."}, forbid: []string{"internal/secrets", "internal/store", "internal/sshclient", "internal/files", "golang.org/x/crypto/ssh/...", "github.com/pkg/sftp/..."}},
	{pkg: "internal/app", forbid: []string{"internal/ui/..."}},
}

// Every sshelf package: no child processes (ARCH-SESS-001, SEC-MEM-008)
// except the user's editor, started by internal/ui/editor only (their M3
// revisions, ADR-019), and no clipboard (SEC-CLIP-005).
var (
	osExec     = "os/exec"
	osExecUser = "internal/ui/editor"
	clipboard  = []string{"clipboard", "osc52"} // substrings of known clipboard package paths
	// pkg/sftp only in internal/files and the test server (ARCH-SFTP-001).
	pkgSFTP   = "github.com/pkg/sftp/..."
	sftpUsers = []string{"internal/files", "internal/testutil/..."}
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
			if imp == osExec && !match(module, osExecUser, p.ImportPath) {
				out = append(out, fmt.Sprintf("%s imports %s", rel(p.ImportPath), imp))
			}
			for _, c := range clipboard {
				if strings.Contains(imp, c) {
					out = append(out, fmt.Sprintf("%s imports clipboard package %s", rel(p.ImportPath), imp))
				}
			}
			if match(module, pkgSFTP, imp) && !matchAny(module, sftpUsers, p.ImportPath) {
				out = append(out, fmt.Sprintf("%s imports %s", rel(p.ImportPath), imp))
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

// osExecImporters returns the packages of the module in dir that import
// os/exec, relative to the module.
func osExecImporters(t *testing.T, dir string) []string {
	t.Helper()
	module, pkgs := load(t, dir)
	var out []string
	for _, p := range pkgs {
		if slices.Contains(p.Imports, osExec) {
			out = append(out, strings.TrimPrefix(p.ImportPath, module+"/"))
		}
	}
	slices.Sort(out)
	return out
}

// TestARCH_SESS_001_M3_ExecOnlyInEditor: os/exec is imported by
// internal/ui/editor and by no other sshelf package, and the import rules
// catch it anywhere else (a fixture module).
func TestARCH_SESS_001_M3_ExecOnlyInEditor(t *testing.T) {
	if got := osExecImporters(t, moduleRoot(t)); !slices.Equal(got, []string{osExecUser}) {
		t.Fatalf("packages importing os/exec: %q, want only %s", got, osExecUser)
	}
	module, pkgs := load(t, filepath.Join("testdata", "osexec"))
	got := violations(module, pkgs)
	if want := []string{"internal/session imports os/exec"}; !slices.Equal(got, want) {
		t.Fatalf("fixture violations = %q, want %q", got, want)
	}
}

// TestSEC_MEM_008_M3_ExecOnlyInEditor is SEC-MEM-008 M3 revision AC1 and
// SEC-SFTP-006 AC2: the only sshelf package that can start a child
// process is internal/ui/editor.
func TestSEC_MEM_008_M3_ExecOnlyInEditor(t *testing.T) {
	if got := osExecImporters(t, moduleRoot(t)); !slices.Equal(got, []string{osExecUser}) {
		t.Fatalf("packages importing os/exec: %q, want only %s", got, osExecUser)
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
			if path != root && skipDir(d.Name()) {
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

// TestARCH_PKG_002_OnlyPathsResolvesDirs checks that no package other than
// internal/paths works out a config or state directory itself.
func TestARCH_PKG_002_OnlyPathsResolvesDirs(t *testing.T) {
	needles := [][]byte{[]byte("UserConfigDir"), []byte("UserCacheDir"), []byte("XDG_")}
	root := moduleRoot(t)
	allowed := filepath.Join(root, "internal", "paths")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			if path == allowed {
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
		for _, n := range needles {
			if bytes.Contains(b, n) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s uses %s; resolve directories through internal/paths", rel, n)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// skipDir reports whether a directory is outside sshelf's own source: hidden
// directories (.git, local tool state such as agent worktrees), test
// fixtures and vendored code.
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "testdata" || name == "vendor"
}
