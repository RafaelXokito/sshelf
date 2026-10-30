package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/RafaelXokito/sshelf/internal/app/apptest"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/session"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
)

func ago(d time.Duration) *time.Time {
	t := testEpoch.Add(-d)
	return &t
}

// recentHosts covers every relative-time form, never-connected hosts
// and a label long enough to be cut before the time column.
var recentHosts = []domain.Host{
	{Meta: domain.Meta{ID: "r1"}, Label: "zeta", Hostname: "10.0.0.30"},
	{Meta: domain.Meta{ID: "r2"}, Label: "db-staging", Hostname: "10.0.1.20", Port: 2222, User: "admin", LastConnectedAt: ago(2 * time.Hour)},
	{Meta: domain.Meta{ID: "r3"}, Label: "web-prod", Hostname: "10.0.0.10", User: "ubuntu", LastConnectedAt: ago(20 * time.Second)},
	{Meta: domain.Meta{ID: "r4"}, Label: "cache", Hostname: "cache.internal", User: "redis", LastConnectedAt: ago(5 * time.Minute)},
	{Meta: domain.Meta{ID: "r5"}, Label: "Bastion", Hostname: "bastion.example.com", Port: 2200, User: "jump"},
	{Meta: domain.Meta{ID: "r6"}, Label: "api-eu", Hostname: "api.eu.example.com", User: "deploy", LastConnectedAt: ago(3 * 24 * time.Hour)},
	{Meta: domain.Meta{ID: "r7"}, Label: "legacy-ftp", Hostname: "ftp.example.org", LastConnectedAt: ago(48 * 24 * time.Hour)},
	{Meta: domain.Meta{ID: "r8"}, Label: "archive", Hostname: "archive.example.org", LastConnectedAt: ago(400 * 24 * time.Hour)},
	{Meta: domain.Meta{ID: "r9"}, Label: "a-host-with-a-very-long-label-for-the-list", Hostname: "long-hostname.subdomain.example.com", User: "someone", LastConnectedAt: ago(26 * time.Hour)},
}

// TestFR_NAV_018_RowGolden renders the list in last-use order with the
// dim relative time at the end of each row, at 80×24 and 120×40.
func TestFR_NAV_018_RowGolden(t *testing.T) {
	d := newDriver(t, &fakeService{hosts: recentHosts})
	var got []domain.ID
	for _, h := range d.root().screen.(hostlist.Model).Hosts() {
		got = append(got, h.ID)
	}
	want := []domain.ID{"r3", "r4", "r2", "r9", "r6", "r7", "r8", "r5", "r1"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	golden(t, "hostlist-recent", d.m)
}

func shownIDs(d *driver) []domain.ID {
	var ids []domain.ID
	for _, h := range d.root().screen.(hostlist.Model).Shown() {
		ids = append(ids, h.ID)
	}
	return ids
}

// TestFR_NAV_018_ResortKeepsSelection: an established session moves its
// host to the top of the list, by the time the user is back on the Hosts
// tab, and a search keeps its query and ranks the new host first among
// equal matches.
func TestFR_NAV_018_ResortKeepsSelection(t *testing.T) {
	d := framedDriver(t, testHosts)
	if got := shownIDs(d); !slices.Equal(got, []domain.ID{"h1", "h2", "h3"}) {
		t.Fatalf("never-connected order = %v, want A–Z", got)
	}
	connectHost(t, d, "h3")
	d.prefix("h")
	if got := shownIDs(d); !slices.Equal(got, []domain.ID{"h3", "h1", "h2"}) {
		t.Fatalf("order after connecting zeta = %v", got)
	}
	if h := selected(t, d); h.ID != "h3" {
		t.Fatalf("selected %s, want the connected host", h.Label)
	}
	if h, _ := d.root().hostByID("h3"); h.LastConnectedAt == nil || !h.LastConnectedAt.Equal(testEpoch) {
		t.Fatalf("zeta's last use = %v, want the UI clock %v", h.LastConnectedAt, testEpoch)
	}
}

// TestFR_NAV_018_ReconnectResorts: reconnecting an ended tab counts as a
// use; the list is sorted again with the selection on the host it was on.
func TestFR_NAV_018_ReconnectResorts(t *testing.T) {
	first := apptest.NewFakeSession("web")
	d := framedDriver(t, testHosts, first)
	connectHost(t, d, "h2") // web-prod, tab 1
	d.clock = d.clock.Add(time.Minute)
	connectHost(t, d, "h1") // db-staging, tab 2
	d.focus(0)
	if got := shownIDs(d); !slices.Equal(got, []domain.ID{"h1", "h2", "h3"}) {
		t.Fatalf("order = %v", got)
	}
	if h := selected(t, d); h.ID != "h1" || d.root().screen.(hostlist.Model).Cursor() != 0 {
		t.Fatalf("selected %s", h.Label)
	}

	d.focus(1)
	endTab(d, 1, first, &session.ExitError{Status: 0})
	d.clock = d.clock.Add(time.Minute)
	d.press('r', "r")
	d.focus(0)
	if got := shownIDs(d); !slices.Equal(got, []domain.ID{"h2", "h1", "h3"}) {
		t.Fatalf("order after reconnecting web-prod = %v", got)
	}
	l := d.root().screen.(hostlist.Model)
	if h := selected(t, d); h.ID != "h1" || l.Cursor() != 1 {
		t.Fatalf("selected %s on row %d, want db-staging on row 1", h.Label, l.Cursor())
	}
}
