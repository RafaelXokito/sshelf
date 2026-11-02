package compat

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/secrets"
	"github.com/RafaelXokito/sshelf/internal/store"
	"github.com/RafaelXokito/sshelf/internal/update"
)

// fixtureRoot holds one directory per past release, named after its tag.
var fixtureRoot = filepath.Join("..", "..", "testdata", "compat")

// keyringService is the credential-store service sshelf uses
// (cmd/sshelf/store.go, SEC-CRED-002).
const keyringService = "sshelf"

// The files of a fixture directory.
const (
	configFile  = "config.json"
	knownHosts  = "known_hosts"
	keyringFile = "keyring.txt" // "<service>\t<account>" per stored secret
)

func fixtureDirs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(fixtureRoot)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, ok := update.Parse(e.Name()); !ok {
			t.Errorf("%s: fixture directories are named after a release tag, vX.Y.Z", e.Name())
			continue
		}
		dirs = append(dirs, filepath.Join(fixtureRoot, e.Name()))
	}
	if len(dirs) == 0 {
		t.Fatal("no fixture directory under testdata/compat")
	}
	return dirs
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from) //nolint:gosec // a fixture
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o600); err != nil { //nolint:gosec // a temp dir of the test
		t.Fatal(err)
	}
}

// TestD027_EveryReleaseFixtureLoads: config.json, known_hosts, the update
// check's state and the credential-store item names written by every past
// release are read by this build, and config.json survives a load and a
// save with nothing lost (FR-CFG-011, FR-CFG-012, ARCH-DATA-006).
func TestD027_EveryReleaseFixtureLoads(t *testing.T) {
	for _, dir := range fixtureDirs(t) {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			data := checkConfig(t, dir)
			checkKnownHosts(t, dir)
			checkUpdateState(t, dir)
			checkKeyring(t, dir, data)
		})
	}
}

func checkConfig(t *testing.T, dir string) domain.Data {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), configFile)
	copyFile(t, filepath.Join(dir, configFile), tmp)
	orig, _ := os.ReadFile(tmp) //nolint:gosec // the test's copy
	s, err := store.Load(tmp)
	if err != nil {
		t.Fatalf("config.json: %v", err)
	}
	if len(s.Warnings) > 0 {
		t.Errorf("config.json: warnings %q", s.Warnings)
	}
	if len(s.Data.Hosts) == 0 || len(s.Data.Identities) == 0 {
		t.Fatalf("config.json: %d hosts, %d identities; a fixture has some of each", len(s.Data.Hosts), len(s.Data.Identities))
	}
	if err := store.Save(tmp, s); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(tmp) //nolint:gosec // the test's copy
	s2, err := store.Load(tmp)
	if err != nil {
		t.Fatalf("config.json after a save: %v", err)
	}
	if !reflect.DeepEqual(s.Data, s2.Data) {
		t.Errorf("config.json: data changed by a save:\nbefore %+v\nafter  %+v", s.Data, s2.Data)
	}
	var before, after any
	if err := json.Unmarshal(orig, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(saved, &after); err != nil {
		t.Fatal(err)
	}
	if p := lost(before, after, "$"); p != "" {
		t.Errorf("config.json: a save lost %s\n--- saved ---\n%s", p, saved)
	}
	return s.Data
}

// lost returns the path of the first value of before that after does not
// keep, or "". Members after adds are fine.
func lost(before, after any, path string) string {
	switch b := before.(type) {
	case map[string]any:
		a, ok := after.(map[string]any)
		if !ok {
			return path
		}
		for k, v := range b {
			av, ok := a[k]
			if !ok {
				return path + "." + k
			}
			if p := lost(v, av, path+"."+k); p != "" {
				return p
			}
		}
		return ""
	case []any:
		a, ok := after.([]any)
		if !ok || len(a) != len(b) {
			return path
		}
		for i := range b {
			if p := lost(b[i], a[i], fmt.Sprintf("%s[%d]", path, i)); p != "" {
				return p
			}
		}
		return ""
	default:
		if !reflect.DeepEqual(before, after) {
			return path
		}
		return ""
	}
}

func checkKnownHosts(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, knownHosts)
	cb, err := knownhosts.New(path)
	if err != nil {
		t.Fatalf("known_hosts: %v", err)
	}
	b, _ := os.ReadFile(path) //nolint:gosec // a fixture
	n := 0
	for rest := b; len(bytes.TrimSpace(rest)) > 0; n++ {
		_, hosts, key, _, r, err := ssh.ParseKnownHosts(rest)
		if err != nil {
			t.Fatalf("known_hosts entry %d: %v", n+1, err)
		}
		rest = r
		host, port := hosts[0], "22"
		if h, p, err := net.SplitHostPort(host); err == nil && strings.HasPrefix(host, "[") {
			host, port = h, p // [host]:port
		}
		addr := net.JoinHostPort(host, port)
		if err := cb(addr, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 22}, key); err != nil {
			t.Errorf("known_hosts: %s not trusted: %v", addr, err)
		}
	}
	if n == 0 {
		t.Error("known_hosts: no entry")
	}
}

func checkUpdateState(t *testing.T, dir string) {
	t.Helper()
	s := update.LoadState(dir)
	if _, ok := update.Parse(s.Latest); !ok || s.CheckedAt.IsZero() {
		t.Errorf("%s: read as %+v", update.StateFile, s)
	}
}

func checkKeyring(t *testing.T, dir string, data domain.Data) {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, keyringFile)) //nolint:gosec // a fixture
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	stored := map[secrets.Ref]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		service, account, ok := strings.Cut(line, "\t")
		if !ok || service != keyringService {
			t.Errorf("keyring.txt: %q: want %q, a tab and an account", line, keyringService)
			continue
		}
		ref, err := secrets.ParseRef(account)
		if err != nil || ref.Account() != account {
			t.Errorf("keyring.txt: account %q no longer parses: %v", account, err)
			continue
		}
		stored[ref] = true
	}
	// Every secret config.json says is stored has its item name.
	for _, id := range data.Identities {
		for field, has := range map[secrets.Field]bool{
			secrets.FieldPassword: id.HasPassword, secrets.FieldPrivateKey: id.HasKey, secrets.FieldKeyPassphrase: id.HasPassphrase,
		} {
			if ref := (secrets.Ref{IdentityID: string(id.ID), Field: field}); has && !stored[ref] {
				t.Errorf("identity %s has %s but keyring.txt has no %q", id.ID, field, ref.Account())
			}
		}
	}
}

// TestD027_WriteFixture writes this build's files into
// testdata/compat/$SSHELF_COMPAT_FIXTURE, a new directory, for the
// release about to be tagged. It never touches an existing directory.
func TestD027_WriteFixture(t *testing.T) {
	tag := os.Getenv("SSHELF_COMPAT_FIXTURE")
	if tag == "" {
		t.Skip("set SSHELF_COMPAT_FIXTURE=vX.Y.Z to write a release's fixture")
	}
	if _, ok := update.Parse(tag); !ok {
		t.Fatalf("SSHELF_COMPAT_FIXTURE=%q is not a release tag", tag)
	}
	dir := filepath.Join(fixtureRoot, tag)
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) { //nolint:gosec // testdata/compat/<tag>, the tag checked above
		t.Fatalf("%s exists; old fixtures are never edited (D-027)", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // a testdata dir in the repo
		t.Fatal(err)
	}
	data := fixtureData()
	if err := store.Save(filepath.Join(dir, configFile), &store.Snapshot{Data: data}); err != nil {
		t.Fatal(err)
	}
	// known_hosts as sshelf's trust prompt writes it (internal/app/tofu.go).
	key, err := ssh.NewPublicKey(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize)).Public())
	if err != nil {
		t.Fatal(err)
	}
	var kh strings.Builder
	for _, addr := range []string{"web.example.com:22", "db.example.com:2222"} {
		kh.WriteString(knownhosts.Line([]string{knownhosts.Normalize(addr)}, key) + "\n")
	}
	if err := os.WriteFile(filepath.Join(dir, knownHosts), []byte(kh.String()), paths.FileMode); err != nil { //nolint:gosec // testdata/compat/<tag>
		t.Fatal(err)
	}
	if err := update.SaveState(dir, update.State{CheckedAt: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC), Latest: tag}); err != nil {
		t.Fatal(err)
	}
	var kr strings.Builder
	kr.WriteString("# credential-store items: service<TAB>account (SEC-CRED-002)\n")
	for _, id := range data.Identities {
		for _, f := range []struct {
			has   bool
			field secrets.Field
		}{{id.HasPassword, secrets.FieldPassword}, {id.HasKey, secrets.FieldPrivateKey}, {id.HasPassphrase, secrets.FieldKeyPassphrase}} {
			if f.has {
				kr.WriteString(keyringService + "\t" + secrets.Ref{IdentityID: string(id.ID), Field: f.field}.Account() + "\n")
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, keyringFile), []byte(kr.String()), paths.FileMode); err != nil { //nolint:gosec // testdata/compat/<tag>
		t.Fatal(err)
	}
	t.Logf("wrote %s; commit it with the release", dir)
}

// fixtureData uses every field config.json has.
func fixtureData() domain.Data {
	at := time.Date(2026, 10, 30, 9, 15, 0, 0, time.UTC)
	return domain.Data{
		Version:  store.Version,
		Settings: domain.Settings{Theme: "dark"},
		Groups: []domain.Group{
			{Meta: domain.Meta{ID: "019a3b10-0000-7000-8000-000000000001"}, Name: "Production"},
			{Meta: domain.Meta{ID: "019a3b10-0000-7000-8000-000000000002"}, Name: "Databases", ParentID: "019a3b10-0000-7000-8000-000000000001"},
		},
		Identities: []domain.Identity{
			{Meta: domain.Meta{ID: "019a3b10-0000-7000-8000-000000000010"}, Label: "deploy", Username: "deploy", HasPassword: true},
			{Meta: domain.Meta{ID: "019a3b10-0000-7000-8000-000000000011"}, Label: "web key", Hidden: true, HasKey: true, HasPassphrase: true,
				PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEoW4G7m4zT0lJ1Yd7oP0E9mQx2m2c9b7j8B1a2C3d4E web"},
			{Meta: domain.Meta{ID: "019a3b10-0000-7000-8000-000000000012"}, Label: "key file", KeyPath: "~/.ssh/id_ed25519", UseAgent: true},
		},
		Hosts: []domain.Host{
			{Meta: domain.Meta{ID: "019a3b10-0000-7000-8000-000000000020"}, Label: "web prod", Hostname: "web.example.com", Port: 22, User: "ubuntu",
				IdentityID: "019a3b10-0000-7000-8000-000000000011", GroupID: "019a3b10-0000-7000-8000-000000000001", Tags: []string{"prod", "web"}, LastConnectedAt: &at},
			{Meta: domain.Meta{ID: "019a3b10-0000-7000-8000-000000000021"}, Label: "db", Hostname: "db.example.com", Port: 2222,
				IdentityID: "019a3b10-0000-7000-8000-000000000010", GroupID: "019a3b10-0000-7000-8000-000000000002",
				JumpHostIDs: []domain.ID{"019a3b10-0000-7000-8000-000000000020"}, Options: domain.HostOptions{ForwardAgent: true}},
			{Meta: domain.Meta{ID: "019a3b10-0000-7000-8000-000000000022"}, Label: "lab", Hostname: "10.0.0.5", IdentityID: "019a3b10-0000-7000-8000-000000000012"},
		},
	}
}
