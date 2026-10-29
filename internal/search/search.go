package search

import (
	"math"
	"slices"
	"strings"

	"github.com/sahilm/fuzzy"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

// field picks one searchable field of a host.
type field func(h *domain.Host) string

// fields are the fields FR-NAV-002 matches in the MVP. Group, tags and
// notes come in v1.
var fields = []field{
	func(h *domain.Host) string { return h.Label },
	func(h *domain.Host) string { return h.Hostname },
	func(h *domain.Host) string { return h.User },
}

// source adapts one field of every host to fuzzy.Source.
type source struct {
	hosts []domain.Host
	get   field
}

func (s source) String(i int) string { return s.get(&s.hosts[i]) }
func (s source) Len() int            { return len(s.hosts) }

// Filter returns the indexes of the hosts whose label, hostname or user
// fuzzy-matches query, case-insensitively, best match first (FR-NAV-002).
// A host scores its best field. Equal scores keep the hosts' order. An
// empty or blank query returns every index in order.
func Filter(hosts []domain.Host, query string) []int {
	query = strings.TrimSpace(query)
	out := make([]int, 0, len(hosts))
	if query == "" {
		for i := range hosts {
			out = append(out, i)
		}
		return out
	}
	best := make([]int, len(hosts))
	for i := range best {
		best[i] = math.MinInt
	}
	for _, f := range fields {
		for _, m := range fuzzy.FindFromNoSort(query, source{hosts: hosts, get: f}) {
			best[m.Index] = max(best[m.Index], m.Score)
		}
	}
	for i, s := range best {
		if s != math.MinInt {
			out = append(out, i)
		}
	}
	slices.SortStableFunc(out, func(a, b int) int { return best[b] - best[a] })
	return out
}
