package sshconfig

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "regenerate testdata/sshg.golden.json with ssh -G")

const goldenPath = "testdata/sshg.golden.json"

// fixture loads testdata/<name>/.ssh/config with HOME pointing at
// testdata/<name>. Tests never read the real ~/.ssh.
func fixture(t *testing.T, name string) (*Config, string) {
	t.Helper()
	home, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	c, err := Load(DefaultPath(home), Options{Home: home, LocalUser: "localuser"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return c, home
}

func planHost(t *testing.T, p Plan, alias string) HostPlan {
	t.Helper()
	for _, h := range p.Hosts {
		if h.Host.Label == alias {
			return h
		}
	}
	t.Fatalf("no host %q in plan", alias)
	return HostPlan{}
}

func TestFR_IMPORT_001_MapsFields(t *testing.T) {
	c, home := fixture(t, "home")
	p := c.Plan()
	web := planHost(t, p, "web")
	if web.Host.Hostname != "10.0.0.10" || web.Host.User != "ubuntu" || web.Host.Port != 0 {
		t.Fatalf("web = %+v", web.Host)
	}
	if web.Identity < 0 {
		t.Fatal("web has no identity")
	}
	id := p.Identities[web.Identity]
	if id.Username != "ubuntu" || id.KeyPath != filepath.Join(home, ".ssh", "id_ed25519") || id.Label != "ubuntu (id_ed25519)" {
		t.Fatalf("web identity = %+v", id)
	}
	if id.HasKey || id.HasPassword || id.HasPassphrase {
		t.Fatalf("import must reference the key by path only: %+v", id)
	}
	if web.Host.Import == nil || web.Host.Import.Alias != "web" || web.Host.Import.File != DefaultPath(home) || web.Host.Import.Line != 7 {
		t.Fatalf("web import source = %+v", web.Host.Import)
	}

	// First value wins across Include (config.d sets Port 7000 first) and
	// tokens in IdentityFile are expanded.
	db := planHost(t, p, "db")
	if db.Host.Port != 7000 || db.Host.User != "postgres" || db.Host.Hostname != "db.example" {
		t.Fatalf("db = %+v", db.Host)
	}
	if got, want := p.Identities[db.Identity].KeyPath, filepath.Join(home, ".ssh", "k_postgres_7000_db.example_db"); got != want {
		t.Fatalf("db key = %q, want %q", got, want)
	}
	if !slices.Contains(db.Host.Import.Unsupported, unsupportedExtraKeys) {
		t.Fatalf("db second IdentityFile not reported: %v", db.Host.Import.Unsupported)
	}

	// Negated pattern: prod-secret is excluded from "Host prod-* !prod-secret".
	if h := planHost(t, p, "prod-app").Host; h.Port != 2201 || h.User != "prod" {
		t.Fatalf("prod-app = %+v", h)
	}
	if h := planHost(t, p, "prod-secret").Host; h.Port != 0 || h.User != "fallback" {
		t.Fatalf("prod-secret = %+v", h)
	}

	for _, h := range p.Hosts {
		if strings.ContainsAny(h.Host.Label, "*?!") {
			t.Fatalf("wildcard pattern imported as host %q", h.Host.Label)
		}
	}
}

func TestFR_IMPORT_001_AliasWithoutHostName(t *testing.T) {
	c, _ := fixture(t, "home")
	if h := planHost(t, c.Plan(), "plain").Host; h.Hostname != "plain" {
		t.Fatalf("plain hostname = %q, want the alias", h.Hostname)
	}
}

func TestFR_IMPORT_001_MultipleAliases(t *testing.T) {
	c, _ := fixture(t, "home")
	p := c.Plan()
	for _, alias := range []string{"a", "b"} {
		if h := planHost(t, p, alias).Host; h.Hostname != alias+".internal.example" {
			t.Fatalf("%s hostname = %q", alias, h.Hostname)
		}
	}
}

func TestFR_IMPORT_001_SharedIdentityDedup(t *testing.T) {
	c, home := fixture(t, "home")
	p := c.Plan()
	idx := -1
	for i := 1; i <= 10; i++ {
		h := planHost(t, p, fmt.Sprintf("aws-%02d", i))
		if idx == -1 {
			idx = h.Identity
		}
		if h.Identity != idx || idx < 0 {
			t.Fatalf("%s identity %d, want shared %d", h.Host.Label, h.Identity, idx)
		}
	}
	id := p.Identities[idx]
	if id.Label != "ubuntu (aws.pem)" || id.KeyPath != filepath.Join(home, ".ssh", "aws.pem") {
		t.Fatalf("shared identity = %+v", id)
	}
	n := 0
	for _, other := range p.Identities {
		if other.KeyPath == id.KeyPath && other.Username == id.Username {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d identities for ubuntu+aws.pem, want 1", n)
	}
}

func TestFR_IMPORT_002_IncludeRelativeToSSHDir(t *testing.T) {
	c, _ := fixture(t, "home")
	p := c.Plan()
	if h := planHost(t, p, "team1").Host; h.Hostname != "team1.example" || h.User != "team" {
		t.Fatalf("team1 (config.d/*) = %+v", h)
	}
	// Nested Include relative to ~/.ssh, then "~/" Include inside a Host block.
	if h := planHost(t, p, "deep").Host; h.Hostname != "deep.example" || h.User != "deep-user" {
		t.Fatalf("deep (config.d/nested, ~/.ssh/extra) = %+v", h)
	}
	if h := planHost(t, p, "cond").Host; h.Hostname != "cond.example" || h.User != "cond-user" {
		t.Fatalf("cond (Include inside Host) = %+v", h)
	}
	// The conditional include must not leak into other hosts.
	if h := planHost(t, p, "plain").Host; h.User != "fallback" {
		t.Fatalf("plain user = %q", h.User)
	}
}

func TestFR_IMPORT_002_IncludeCycleFails(t *testing.T) {
	home, _ := filepath.Abs("testdata/loop")
	_, err := Load(DefaultPath(home), Options{Home: home})
	if err == nil || !strings.Contains(err.Error(), "nested more than") {
		t.Fatalf("err = %v, want include depth error", err)
	}
}

func TestFR_IMPORT_002_MatchSkippedAndReported(t *testing.T) {
	c, home := fixture(t, "home")
	p := c.Plan()
	if len(p.Matches) != 1 || p.Matches[0] != (Location{File: DefaultPath(home), Line: 48}) {
		t.Fatalf("matches = %+v", p.Matches)
	}
	for _, h := range p.Hosts {
		if h.Host.User == "from-match" {
			t.Fatalf("%s took a value from a Match block", h.Host.Label)
		}
		if !slices.Contains(h.Host.Import.Unsupported, "Match") {
			t.Fatalf("%s does not report Match: %v", h.Host.Label, h.Host.Import.Unsupported)
		}
	}
}

func TestFR_IMPORT_003_PlanListsUnsupported(t *testing.T) {
	c, _ := fixture(t, "home")
	got := planHost(t, c.Plan(), "bastion").Host.Import.Unsupported
	want := []string{"ProxyCommand", "ProxyJump", "LocalForward", "ForwardAgent", "ServerAliveInterval", "Match"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bastion unsupported = %q, want %q", got, want)
	}
}

func TestParseLine(t *testing.T) {
	tests := []struct {
		in   string
		key  string
		args []string
		err  bool
	}{
		{in: "  # comment"},
		{in: ""},
		{in: "Host a b", key: "host", args: []string{"a", "b"}},
		{in: "HostName=x", key: "hostname", args: []string{"x"}},
		{in: "Port = 22 # c", key: "port", args: []string{"22"}},
		{in: `IdentityFile "/a b/c"`, key: "identityfile", args: []string{"/a b/c"}},
		{in: `IdentityFile /a\ b`, key: "identityfile", args: []string{"/a b"}},
		{in: "User a#b", key: "user", args: []string{"a#b"}},
		{in: "User", err: true},
		{in: "User # only comment", err: true},
		{in: `User "x`, err: true},
	}
	for _, tt := range tests {
		ln, ok, err := parseLine(tt.in)
		if (err != nil) != tt.err {
			t.Fatalf("%q: err = %v", tt.in, err)
		}
		if tt.err {
			continue
		}
		if ok != (tt.key != "") || ln.key != tt.key || !reflect.DeepEqual(ln.args, tt.args) {
			t.Fatalf("%q = %q %q ok=%v", tt.in, ln.key, ln.args, ok)
		}
	}
}

// sshG is the subset of `ssh -G` output the import maps.
type sshG struct {
	HostName      string   `json:"hostname"`
	User          string   `json:"user"`
	Port          string   `json:"port"`
	IdentityFiles []string `json:"identityfile"`
}

// runSSHG runs `ssh -G` (print the config and exit; nothing is connected or
// executed) against the fixture with HOME pointing at it.
func runSSHG(t *testing.T, sshPath, home, alias string) sshG {
	t.Helper()
	cmd := exec.Command(sshPath, "-G", "-F", DefaultPath(home), alias) //nolint:gosec // test only, fixed binary
	cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("ssh -G %s: %v", alias, err)
	}
	var g sshG
	for _, l := range strings.Split(string(out), "\n") {
		k, v, _ := strings.Cut(l, " ")
		switch k {
		case "hostname":
			g.HostName = v
		case "user":
			g.User = v
		case "port":
			g.Port = v
		case "identityfile":
			g.IdentityFiles = append(g.IdentityFiles, v)
		}
	}
	return g
}

func compareSSHG(t *testing.T, c *Config, p Plan, want map[string]sshG, source string) {
	t.Helper()
	if len(want) != len(p.Hosts) {
		t.Errorf("%s: %d aliases, plan has %d", source, len(want), len(p.Hosts))
	}
	for _, hp := range p.Hosts {
		alias := hp.Host.Label
		w, ok := want[alias]
		if !ok {
			t.Errorf("%s: no entry for %q", source, alias)
			continue
		}
		port := "22"
		if hp.Host.Port != 0 {
			port = strconv.Itoa(hp.Host.Port)
		}
		got := sshG{HostName: hp.Host.Hostname, User: hp.Host.User, Port: port, IdentityFiles: c.Resolve(alias).IdentityFiles}
		if len(got.IdentityFiles) == 0 {
			// ssh lists its built-in defaults; an import references none.
			for _, f := range w.IdentityFiles {
				if !strings.HasPrefix(f, "~/.ssh/id_") {
					t.Errorf("%s: %s: ssh has identityfile %q, import has none", source, alias, f)
				}
			}
			w.IdentityFiles = nil
		}
		if !reflect.DeepEqual(got, w) {
			t.Errorf("%s: %s:\n got  %+v\n want %+v", source, alias, got, w)
		}
	}
}

func TestFR_IMPORT_002_MatchesSSHG(t *testing.T) {
	c, home := fixture(t, "home")
	p := c.Plan()
	sshPath, lookErr := exec.LookPath("ssh")

	if *update {
		if lookErr != nil {
			t.Fatal("-update needs ssh on PATH")
		}
		want := map[string]sshG{}
		for _, hp := range p.Hosts {
			want[hp.Host.Label] = runSSHG(t, sshPath, home, hp.Host.Label)
		}
		data, err := json.MarshalIndent(want, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string]sshG
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&golden); err != nil {
		t.Fatal(err)
	}
	compareSSHG(t, c, p, golden, "golden")

	if lookErr != nil {
		t.Log("ssh not on PATH; live ssh -G check skipped")
		return
	}
	live := map[string]sshG{}
	for _, hp := range p.Hosts {
		live[hp.Host.Label] = runSSHG(t, sshPath, home, hp.Host.Label)
	}
	compareSSHG(t, c, p, live, "live ssh -G")
}
