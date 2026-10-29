package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

var searchHosts = []domain.Host{
	{Meta: domain.Meta{ID: "s1"}, Label: "db-staging", Hostname: "10.0.1.20", User: "admin"},
	{Meta: domain.Meta{ID: "s2"}, Label: "web-prod", Hostname: "10.0.0.10", User: "ubuntu"},
	{Meta: domain.Meta{ID: "s3"}, Label: "web-prod-eu", Hostname: "eu.example.com", User: "deploy"},
	{Meta: domain.Meta{ID: "s4"}, Label: "zeta", Hostname: "zeta.internal"},
}

func shownLabels(d *driver) []string {
	var out []string
	for _, h := range d.list().Shown() {
		out = append(out, h.Label)
	}
	return out
}

func TestFR_NAV_002_EnterConnectsTopMatch(t *testing.T) {
	svc := &fakeService{hosts: searchHosts, session: &stubSession{}}
	d := newDriver(t, svc)
	d.press('j', "j")
	d.press('j', "j") // web-prod-eu selected before searching
	d.press('/', "/")
	d.typeText("wbprd")
	if got := shownLabels(d); len(got) != 2 || got[0] != "web-prod" || !slices.Contains(got, "web-prod-eu") {
		t.Fatalf("shown %v", got)
	}
	if h := selected(t, d); h.Label != "web-prod" {
		t.Fatalf("top match not selected: %s", h.Label)
	}
	golden(t, "search", d.m)
	d.press(tea.KeyEnter, "")
	if len(svc.connected) != 1 || svc.connected[0] != "s2" {
		t.Fatalf("connected %v, want web-prod (s2)", svc.connected)
	}

	// The arrows pick another result; letters keep typing.
	d.press('/', "/") // the field still has focus: / is part of the query
	d.key(tea.KeyBackspace, 0)
	d.key(tea.KeyDown, 0)
	d.press(tea.KeyEnter, "")
	if len(svc.connected) != 2 || svc.connected[1] != "s3" {
		t.Fatalf("connected %v, want web-prod-eu (s3) second", svc.connected)
	}

	// Hostname and user match too, case-insensitively.
	for q, want := range map[string]string{"10.0.1": "db-staging", "DEPLOY": "web-prod-eu", "intern": "zeta"} {
		d.key('u', tea.ModCtrl)
		d.typeText(q)
		if got := shownLabels(d); len(got) == 0 || got[0] != want {
			t.Errorf("%q: shown %v, want %s first", q, got, want)
		}
	}
	d.key('u', tea.ModCtrl)
	d.typeText("nothing-matches")
	if v := d.view(); !strings.Contains(v, "No host matches the search.") {
		t.Fatalf("no empty-result message:\n%s", v)
	}
	d.press(tea.KeyEnter, "") // nothing to connect to
	if len(svc.connected) != 2 {
		t.Fatalf("connected %v", svc.connected)
	}
}

func TestFR_NAV_002_EscClearsQuery(t *testing.T) {
	d := newDriver(t, &fakeService{hosts: searchHosts})
	d.press('/', "/")
	d.typeText("zeta")
	d.esc()
	l := d.list()
	if l.Searching() || l.Query() != "" || len(l.Shown()) != len(searchHosts) {
		t.Fatalf("after esc: searching %v, query %q, %d shown", l.Searching(), l.Query(), len(l.Shown()))
	}
	if h := selected(t, d); h.Label != "zeta" {
		t.Fatalf("the selected result is not kept: %s", h.Label)
	}
	if v := plain(d.view()); strings.Contains(v, "/ zeta") || strings.Contains(v, "\n/ ") {
		t.Fatalf("search line still shown:\n%s", v)
	}
	// Focus is back on the list: k moves instead of typing.
	d.press('k', "k")
	if h := selected(t, d); h.Label != "web-prod-eu" {
		t.Fatalf("k after esc selected %s", h.Label)
	}
}
