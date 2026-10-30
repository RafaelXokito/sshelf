package domain

import (
	"slices"
	"testing"
	"time"
)

func at(t time.Time) *time.Time { return &t }

func TestFR_NAV_018_Order(t *testing.T) {
	t0 := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	hosts := []Host{
		{Label: "zeta"},
		{Label: "old", LastConnectedAt: at(t0.Add(-48 * time.Hour))},
		{Label: "Beta"},
		{Label: "recent", LastConnectedAt: at(t0)},
		{Label: "alpha"},
		{Label: "tie-b", LastConnectedAt: at(t0.Add(-time.Hour))},
		{Label: "Tie-A", LastConnectedAt: at(t0.Add(-time.Hour))},
		// The same instant in another zone is an equal time.
		{Label: "tie-c", LastConnectedAt: at(t0.Add(-time.Hour).In(time.FixedZone("x", 3600)))},
	}
	got := SortByLastUse(hosts)
	var labels []string
	for _, h := range got {
		labels = append(labels, h.Label)
	}
	want := []string{"recent", "Tie-A", "tie-b", "tie-c", "old", "alpha", "Beta", "zeta"}
	if !slices.Equal(labels, want) {
		t.Fatalf("order = %q, want %q", labels, want)
	}
	if hosts[0].Label != "zeta" {
		t.Fatal("SortByLastUse changed its argument")
	}
	if c := CompareRecency(Host{}, Host{}); c != 0 {
		t.Errorf("two never-connected hosts compare %d", c)
	}
}
