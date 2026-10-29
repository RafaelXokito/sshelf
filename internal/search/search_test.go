package search

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

var hosts = []domain.Host{
	{Label: "db-staging", Hostname: "10.0.1.20", User: "admin"},
	{Label: "web-prod", Hostname: "10.0.0.10", User: "ubuntu"},
	{Label: "Web-Prod-EU", Hostname: "eu.example.com", User: "deploy"},
	{Label: "zeta", Hostname: "zeta.internal"},
}

func labels(idx []int) []string {
	out := make([]string, len(idx))
	for i, j := range idx {
		out[i] = hosts[j].Label
	}
	return out
}

func TestFR_NAV_002_Matches(t *testing.T) {
	for _, tc := range []struct {
		query string
		first string
		want  []string // every match, any order; nil = only check first
	}{
		{"wbprd", "web-prod", []string{"web-prod", "Web-Prod-EU"}},
		{"WBPRD", "web-prod", []string{"web-prod", "Web-Prod-EU"}}, // case-insensitive
		{"10.0.1", "db-staging", nil},
		{"deploy", "Web-Prod-EU", []string{"Web-Prod-EU"}}, // user
		{"internal", "zeta", []string{"zeta"}},             // hostname
		{"xyzzy", "", []string{}},
	} {
		got := labels(Filter(hosts, tc.query))
		if tc.first != "" && (len(got) == 0 || got[0] != tc.first) {
			t.Errorf("%q: top match %v, want %q first", tc.query, got, tc.first)
		}
		if tc.want != nil {
			g, w := slices.Clone(got), slices.Clone(tc.want)
			slices.Sort(g)
			slices.Sort(w)
			if !slices.Equal(g, w) {
				t.Errorf("%q: matches %v, want %v", tc.query, got, tc.want)
			}
		}
	}
	if got := Filter(hosts, "  "); !slices.Equal(got, []int{0, 1, 2, 3}) {
		t.Errorf("blank query: %v", got)
	}
}

// fixture5k is a generated 5,000-host fixture.
func fixture5k() []domain.Host {
	r := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // a deterministic fixture
	words := []string{"web", "db", "cache", "api", "prod", "staging", "eu", "us", "west", "east", "worker", "queue", "edge", "auth"}
	users := []string{"ubuntu", "admin", "deploy", "root", "ec2-user", ""}
	out := make([]domain.Host, 5000)
	for i := range out {
		out[i] = domain.Host{
			Label:    fmt.Sprintf("%s-%s-%s-%04d", words[r.IntN(len(words))], words[r.IntN(len(words))], words[r.IntN(len(words))], i),
			Hostname: fmt.Sprintf("10.%d.%d.%d", r.IntN(256), r.IntN(256), r.IntN(256)),
			User:     users[r.IntN(len(users))],
		}
	}
	return out
}

var queries = []string{"w", "wb", "wbp", "wbpr", "wbprd", "10.0.1", "dpl", "api-eu"}

func BenchmarkNFR_PERF_002_Filter5k(b *testing.B) {
	hs := fixture5k()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		Filter(hs, queries[i%len(queries)])
	}
}

// TestNFR_PERF_002_Filter5k fails when one filter of 5,000 hosts takes
// more than 16 ms (one 60 Hz frame).
func TestNFR_PERF_002_Filter5k(t *testing.T) {
	if testing.Short() {
		t.Skip("benchmark")
	}
	r := testing.Benchmark(BenchmarkNFR_PERF_002_Filter5k)
	if ns := r.NsPerOp(); ns > 16_000_000 {
		t.Fatalf("Filter over 5,000 hosts: %d ns/op, want at most 16,000,000", ns)
	} else {
		t.Logf("Filter over 5,000 hosts: %d ns/op", ns)
	}
}
