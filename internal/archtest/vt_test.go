package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// vtPath is the terminal emulator sshelf pins (ADR-013).
const vtPath = "github.com/charmbracelet/x/vt"

// TestARCH_PKG_001_OnlyEmulatorGoImportsVT: x/vt is imported by
// internal/session/emulator.go and nowhere else, so a later version or
// another emulator touches one file (ADR-013).
func TestARCH_PKG_001_OnlyEmulatorGoImportsVT(t *testing.T) {
	root := moduleRoot(t)
	allowed := filepath.Join(root, "internal", "session", "emulator.go")
	fset := token.NewFileSet()
	seen := false
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
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if p != vtPath && !strings.HasPrefix(p, vtPath+"/") {
				continue
			}
			if path == allowed {
				seen = true
				continue
			}
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s imports %s; only internal/session/emulator.go may (ADR-013)", rel, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !seen {
		t.Fatalf("internal/session/emulator.go does not import %s", vtPath)
	}
}
