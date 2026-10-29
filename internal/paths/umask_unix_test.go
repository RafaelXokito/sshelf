//go:build unix

package paths

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestSEC_FILE_001_ModesUnderUmask000(t *testing.T) {
	for _, umask := range []int{0o000, 0o022} {
		old := syscall.Umask(umask)
		dir := filepath.Join(t.TempDir(), "parent", "sshelf")
		_, err := EnsureDir(dir)
		syscall.Umask(old)
		if err != nil {
			t.Fatal(err)
		}
		if m := mode(t, dir); m != DirMode {
			t.Errorf("umask %03o: dir mode %o, want %o", umask, m, DirMode)
		}
	}
}
