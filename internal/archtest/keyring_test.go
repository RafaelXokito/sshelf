package archtest

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const goKeyring = "github.com/zalando/go-keyring"

// cgoKeychainPackages are credential-store libraries whose macOS backend
// needs cgo and binds item ACLs to the binary's signature (D-008).
var cgoKeychainPackages = []string{
	"github.com/99designs/keyring",
	"github.com/99designs/go-keychain",
	"github.com/keybase/go-keychain",
}

// TestSEC_CRED_009_BackendIsGoKeyring checks that secrets uses go-keyring
// and that nothing in sshelf pulls in a cgo Keychain library.
func TestSEC_CRED_009_BackendIsGoKeyring(t *testing.T) {
	module, pkgs := load(t, moduleRoot(t))
	i := slices.IndexFunc(pkgs, func(p pkg) bool { return p.ImportPath == module+"/internal/secrets" })
	if i < 0 {
		t.Fatal("internal/secrets not found")
	}
	if !slices.Contains(pkgs[i].Imports, goKeyring) {
		t.Errorf("internal/secrets does not import %s; imports: %v", goKeyring, pkgs[i].Imports)
	}

	for _, goos := range []string{"darwin", "linux"} {
		cmd := exec.Command("go", "list", "-deps", "-tags", buildTags, "-f", "{{.ImportPath}}", "./...")
		cmd.Dir = moduleRoot(t)
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOOS="+goos, "CGO_ENABLED=1")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list (GOOS=%s): %v", goos, err)
		}
		for _, dep := range strings.Fields(string(out)) {
			for _, bad := range cgoKeychainPackages {
				if dep == bad || strings.HasPrefix(dep, bad+"/") {
					t.Errorf("GOOS=%s: dependency %s is a cgo keychain backend (D-008)", goos, dep)
				}
			}
		}
	}
}
