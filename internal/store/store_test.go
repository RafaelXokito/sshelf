package store

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // test temp file
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMissingIsFirstRun(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "config.json"))
	if err != nil || !s.FirstRun || len(s.Data.Hosts) != 0 {
		t.Fatalf("Load(missing) = %+v, %v", s, err)
	}
}

func TestLoadSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	const cfg = `{
  "version": 1,
  "settings": {"theme": "auto"},
  "groups": [],
  "identities": [{"id": "i1", "name": "ops", "username": "deploy", "key_path": "~/.ssh/id_ed25519",
                  "has_password": true, "has_private_key": false, "has_key_passphrase": true, "future": 1}],
  "hosts": [{"id": "h1", "label": "db 1", "hostname": "db1.example.com", "port": 2222, "user": "",
             "identity_id": "i1", "group_id": "", "tags": ["pg"], "jump_host_ids": [], "forward_agent": false}]
}`
	writeTestFile(t, path, cfg)
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.FirstRun || s.Data.Version != 1 || len(s.Data.Hosts) != 1 || len(s.Warnings) != 0 {
		t.Fatalf("snapshot = %+v", s)
	}
	wantID := domain.Identity{Meta: domain.Meta{ID: "i1"}, Label: "ops", Username: "deploy",
		KeyPath: "~/.ssh/id_ed25519", HasPassword: true, HasPassphrase: true}
	if len(s.Data.Identities) != 1 || s.Data.Identities[0] != wantID {
		t.Fatalf("identities = %+v", s.Data.Identities)
	}
	h := s.Data.Hosts
	if len(h) != 1 || h[0].ID != "h1" || h[0].Label != "db 1" || h[0].Hostname != "db1.example.com" ||
		h[0].Port != 2222 || h[0].IdentityID != "i1" || len(h[0].Tags) != 1 || s.Data.Settings.Theme != "auto" {
		t.Fatalf("data = %+v", s.Data)
	}
	if string(readFile(t, path)) != cfg {
		t.Fatal("Load changed the file")
	}
}

// TestSchemaV1RoundTripGolden loads the credential-storage §3.3 schema and
// saves it again: the bytes are identical.
func TestSchemaV1RoundTripGolden(t *testing.T) {
	golden := readFile(t, filepath.Join("testdata", "config_v1.json"))
	path := filepath.Join(t.TempDir(), "config.json")
	writeTestFile(t, path, string(golden))
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	g := s.Data.Groups
	if len(g) != 2 || g[0].ParentID != "" || g[1].ParentID != g[0].ID || len(s.Data.Hosts) != 2 ||
		!s.Data.Hosts[1].Options.ForwardAgent || len(s.Data.Hosts[1].JumpHostIDs) != 1 {
		t.Fatalf("data = %+v", s.Data)
	}
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); !bytes.Equal(got, golden) {
		t.Fatalf("round trip differs:\n%s", got)
	}
}

func TestFR_CFG_003_SyntaxErrorPosition(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		line, col     int
	}{
		{"bad token", "{\n  \"version\": 1,\n  \"hosts\": [}\n}", 3, 13},
		{"truncated", "{\n  \"hosts\": [", 2, 12},
		{"wrong type", "{\n  \"version\": 1,\n  \"hosts\": [{\"port\": \"22\"}]\n}", 3, 25},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			writeTestFile(t, path, tc.content)
			_, err := Load(path)
			var le *LoadError
			if !errors.As(err, &le) || !errors.Is(err, ErrCorrupt) {
				t.Fatalf("err = %v, want a *LoadError wrapping ErrCorrupt", err)
			}
			if le.Path != path || le.Line != tc.line || le.Column != tc.col || le.Problem == "" {
				t.Fatalf("LoadError = %+v, want line %d column %d", le, tc.line, tc.col)
			}
			if !strings.HasPrefix(err.Error(), fmt.Sprintf("%s:%d:%d: ", path, tc.line, tc.col)) {
				t.Fatalf("Error() = %q", err)
			}
		})
	}
}

func TestFR_CFG_003_FileByteForByteUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	const broken = "{\"version\": 1, \"hosts\": [\n"
	writeTestFile(t, path, broken)
	before, _ := os.Stat(path)
	for range 2 {
		if _, err := Load(path); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("err = %v", err)
		}
	}
	after, _ := os.Stat(path)
	if string(readFile(t, path)) != broken || !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("Load changed a broken file")
	}
}

func TestFR_CFG_003_UnknownSettingWarns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTestFile(t, path, `{"version": 1, "settings": {"theme": "dark", "zzz": 1, "colour": "red"}, "hosts": []}`)
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`config.json: unknown setting "colour" ignored`, `config.json: unknown setting "zzz" ignored`}
	if !reflect.DeepEqual(s.Warnings, want) || s.Data.Settings.Theme != "dark" {
		t.Fatalf("warnings = %q, theme = %q", s.Warnings, s.Data.Settings.Theme)
	}
}

func TestNewerVersionRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTestFile(t, path, `{"version": 2, "hosts": []}`)
	_, err := Load(path)
	var le *LoadError
	if !errors.Is(err, ErrNewerVersion) || !errors.As(err, &le) || !strings.Contains(le.Problem, "version 2") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	big := `{"version": 1, "hosts": [], "pad": "` + strings.Repeat("x", MaxFileSize) + `"}`
	writeTestFile(t, path, big)
	if _, err := Load(path); !errors.Is(err, ErrCorrupt) || !strings.Contains(err.Error(), "16 MiB") {
		t.Fatalf("err = %v", err)
	}
}

func TestSEC_CRED_001_SchemaHasNoSecretFields(t *testing.T) {
	forbidden := []string{"password", "passphrase", "private_key", "privatekey", "secret"}
	seen := map[reflect.Type]bool{}
	var walk func(path string, typ reflect.Type)
	walk = func(path string, typ reflect.Type) {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Map {
			if typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Uint8 {
				t.Errorf("%s is a []byte", path)
				return
			}
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct || seen[typ] {
			return
		}
		seen[typ] = true
		for i := range typ.NumField() {
			f := typ.Field(i)
			name := strings.ToLower(f.Name + " " + f.Tag.Get("json"))
			p := path + "." + f.Name
			if f.Type.Name() == "Secret" {
				t.Errorf("%s is a Secret", p)
			}
			if f.Type.Kind() == reflect.String {
				for _, w := range forbidden {
					if strings.Contains(name, w) {
						t.Errorf("%s is a string field named like a secret (%s)", p, w)
					}
				}
			}
			walk(p, f.Type)
		}
	}
	walk("fileConfig", reflect.TypeFor[fileConfig]())
	if len(seen) < 4 {
		t.Fatalf("walked only %d struct types", len(seen))
	}
}

// faultFS is osFS with one failing step.
type faultFS struct {
	osFS
	fail string
}

var errFault = errors.New("injected fault")

func (f faultFS) EvalSymlinks(p string) (string, error) {
	if f.fail == "EvalSymlinks" {
		return "", errFault
	}
	return f.osFS.EvalSymlinks(p)
}

func (f faultFS) CreateTemp(dir, pattern string) (tempFile, error) {
	if f.fail == "CreateTemp" {
		return nil, errFault
	}
	t, err := f.osFS.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	return faultFile{tempFile: t, fail: f.fail}, nil
}

func (f faultFS) Rename(o, n string) error {
	if f.fail == "Rename" {
		return errFault
	}
	return f.osFS.Rename(o, n)
}

func (f faultFS) SyncDir(d string) error {
	if f.fail == "SyncDir" {
		return errFault
	}
	return f.osFS.SyncDir(d)
}

type faultFile struct {
	tempFile
	fail string
}

func (f faultFile) Write(p []byte) (int, error) {
	if f.fail == "Write" {
		n, _ := f.tempFile.Write(p[:len(p)/2]) // a partial write
		return n, errFault
	}
	return f.tempFile.Write(p)
}

func (f faultFile) Chmod(m fs.FileMode) error {
	if f.fail == "Chmod" {
		return errFault
	}
	return f.tempFile.Chmod(m)
}

func (f faultFile) Sync() error {
	if f.fail == "Sync" {
		return errFault
	}
	return f.tempFile.Sync()
}

func (f faultFile) Close() error {
	err := f.tempFile.Close()
	if f.fail == "Close" {
		return errFault
	}
	return err
}

func snapshotWith(labels ...string) *Snapshot {
	s := &Snapshot{}
	for i, l := range labels {
		s.Data.Hosts = append(s.Data.Hosts, domain.Host{Meta: domain.Meta{ID: domain.ID(strconv.Itoa(i))}, Label: l, Hostname: l})
	}
	return s
}

func TestSEC_FILE_003_FaultAtEachStep(t *testing.T) {
	oldSnap, newSnap := snapshotWith("old"), snapshotWith("new", "newer")
	oldBytes, _ := encode(oldSnap.Data)
	newBytes, _ := encode(newSnap.Data)
	for _, step := range []string{"EvalSymlinks", "CreateTemp", "Write", "Chmod", "Sync", "Close", "Rename", "SyncDir"} {
		t.Run(step, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			if err := Save(path, oldSnap); err != nil {
				t.Fatal(err)
			}
			err := save(faultFS{fail: step}, path, newSnap)
			if !errors.Is(err, errFault) {
				t.Fatalf("save err = %v, want the injected fault", err)
			}
			got := readFile(t, path)
			want := oldBytes
			if step == "SyncDir" { // the rename already happened
				want = newBytes
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("after a fault at %s the file is:\n%s", step, got)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 1 {
				t.Fatalf("temp file left behind: %v", entries)
			}
		})
	}
}

func TestSEC_FILE_003_SymlinkPreserved(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles", "sshelf.json")
	if err := os.Mkdir(filepath.Dir(real), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Save(real, snapshotWith("old")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.json")
	if err := os.Symlink(filepath.Join("dotfiles", "sshelf.json"), link); err != nil {
		t.Fatal(err)
	}
	if err := Save(link, snapshotWith("new")); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("config.json is no longer a symlink: %v, %v", st, err)
	}
	s, err := Load(real)
	if err != nil || len(s.Data.Hosts) != 1 || s.Data.Hosts[0].Label != "new" {
		t.Fatalf("target = %+v, %v", s, err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(real)); len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
}

func TestWriteFileCreatesNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := WriteFile(path, []byte("line\n")); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != fileMode || string(readFile(t, path)) != "line\n" {
		t.Fatalf("stat = %v, %v", st, err)
	}
}

// The helper process for the crash tests. It runs only when the test binary
// is re-executed with SSHELF_STORE_HELPER set.
func TestStoreHelperProcess(t *testing.T) {
	path := os.Getenv("SSHELF_STORE_HELPER_PATH")
	switch os.Getenv("SSHELF_STORE_HELPER") {
	case "save-once":
		if err := Save(path, snapshotWith("after")); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
		fmt.Println("Saved")
		time.Sleep(time.Minute) // until SIGKILL
	case "save-loop":
		labels := make([]string, 3000)
		for i := 0; ; i++ {
			for j := range labels {
				labels[j] = fmt.Sprintf("gen-%d", i%2)
			}
			if err := Save(path, snapshotWith(labels...)); err != nil {
				fmt.Println("error:", err)
				os.Exit(1)
			}
			if i == 0 {
				fmt.Println("Saved")
			}
		}
	default:
		t.Skip("helper process only")
	}
}

// startHelper re-executes the test binary as a saver and waits for its
// first "Saved".
func startHelper(t *testing.T, mode, path string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestStoreHelperProcess$") //nolint:gosec // the test binary itself
	cmd.Env = append(os.Environ(), "SSHELF_STORE_HELPER="+mode, "SSHELF_STORE_HELPER_PATH="+path)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || line != "Saved\n" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("helper said %q, %v", line, err)
	}
	return cmd
}

func kill(cmd *exec.Cmd) {
	_ = cmd.Process.Kill() // SIGKILL
	_ = cmd.Wait()
}

func TestFR_CFG_002_SigkillAfterSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, snapshotWith("before")); err != nil {
		t.Fatal(err)
	}
	kill(startHelper(t, "save-once", path))
	s, err := Load(path)
	if err != nil || len(s.Data.Hosts) != 1 || s.Data.Hosts[0].Label != "after" {
		t.Fatalf("after SIGKILL: %+v, %v", s, err)
	}
}

func TestFR_CFG_002_NoTruncatedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for i := range 10 {
		cmd := startHelper(t, "save-loop", path)
		time.Sleep(time.Duration(1+i*3) * time.Millisecond)
		kill(cmd)
		s, err := Load(path)
		if err != nil {
			t.Fatalf("kill %d: %v", i, err)
		}
		h := s.Data.Hosts
		if len(h) != 3000 || h[0].Label != h[len(h)-1].Label {
			t.Fatalf("kill %d: %d hosts, a mix of generations", i, len(h))
		}
	}
}

func FuzzLoadConfig(f *testing.F) {
	golden, err := os.ReadFile(filepath.Join("testdata", "config_v1.json"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(golden)
	f.Add([]byte(`{"version": 1, "settings": {"x": []}}`))
	f.Add([]byte("{\n\"hosts\": [\n"))
	f.Add([]byte(`{"version": 99}`))
	// FR-NAV-018: an offset and a fraction are normalised; a bad time is dropped.
	f.Add([]byte(`{"version": 1, "hosts": [{"id": "h", "last_connected_at": "2026-10-30T10:00:00.5+01:00"}]}`))
	f.Add([]byte(`{"version": 1, "hosts": [{"id": "h", "last_connected_at": "9999-12-31T23:59:59-01:00"}]}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		s, err := decode("config.json", b)
		if err != nil {
			var le *LoadError
			if !errors.As(err, &le) || le.Line < 0 || le.Column < 0 {
				t.Fatalf("decode error %v is not a valid *LoadError", err)
			}
			return
		}
		// Whatever loads can be saved and loads again the same.
		out, err := encode(s.Data)
		if err != nil {
			t.Fatal(err)
		}
		s2, err := decode("config.json", out)
		if err != nil {
			t.Fatalf("re-decode: %v", err)
		}
		out2, _ := encode(s2.Data)
		if !bytes.Equal(out, out2) {
			t.Fatalf("encode is not stable:\n%s\n%s", out, out2)
		}
	})
}
