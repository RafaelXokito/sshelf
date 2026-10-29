package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/secrets"
)

// importApp is a loaded App with an empty config dir and HOME set to a
// temp dir whose ~/.ssh/config is sshConfig ("" for none).
func importApp(t *testing.T, sshConfig string) (*App, *secrets.MemStore, string) {
	t.Helper()
	home := t.TempDir()
	if sshConfig != "" {
		if err := os.Mkdir(filepath.Join(home, ".ssh"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(sshConfig), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ms := secrets.NewMemStore()
	dir := filepath.Join(t.TempDir(), "sshelf")
	a := New(Config{Dir: dir, Home: home, Secrets: ms, Terminal: stillTerm{}})
	if _, err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	return a, ms, home
}

const importFixture = `Host web
    HostName 10.0.0.10
    User ubuntu
    IdentityFile ~/.ssh/id_ed25519

Host a b

Host aws-01 aws-02 aws-03
    User ubuntu
    IdentityFile ~/.ssh/aws.pem

Host bastion
    HostName bastion.example
    ProxyCommand ssh -W %h:%p jump
    ProxyJump jump.example
    LocalForward 8080 localhost:80
    ForwardAgent yes

Match originalhost never
    User nobody

Host *
    ServerAliveInterval 30
`

func TestFR_IMPORT_001_KeysReferencedNotCopied(t *testing.T) {
	a, ms, home := importApp(t, importFixture)
	rep, err := a.ImportSSHConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ms.Calls(secrets.OpSet) != 0 || ms.Calls(secrets.OpGet) != 0 || len(ms.Keys()) != 0 {
		t.Fatalf("the credential store was used: set %d, get %d", ms.Calls(secrets.OpSet), ms.Calls(secrets.OpGet))
	}
	if want := []string{"web", "a", "b", "aws-01", "aws-02", "aws-03", "bastion"}; !slices.Equal(rep.Created, want) {
		t.Fatalf("created %v, want %v", rep.Created, want)
	}
	if want := []string{"ubuntu (id_ed25519)", "ubuntu (aws.pem)"}; !slices.Equal(rep.Identities, want) {
		t.Fatalf("identities %v, want %v", rep.Identities, want)
	}
	byLabel := map[string]domain.Host{}
	for _, h := range rep.AllHosts {
		byLabel[h.Label] = h
	}
	idOf := func(label string) domain.Identity {
		i := slices.IndexFunc(rep.AllIdentities, func(id domain.Identity) bool { return id.ID == byLabel[label].IdentityID })
		if i < 0 {
			t.Fatalf("%s has no identity", label)
		}
		return rep.AllIdentities[i]
	}
	web := byLabel["web"]
	if web.Hostname != "10.0.0.10" || web.User != "ubuntu" || web.Import == nil || web.Import.Alias != "web" || web.Import.ImportedAt.IsZero() {
		t.Fatalf("web = %+v", web)
	}
	if id := idOf("web"); id.KeyPath != filepath.Join(home, ".ssh", "id_ed25519") || id.HasKey || id.Username != "ubuntu" {
		t.Fatalf("web identity %+v", id)
	}
	if byLabel["a"].Hostname != "a" || byLabel["a"].IdentityID != "" {
		t.Fatalf("a = %+v", byLabel["a"])
	}
	if idOf("aws-01").ID != idOf("aws-03").ID {
		t.Fatal("aws hosts do not share one identity")
	}

	// The file on disk holds paths only, and survives a restart.
	cfg, err := os.ReadFile(filepath.Join(a.cfg.Dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(cfg, []byte(filepath.Join(home, ".ssh", "aws.pem"))) {
		t.Fatal("config.json lacks the key path")
	}
	l, err := New(Config{Dir: a.cfg.Dir, Terminal: stillTerm{}}).Load(context.Background())
	if err != nil || len(l.Hosts) != 7 || len(l.Identities) != 2 {
		t.Fatalf("after restart: %d hosts, %d identities, %v", len(l.Hosts), len(l.Identities), err)
	}
}

func TestFR_IMPORT_003_SecondImportSkipsAll(t *testing.T) {
	a, _, _ := importApp(t, importFixture)
	first, err := a.ImportSSHConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(a.cfg.Dir, "config.json")
	before, err := os.ReadFile(file) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.ImportSSHConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Created) != 0 || len(second.Identities) != 0 || len(second.NotImported) != 0 {
		t.Fatalf("second import created %v, %v", second.Created, second.Identities)
	}
	var skipped []string
	for _, s := range second.Skipped {
		if s.Reason != SkipLabelExists {
			t.Errorf("%s skipped for %q", s.Alias, s.Reason)
		}
		skipped = append(skipped, s.Alias)
	}
	if !slices.Equal(skipped, first.Created) {
		t.Fatalf("skipped %v, want every alias %v", skipped, first.Created)
	}
	after, err := os.ReadFile(file) //nolint:gosec // test temp dir
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("config.json changed by an import that created nothing (%v)", err)
	}
	if len(second.AllHosts) != 7 {
		t.Fatalf("AllHosts = %d", len(second.AllHosts))
	}
}

func TestFR_IMPORT_003_ExistingLabelCaseInsensitive(t *testing.T) {
	a, _, _ := importApp(t, importFixture)
	if _, err := a.SaveHost(context.Background(), domain.Host{Label: "WEB", Hostname: "other"}); err != nil {
		t.Fatal(err)
	}
	rep, err := a.ImportSSHConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Skipped) != 1 || rep.Skipped[0].Alias != "web" || slices.Contains(rep.Created, "web") {
		t.Fatalf("skipped %+v, created %v", rep.Skipped, rep.Created)
	}
	// web was the only host using id_ed25519, so no identity for it.
	if slices.Contains(rep.Identities, "ubuntu (id_ed25519)") {
		t.Fatalf("identities %v", rep.Identities)
	}
}

func TestFR_IMPORT_003_UnsupportedListed(t *testing.T) {
	a, _, _ := importApp(t, importFixture)
	rep, err := a.ImportSSHConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for _, hd := range rep.NotImported {
		got[hd.Label] = hd.Directives
	}
	for _, want := range []string{"ProxyCommand", "ProxyJump", "LocalForward", "ForwardAgent", "Match"} {
		if !slices.Contains(got["bastion"], want) {
			t.Errorf("bastion: %v lacks %s", got["bastion"], want)
		}
	}
	if !slices.Contains(got["web"], "Match") || slices.Contains(got["web"], "ProxyCommand") {
		t.Errorf("web: %v", got["web"])
	}
}

func TestFR_IMPORT_001_SanitisesImportedStrings(t *testing.T) {
	a, _, _ := importApp(t, "Host evil\x1b[31mred\n    HostName h\x07ost\n    User u\x1b]52;c;ZXZpbA==\x07ser\n")
	rep, err := a.ImportSSHConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.AllHosts) != 1 {
		t.Fatalf("hosts %+v", rep.AllHosts)
	}
	h := rep.AllHosts[0]
	if h.Label != "evilred" || h.Hostname != "host" || h.User != "user" || h.Import.Alias != "evilred" {
		t.Fatalf("not sanitised: %q %q %q %q", h.Label, h.Hostname, h.User, h.Import.Alias)
	}
	if strings.ContainsAny(strings.Join(rep.Created, ""), "\x1b\x07") {
		t.Fatalf("report not sanitised: %q", rep.Created)
	}
}

func TestFR_IMPORT_001_NoSSHConfig(t *testing.T) {
	a, _, _ := importApp(t, "")
	if _, err := a.ImportSSHConfig(context.Background()); !errors.Is(err, ErrNoSSHConfig) {
		t.Fatalf("err = %v", err)
	}
}
