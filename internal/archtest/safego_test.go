package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// goStatements returns the position of every go statement in src.
func goStatements(fset *token.FileSet, filename string, src any) ([]token.Position, error) {
	f, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var out []token.Position
	ast.Inspect(f, func(n ast.Node) bool {
		if g, ok := n.(*ast.GoStmt); ok {
			out = append(out, fset.Position(g.Pos()))
		}
		return true
	})
	return out, nil
}

// TestARCH_SEC_004_NoBareGoStatements checks that every goroutine sshelf
// starts goes through term.SafeGo: no go statement outside internal/term.
// Test files and the test helpers in internal/testutil are not sshelf's
// runtime and are skipped. Build tags are ignored, so tagged files count.
func TestARCH_SEC_004_NoBareGoStatements(t *testing.T) {
	root := moduleRoot(t)
	exempt := []string{filepath.Join(root, "internal", "term"), filepath.Join(root, "internal", "testutil")}
	fset := token.NewFileSet()
	files := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			for _, e := range exempt {
				if path == e {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files++
		found, err := goStatements(fset, path, nil)
		if err != nil {
			return err
		}
		for _, p := range found {
			rel, _ := filepath.Rel(root, p.Filename)
			t.Errorf("%s:%d: bare go statement; use term.SafeGo (ARCH-SEC-004)", rel, p.Line)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatal("no Go files checked")
	}
}

func TestARCH_SEC_004_GoStatementDetected(t *testing.T) {
	src := "package p\nfunc f() { go func() {}(); defer func() {}() }\nfunc g() { go f() }\n"
	found, err := goStatements(token.NewFileSet(), "p.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 || found[0].Line != 2 || found[1].Line != 3 {
		t.Fatalf("found %v, want lines 2 and 3", found)
	}
}
