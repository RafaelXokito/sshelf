package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// networkFuncs are the net and x/crypto/ssh identifiers that open a
// network connection or listen for one.
var networkFuncs = map[string][]string{
	"net": {
		"Dial", "DialTimeout", "Dialer", "DialIP", "DialTCP", "DialUDP", "DialUnix",
		"Listen", "ListenConfig", "ListenIP", "ListenMulticastUDP", "ListenPacket", "ListenTCP", "ListenUDP", "ListenUnix", "ListenUnixgram",
	},
	"golang.org/x/crypto/ssh": {"Dial"},
}

// networkUses returns "file:line: pkg.Name" for every reference in src to
// one of networkFuncs, resolving import names and aliases. A dot import
// of one of those packages is reported too.
func networkUses(fset *token.FileSet, filename string, src any) ([]string, error) {
	f, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	names := map[string]string{} // local name -> import path
	var out []string
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, err
		}
		if _, ok := networkFuncs[path]; !ok {
			continue
		}
		name := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name == "." {
			out = append(out, fset.Position(imp.Pos()).String()+": dot import of "+path)
			continue
		}
		names[name] = path
	}
	if len(names) == 0 {
		return out, nil
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if path, ok := names[id.Name]; ok && slices.Contains(networkFuncs[path], sel.Sel.Name) {
			out = append(out, fset.Position(sel.Pos()).String()+": "+path+"."+sel.Sel.Name)
		}
		return true
	})
	return out, nil
}

// TestNFR_PRIV_001_NoHTTPClient checks that sshelf cannot make network
// traffic other than the SSH connections the user starts: the binary does
// not link net/http (no telemetry, no update checks) on any supported OS,
// and only internal/sshclient dials or listens. Test files and the test
// helpers in internal/testutil are not part of the binary and are skipped.
// The scan is syntactic (go/parser with import aliases resolved), which
// avoids a golang.org/x/tools dependency for go/packages.
func TestNFR_PRIV_001_NoHTTPClient(t *testing.T) {
	root := moduleRoot(t)
	for _, goos := range []string{"linux", "darwin"} {
		cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", "./cmd/sshelf")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOOS="+goos, "CGO_ENABLED=0")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list (GOOS=%s): %v", goos, err)
		}
		deps := strings.Fields(string(out))
		if !slices.Contains(deps, "net") {
			t.Fatalf("GOOS=%s: go list output lacks net; the check would be vacuous", goos)
		}
		for _, dep := range deps {
			if dep == "net/http" || strings.HasPrefix(dep, "net/http/") {
				t.Errorf("GOOS=%s: sshelf links %s", goos, dep)
			}
		}
	}

	allowed := filepath.Join(root, "internal", "sshclient")
	exempt := filepath.Join(root, "internal", "testutil")
	fset := token.NewFileSet()
	var inSSHClient int
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (skipDir(d.Name()) || path == exempt) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		uses, err := networkUses(fset, path, nil)
		if err != nil {
			return err
		}
		if filepath.Dir(path) == allowed {
			inSSHClient += len(uses)
			return nil
		}
		for _, u := range uses {
			rel, _ := filepath.Rel(root, u)
			t.Errorf("%s outside internal/sshclient (NFR-PRIV-001)", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if inSSHClient == 0 {
		t.Fatal("found no dialer in internal/sshclient; the scan is not working")
	}
}

func TestNFR_PRIV_001_NetworkUseDetected(t *testing.T) {
	src := `package p
import (
	"net"
	n2 "net"
	"golang.org/x/crypto/ssh"
	"strings"
)
func f() {
	var d net.Dialer
	_, _ = n2.Listen("tcp", ":0")
	_, _ = ssh.Dial("tcp", "x", nil)
	_ = net.ParseIP("::1")
	_ = strings.Dial
	_ = d
}
`
	got, err := networkUses(token.NewFileSet(), "p.go", src)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"p.go:9:8: net.Dialer", "p.go:10:9: net.Listen", "p.go:11:9: golang.org/x/crypto/ssh.Dial"}
	if !slices.Equal(got, want) {
		t.Fatalf("networkUses = %q, want %q", got, want)
	}
	got, err = networkUses(token.NewFileSet(), "q.go", "package q\nimport . \"net\"\n")
	if err != nil || len(got) != 1 {
		t.Fatalf("dot import: %q, %v", got, err)
	}
}
