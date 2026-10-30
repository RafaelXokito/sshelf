package search

import (
	"slices"
	"testing"
	"time"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

// TestFR_NAV_018_SearchTieBreak: results are ordered by match score;
// equal scores put the more recently used host first, never-connected
// hosts after, keeping the list order among them.
func TestFR_NAV_018_SearchTieBreak(t *testing.T) {
	t0 := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	older, newer := t0.Add(-time.Hour), t0
	hs := []domain.Host{
		{Label: "api-1", Hostname: "a1"},
		{Label: "api-2", Hostname: "a2", LastConnectedAt: &older},
		{Label: "api-3", Hostname: "a3", LastConnectedAt: &newer},
		{Label: "api-4", Hostname: "a4"},
		{Label: "xapixx", Hostname: "x", LastConnectedAt: &newer}, // a weaker match, however recent
	}
	got := Filter(hs, "api")
	var labels []string
	for _, i := range got {
		labels = append(labels, hs[i].Label)
	}
	want := []string{"api-3", "api-2", "api-1", "api-4", "xapixx"}
	if !slices.Equal(labels, want) {
		t.Fatalf("Filter(api) = %q, want %q", labels, want)
	}
}
