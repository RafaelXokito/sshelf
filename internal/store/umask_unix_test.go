//go:build unix

package store

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestSEC_FILE_001_ModesUnderUmask000(t *testing.T) {
	for _, umask := range []int{0o000, 0o022} {
		path := filepath.Join(t.TempDir(), "config.json")
		old := syscall.Umask(umask)
		err := Save(path, &Snapshot{})
		syscall.Umask(old)
		if err != nil {
			t.Fatal(err)
		}
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if m := st.Mode().Perm(); m != 0o600 {
			t.Errorf("umask %03o: config.json mode %o, want 0600", umask, m)
		}
	}
}
