package archtest

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestARCH_SFTP_001_OnlyRemoteGoImportsSftp: within internal/files, only
// remote.go imports github.com/pkg/sftp (ADR-016); the import rules above
// keep it out of every other package but the test server.
func TestARCH_SFTP_001_OnlyRemoteGoImportsSftp(t *testing.T) {
	dir := filepath.Join(moduleRoot(t), "internal", "files")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var importers []string
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, e.Name()), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == "github.com/pkg/sftp" || strings.HasPrefix(p, "github.com/pkg/sftp/") {
				importers = append(importers, e.Name())
			}
		}
	}
	if !slices.Equal(importers, []string{"remote.go"}) {
		t.Fatalf("files importing pkg/sftp in internal/files = %v, want [remote.go]", importers)
	}
}

func TestARCH_SFTP_001_SftpImportElsewhereFails(t *testing.T) {
	module := "example.com/m"
	pkgs := []pkg{
		{ImportPath: module + "/internal/app", Imports: []string{"github.com/pkg/sftp"}},
		{ImportPath: module + "/internal/files", Imports: []string{"github.com/pkg/sftp"}},
		{ImportPath: module + "/internal/testutil/sshtest", Imports: []string{"github.com/pkg/sftp"}},
	}
	got := violations(module, pkgs)
	if !slices.Equal(got, []string{"internal/app imports github.com/pkg/sftp"}) {
		t.Fatalf("violations = %q", got)
	}
}
