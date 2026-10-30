package domain

import (
	"slices"
	"strconv"
	"strings"
)

// CopyLabel returns the label of a duplicate of the host labelled label
// (FR-HOST-004, D-022): "<label> Copy", or "<label> Copy <n>" with the
// smallest n from 2 that no host in hosts uses, compared
// case-insensitively as FR-HOST-003 does. The label is not shortened; a
// result over MaxLabelLen fails ValidateHost when saved.
func CopyLabel(label string, hosts []Host) string {
	base := strings.TrimSpace(label) + " Copy"
	taken := func(l string) bool {
		return slices.ContainsFunc(hosts, func(h Host) bool { return strings.EqualFold(strings.TrimSpace(h.Label), l) })
	}
	l := base
	for n := 2; taken(l); n++ {
		l = base + " " + strconv.Itoa(n)
	}
	return l
}
