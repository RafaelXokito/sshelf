package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
	importscreen "github.com/RafaelXokito/sshelf/internal/ui/screens/import"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/welcome"
)

// fakeSSHConfig is what the fake service's ImportSSHConfig finds in
// ~/.ssh/config, with the directives each host does not import.
var fakeSSHConfig = []struct {
	host        domain.Host
	unsupported []string
}{
	{domain.Host{Label: "web", Hostname: "10.0.0.10", User: "ubuntu"}, []string{"Match"}},
	{domain.Host{Label: "bastion", Hostname: "bastion.example"}, []string{"ProxyCommand", "ProxyJump", "LocalForward", "ForwardAgent", "Match"}},
	{domain.Host{Label: "db", Hostname: "db.example", Port: 5432, User: "postgres"}, []string{"Match"}},
}

// failingImport is the fake service with an ImportSSHConfig that fails.
type failingImport struct{ *fakeService }

func (failingImport) ImportSSHConfig(context.Context) (app.ImportReport, error) {
	return app.ImportReport{}, app.ErrNoSSHConfig
}

// ImportSSHConfig mimics app.App.ImportSSHConfig in memory.
func (f *fakeService) ImportSSHConfig(context.Context) (app.ImportReport, error) {
	f.checkGoroutine("ImportSSHConfig")
	f.mu.Lock()
	defer f.mu.Unlock()
	rep := app.ImportReport{File: "/home/u/.ssh/config"}
	for i, e := range fakeSSHConfig {
		if slices.ContainsFunc(f.hosts, func(h domain.Host) bool { return strings.EqualFold(h.Label, e.host.Label) }) {
			rep.Skipped = append(rep.Skipped, app.SkippedAlias{Alias: e.host.Label, Reason: app.SkipLabelExists})
			continue
		}
		h := e.host
		h.ID = domain.ID(fmt.Sprintf("imp%d", i))
		f.hosts = append(f.hosts, h)
		rep.Created = append(rep.Created, h.Label)
		rep.NotImported = append(rep.NotImported, app.HostDirectives{Label: h.Label, Directives: e.unsupported})
	}
	if len(rep.Created) > 0 {
		rep.Identities = []string{"ubuntu (id_ed25519)"}
	}
	rep.AllHosts = slices.Clone(f.hosts)
	return rep, nil
}

func (d *driver) report() (importscreen.Model, bool) {
	r, ok := d.root().screen.(importscreen.Model)
	return r, ok
}

func TestFR_NAV_003_CapitalIStartsImport(t *testing.T) {
	svc := &fakeService{hosts: slices.Clone(testHosts)}
	d := newDriver(t, svc)
	d.press('I', "I")
	if _, ok := d.report(); !ok {
		t.Fatalf("I did not show the import report:\n%s", d.view())
	}
	if len(svc.hosts) != len(testHosts)+3 {
		t.Fatalf("hosts after import: %d", len(svc.hosts))
	}
	// Enter goes to the host list, which now shows the imported hosts.
	d.press(tea.KeyEnter, "")
	var labels []string
	for _, h := range d.list().Hosts() {
		labels = append(labels, h.Label)
	}
	if want := []string{"bastion", "db", "db-staging", "web", "web-prod", "zeta"}; !slices.Equal(labels, want) {
		t.Fatalf("list %v, want %v", labels, want)
	}
	// i (identities) is T-019's: it must not start an import.
	d.press('i', "i")
	if _, ok := d.report(); ok {
		t.Fatal("i started an import")
	}
}

func TestFR_IMPORT_003_ReportGolden(t *testing.T) {
	svc := &fakeService{hosts: []domain.Host{{Meta: domain.Meta{ID: "x"}, Label: "DB", Hostname: "old"}}}
	d := newDriver(t, svc)
	d.press('I', "I")
	r, ok := d.report()
	if !ok {
		t.Fatalf("no report:\n%s", d.view())
	}
	v := plain(d.view())
	for _, want := range []string{
		"Created 2 hosts and 1 identity.",
		"Skipped (1):", "  db: label already exists",
		"Not imported:", "  bastion: ProxyCommand, ProxyJump, LocalForward, ForwardAgent, Match",
		"enter/esc back to hosts",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("report lacks %q:\n%s", want, v)
		}
	}
	if len(r.Report().Created) != 2 {
		t.Fatalf("report %+v", r.Report())
	}
	golden(t, "import-report", d.m)
	d.esc()
	if _, ok := d.root().screen.(hostlist.Model); !ok {
		t.Fatalf("esc did not go to the host list:\n%s", d.view())
	}
}

func TestFR_IMPORT_003_SecondImportSkipsAllUI(t *testing.T) {
	d := newDriver(t, &fakeService{hosts: slices.Clone(testHosts)})
	d.press('I', "I")
	d.press(tea.KeyEnter, "")
	d.press('I', "I")
	v := plain(d.view())
	for _, want := range []string{"Created 0 hosts and 0 identities.", "Skipped (3):", "  web: label already exists", "  bastion: label already exists", "  db: label already exists"} {
		if !strings.Contains(v, want) {
			t.Errorf("second report lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "Not imported:") {
		t.Errorf("second report lists directives of hosts it did not create:\n%s", v)
	}
}

func TestFR_IMPORT_001_FromWelcomeLandsOnList(t *testing.T) {
	d := newDriver(t, &fakeService{})
	if !d.onWelcome() {
		t.Fatal("not on Welcome")
	}
	d.press('I', "I")
	d.press(tea.KeyEnter, "")
	if l := d.list(); len(l.Hosts()) != 3 {
		t.Fatalf("hosts %+v", l.Hosts())
	}
}

func TestFR_IMPORT_001_FailureShown(t *testing.T) {
	var m tea.Model = New(failingImport{&fakeService{}})
	m, _ = m.Update(LoadedMsg{})
	m, cmd := m.Update(tea.KeyPressMsg{Code: 'I', Text: "I"})
	m, cmd = m.Update(cmd())
	m, _ = m.Update(cmd())
	if _, ok := m.(Model).screen.(welcome.Model); !ok || !strings.Contains(m.View().Content, "✗ import failed: no ~/.ssh/config found") {
		t.Fatalf("view:\n%s", m.View().Content)
	}
}

func TestImportReportScrolls(t *testing.T) {
	rep := app.ImportReport{File: "/home/u/.ssh/config"}
	for i := range 60 {
		rep.Skipped = append(rep.Skipped, app.SkippedAlias{Alias: fmt.Sprintf("host-%02d", i), Reason: app.SkipLabelExists})
	}
	m := withScreen(importscreen.New(rep))
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if v := plain(m.View().Content); !strings.Contains(v, "host-00") || strings.Contains(v, "host-59") || !strings.Contains(v, "↓/j down") {
		t.Fatalf("top of a long report:\n%s", v)
	}
	for range 100 {
		m, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	}
	if v := plain(m.View().Content); !strings.Contains(v, "host-59") {
		t.Fatalf("bottom of a long report:\n%s", v)
	}
	checkLayout(t, m.(Model), m.View().Content, 80, 24)
}
