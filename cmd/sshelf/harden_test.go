//go:build unix

package main

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// hardenChildEnv makes TestMain run hardenChild instead of the tests, so
// harden changes a child process and never the test process itself.
const hardenChildEnv = "SSHELF_TEST_HARDEN_CHILD"

// hardenChild calls harden and prints the resulting core limit and
// dumpable flag (-1 where the OS has none).
func hardenChild() int {
	if err := harden(); err != nil {
		fmt.Fprintln(os.Stderr, "harden:", err)
		return 1
	}
	var lim unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_CORE, &lim); err != nil {
		fmt.Fprintln(os.Stderr, "getrlimit:", err)
		return 1
	}
	d, err := dumpable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "dumpable:", err)
		return 1
	}
	fmt.Printf("core %d %d dumpable %d\n", lim.Cur, lim.Max, d)
	return 0
}

// hardenedState runs hardenChild in a re-executed test binary.
func hardenedState(t *testing.T) (coreCur, coreMax uint64, dumpable int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0]) //nolint:gosec // re-exec of this test binary
	cmd.Env = append(os.Environ(), hardenChildEnv+"=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("harden child: %v", err)
	}
	if _, err := fmt.Sscanf(string(out), "core %d %d dumpable %d", &coreCur, &coreMax, &dumpable); err != nil {
		t.Fatalf("harden child output %q: %v", out, err)
	}
	return coreCur, coreMax, dumpable
}

func TestSEC_MEM_003_CoreLimitZero(t *testing.T) {
	if cur, maxLim, _ := hardenedState(t); cur != 0 || maxLim != 0 {
		t.Fatalf("RLIMIT_CORE after harden = {%d,%d}, want {0,0}", cur, maxLim)
	}
}

func TestSEC_MEM_003_NotDumpableOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("PR_SET_DUMPABLE is Linux only")
	}
	if _, _, d := hardenedState(t); d != 0 {
		t.Fatalf("PR_GET_DUMPABLE after harden = %d, want 0", d)
	}
}

// TestSEC_MEM_003_HardenFirstInMain checks AC1's "called first thing at
// startup": the first statement of main is `if err := harden(); ...`.
func TestSEC_MEM_003_HardenFirstInMain(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "main" || fn.Recv != nil {
			continue
		}
		if len(fn.Body.List) > 0 {
			if is, ok := fn.Body.List[0].(*ast.IfStmt); ok {
				if as, ok := is.Init.(*ast.AssignStmt); ok && len(as.Rhs) == 1 {
					if call, ok := as.Rhs[0].(*ast.CallExpr); ok {
						if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "harden" {
							return
						}
					}
				}
			}
		}
		t.Fatal("the first statement of main is not `if err := harden(); ...`")
	}
	t.Fatal("func main not found in main.go")
}
