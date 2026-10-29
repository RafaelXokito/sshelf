package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/confirm"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
)

// DeleteHost mimics app.App.DeleteHost in memory.
func (f *fakeService) DeleteHost(_ context.Context, id domain.ID) (app.DeletedHost, error) {
	f.checkGoroutine("DeleteHost")
	f.mu.Lock()
	defer f.mu.Unlock()
	i := slices.IndexFunc(f.hosts, func(h domain.Host) bool { return h.ID == id })
	if i < 0 {
		return app.DeletedHost{}, app.ErrHostNotFound
	}
	gone := f.hosts[i]
	f.hosts = slices.Delete(slices.Clone(f.hosts), i, i+1)
	// A hidden identity goes with its host (FR-HOST-010).
	f.identities = slices.DeleteFunc(slices.Clone(f.identities), func(o domain.Identity) bool {
		return o.Hidden && o.ID == gone.IdentityID
	})
	return app.DeletedHost{Hosts: slices.Clone(f.hosts), Identities: slices.Clone(f.identities)}, nil
}

// withScreen returns a loaded root model showing s.
func withScreen(s components.Screen) tea.Model {
	var m tea.Model = New(&fakeService{hosts: testHosts})
	m, _ = m.Update(LoadedMsg{Hosts: testHosts})
	r := m.(Model)
	r.screen = s
	return r
}

func (d *driver) resize(w, h int) { d.send(tea.WindowSizeMsg{Width: w, Height: h}) }

func (d *driver) list() hostlist.Model {
	d.t.Helper()
	l, ok := d.root().screen.(hostlist.Model)
	if !ok {
		d.t.Fatalf("not on the host list:\n%s", d.view())
	}
	return l
}

func TestFR_NAV_001_SortedPort22OmittedTruncated(t *testing.T) {
	long := strings.Repeat("very-long-label-", 4) // 64 characters
	hosts := []domain.Host{
		{Meta: domain.Meta{ID: "z"}, Label: "zeta", Hostname: "10.0.0.30", Port: 22},
		{Meta: domain.Meta{ID: "w"}, Label: "Web-Prod", Hostname: "10.0.0.10", User: "ubuntu"},
		{Meta: domain.Meta{ID: "v6"}, Label: "ipv6", Hostname: "2001:db8::1", Port: 2222, User: "root"},
		{Meta: domain.Meta{ID: "l"}, Label: long, Hostname: "long.example.com", User: "someone-with-a-long-name"},
		{Meta: domain.Meta{ID: "a"}, Label: "api", Hostname: "api.internal"},
	}
	d := newDriver(t, &fakeService{hosts: hosts})
	d.resize(80, 24)
	var labels []string
	for _, h := range d.list().Hosts() {
		labels = append(labels, h.Label)
	}
	if want := []string{"api", "ipv6", long, "Web-Prod", "zeta"}; !slices.Equal(labels, want) {
		t.Fatalf("order %q, want %q", labels, want)
	}
	v := plain(d.view())
	for _, want := range []string{"> api                  api.internal", "  ipv6                 root@[2001:db8::1]:2222", "  Web-Prod             ubuntu@10.0.0.10\n", "  zeta                 10.0.0.30\n"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, ":22\n") {
		t.Errorf("port 22 shown:\n%s", v)
	}
	l := lineWith(v, "very-long-label")
	if !strings.HasSuffix(l, "…") || lipgloss.Width(l) != 80 {
		t.Errorf("long line not truncated to 80 columns with …: %q (%d)", l, lipgloss.Width(l))
	}
	golden(t, "hostlist", d.m)
}

func TestFR_NAV_003_MoveKeys(t *testing.T) {
	d := newDriver(t, &fakeService{hosts: testHosts})
	for _, step := range []struct {
		code rune
		text string
		want domain.ID
	}{
		{'j', "j", "h2"}, {tea.KeyDown, "", "h3"}, {tea.KeyDown, "", "h3"}, {'k', "k", "h2"}, {tea.KeyUp, "", "h1"}, {'k', "k", "h1"},
	} {
		d.press(step.code, step.text)
		if h := selected(t, d); h.ID != step.want {
			t.Fatalf("after %q: selected %s, want %s", step.text, h.ID, step.want)
		}
	}
}

func TestFR_NAV_003_FirstLast(t *testing.T) {
	var hosts []domain.Host
	for i := range 100 {
		hosts = append(hosts, domain.Host{Meta: domain.Meta{ID: domain.ID(fmt.Sprint(i))}, Label: fmt.Sprintf("host-%03d", i), Hostname: "h"})
	}
	d := newDriver(t, &fakeService{hosts: hosts})
	d.resize(80, 24)
	d.press('G', "G")
	if h := selected(t, d); h.Label != "host-099" || !strings.Contains(d.view(), "> host-099") {
		t.Fatalf("G selected %s:\n%s", h.Label, d.view())
	}
	d.press('g', "g")
	if h := selected(t, d); h.Label != "host-000" || !strings.Contains(d.view(), "> host-000") {
		t.Fatalf("g selected %s:\n%s", h.Label, d.view())
	}
	d.press(tea.KeyEnd, "")
	d.press(tea.KeyHome, "")
	if h := selected(t, d); h.Label != "host-000" {
		t.Fatalf("end, home selected %s", h.Label)
	}
}

func TestFR_NAV_003_AddEditDelete(t *testing.T) {
	svc := formService()
	d := newDriver(t, svc)
	d.press('a', "a")
	if !d.inForm() || !strings.Contains(d.view(), "Add host") {
		t.Fatalf("a: %s", d.view())
	}
	d.esc()
	d.press('j', "j")
	d.press('e', "e")
	if !strings.Contains(d.view(), "Edit host web-prod") {
		t.Fatalf("e: %s", d.view())
	}
	d.esc()
	if h := selected(t, d); h.ID != "h2" {
		t.Fatalf("esc from the form lost the selection: %s", h.ID)
	}
	d.press('d', "d")
	d.press('y', "y")
	if slices.ContainsFunc(svc.hosts, func(h domain.Host) bool { return h.ID == "h2" }) {
		t.Fatal("d, y did not delete web-prod")
	}
	v := d.view()
	if strings.Contains(v, "ubuntu@10.0.0.10") || !strings.Contains(v, "web-prod: deleted") {
		t.Fatalf("view after delete:\n%s", v)
	}
	if h := selected(t, d); h.ID != "h3" {
		t.Fatalf("after deleting row 2, selected %s, want h3 (now row 2)", h.ID)
	}
}

func TestFR_NAV_003_SearchFieldTakesLetters(t *testing.T) {
	svc := formService()
	d := newDriver(t, svc)
	d.press('/', "/")
	if !d.list().Searching() {
		t.Fatal("/ did not focus the search field")
	}
	d.typeText("aedqjkgGiI/")
	l := d.list()
	if l.Query() != "aedqjkgGiI/" || d.quit || len(d.root().modals) != 0 || d.inForm() {
		t.Fatalf("query %q, quit %v, modals %d", l.Query(), d.quit, len(d.root().modals))
	}
	if v := plain(d.view()); !strings.Contains(v, "/ aedqjkgGiI/_") {
		t.Fatalf("search line not shown:\n%s", v)
	}
	d.esc()
	if l := d.list(); l.Searching() || l.Query() != "" {
		t.Fatalf("esc: searching %v, query %q", l.Searching(), l.Query())
	}
	d.press('q', "q")
	if !d.quit {
		t.Fatal("q after leaving search did not quit")
	}
}

func TestFR_NAV_003_EscBackQOnlyFromList(t *testing.T) {
	svc := formService()
	d := newDriver(t, svc)
	// In the form, q is a letter, and esc goes back.
	d.press('a', "a")
	d.press('q', "q")
	if d.quit || !strings.Contains(d.view(), "q_") {
		t.Fatalf("q in the form: quit %v:\n%s", d.quit, d.view())
	}
	d.key('u', tea.ModCtrl)
	d.esc()
	if d.inForm() {
		t.Fatal("esc did not leave the form")
	}
	// In the confirm modal, q does nothing; esc closes it.
	d.press('d', "d")
	d.press('q', "q")
	if d.quit || len(d.root().modals) != 1 {
		t.Fatalf("q in the confirm modal: quit %v", d.quit)
	}
	d.esc()
	if len(d.root().modals) != 0 || len(svc.hosts) != 3 {
		t.Fatal("esc did not close the confirm modal without deleting")
	}
	// On the list, esc does nothing and q quits.
	d.esc()
	if d.quit {
		t.Fatal("esc quit")
	}
	d.press('q', "q")
	if !d.quit {
		t.Fatal("q on the list did not quit")
	}
}

func TestFR_NAV_004_FooterPerView(t *testing.T) {
	d := newDriver(t, formService())
	d.resize(80, 24)
	lastLine := func() string {
		lines := strings.Split(plain(d.view()), "\n")
		return lines[len(lines)-1]
	}
	list := lastLine()
	for _, want := range []string{"enter connect", "/ search", "a add", "q quit"} {
		if !strings.Contains(list, want) {
			t.Errorf("list footer %q lacks %q", list, want)
		}
	}
	d.press('/', "/")
	if f := lastLine(); f == list || !strings.Contains(f, "esc clear search") {
		t.Errorf("search footer %q", f)
	}
	d.esc()
	d.press('a', "a")
	if f := lastLine(); f == list || !strings.Contains(f, "ctrl+s save") {
		t.Errorf("form footer %q", f)
	}
	d.esc()
	d.press('d', "d")
	if f := lastLine(); f != "y yes • n/enter/esc no" {
		t.Errorf("confirm footer %q", f)
	}
}

func TestFR_HOST_002_ConfirmNamesHostDefaultsNo(t *testing.T) {
	svc := formService()
	d := newDriver(t, svc)
	d.press('j', "j")
	d.press('d', "d")
	if _, ok := d.root().modals[0].(confirm.Model); !ok || !strings.Contains(d.view(), "Delete host web-prod?") {
		t.Fatalf("no confirm naming the host:\n%s", d.view())
	}
	golden(t, "hostlist-delete", d.m)
	for _, no := range []struct {
		code rune
		text string
	}{{tea.KeyEnter, ""}, {'n', "n"}, {tea.KeyEscape, ""}} {
		if len(d.root().modals) == 0 {
			d.press('d', "d")
		}
		d.press(no.code, no.text)
		if len(d.root().modals) != 0 || len(svc.hosts) != 3 {
			t.Fatalf("%q: modals %d, hosts %d", no.text, len(d.root().modals), len(svc.hosts))
		}
	}
	d.press('d', "d")
	d.press('y', "y")
	if len(svc.hosts) != 2 {
		t.Fatalf("y did not delete: %d hosts", len(svc.hosts))
	}
}

// NFR-UX-002: every screen and modal fits at 80×24 and 120×40 with the
// footer on the last line, here with a list longer than the terminal.
func TestNFR_UX_002_LinesFitAndFooterVisible(t *testing.T) {
	var hosts []domain.Host
	for i := range 200 {
		hosts = append(hosts, domain.Host{
			Meta: domain.Meta{ID: domain.ID(fmt.Sprint(i))}, Label: fmt.Sprintf("host-%03d-%s", i, strings.Repeat("x", 50)),
			Hostname: fmt.Sprintf("10.0.%d.%d", i/250, i%250), User: strings.Repeat("u", 40),
		})
	}
	check := func(name string, m tea.Model) {
		t.Helper()
		for _, sz := range goldenSizes {
			sm, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
			t.Run(fmt.Sprintf("%s/%dx%d", name, sz.w, sz.h), func(t *testing.T) {
				checkLayout(t, sm.(Model), sm.View().Content, sz.w, sz.h)
			})
		}
	}
	d := newDriver(t, &fakeService{hosts: hosts, identities: testIdentities})
	d.resize(80, 24)
	check("top", d.m)
	d.press('G', "G")
	if !strings.Contains(d.view(), "> host-199") {
		t.Fatalf("the last host is not visible after G:\n%s", d.view())
	}
	check("bottom", d.m)
	d.press('/', "/")
	check("search", d.m)
	d.esc()
	d.press('d', "d")
	check("confirm", d.m)
	d.esc()
	d.press('e', "e")
	check("form", d.m)
	d.send(HostSaveFailedMsg{Err: fmt.Errorf("could not save config.json: %s", strings.Repeat("disk full ", 20))})
	check("form-error", d.m)
}

// NFR-UX-003: rendered without colour, selection, focus and errors still
// carry a glyph or text marker.
func TestNFR_UX_003_AsciiProfile(t *testing.T) {
	d := newDriver(t, formService())
	d.press('j', "j")
	if !strings.Contains(plain(d.view()), hostlist.SelectGlyph+"web-prod") {
		t.Fatalf("selected row has no glyph:\n%s", plain(d.view()))
	}
	d.press('a', "a")
	v := plain(d.view())
	if !strings.Contains(v, "> Label") {
		t.Fatalf("focused field has no glyph:\n%s", v)
	}
	d.save()
	if v := plain(d.view()); !strings.Contains(v, components.ErrorPrefix+domain.MsgLabelRequired) {
		t.Fatalf("error has no prefix:\n%s", v)
	}
	d.key('c', tea.ModCtrl)
	d.send(HostDeleteFailedMsg{Label: "web-prod", Err: fmt.Errorf("disk full")})
	if v := plain(d.view()); !strings.Contains(v, components.ErrorPrefix+"web-prod: not deleted") {
		t.Fatalf("status error has no prefix:\n%s", v)
	}
}
