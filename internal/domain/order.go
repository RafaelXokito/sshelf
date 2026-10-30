package domain

import (
	"cmp"
	"slices"
	"strings"
)

// CompareLastUse orders hosts for the host list (FR-NAV-018, D-016): the
// most recently connected first, then the hosts never connected. Equal
// times, and the hosts never connected, are A–Z by label,
// case-insensitively.
func CompareLastUse(a, b Host) int {
	if c := CompareRecency(a, b); c != 0 {
		return c
	}
	return cmp.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label))
}

// CompareRecency orders a before b when a was connected more recently; a
// host never connected comes after every host that was. It returns 0 for
// equal times and for two hosts never connected.
func CompareRecency(a, b Host) int {
	switch {
	case a.LastConnectedAt == nil && b.LastConnectedAt == nil:
		return 0
	case a.LastConnectedAt == nil:
		return 1
	case b.LastConnectedAt == nil:
		return -1
	}
	return b.LastConnectedAt.Compare(*a.LastConnectedAt)
}

// SortByLastUse returns a copy of hosts in CompareLastUse order. Hosts
// that compare equal keep their order.
func SortByLastUse(hosts []Host) []Host {
	hosts = slices.Clone(hosts)
	slices.SortStableFunc(hosts, CompareLastUse)
	return hosts
}
