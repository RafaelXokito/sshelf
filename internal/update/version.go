package update

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a release version, vMAJOR.MINOR.PATCH.
type Version struct{ Major, Minor, Patch int }

// Parse reads a release version: "v" and three dot-separated decimal
// numbers without leading zeros, nothing else (SEC-UPD-001). A
// pre-release or build suffix ("v1.2.3-rc1", "v0.0.0-e2e", a Go
// pseudo-version, "v1.2.3-4-gabc" from git describe) is not a release, so
// it does not parse: such builds count as development builds (D-110).
func Parse(s string) (Version, bool) {
	rest, ok := strings.CutPrefix(s, "v")
	if !ok || len(rest) > 32 {
		return Version{}, false
	}
	parts := strings.Split(rest, ".")
	if len(parts) != 3 {
		return Version{}, false
	}
	var n [3]int
	for i, p := range parts {
		if p == "" || len(p) > 9 || (len(p) > 1 && p[0] == '0') {
			return Version{}, false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return Version{}, false
			}
		}
		n[i], _ = strconv.Atoi(p) // digits only, at most 9: cannot fail
	}
	return Version{n[0], n[1], n[2]}, true
}

// String returns "vMAJOR.MINOR.PATCH". It is built from the numbers, so it
// never holds anything that came over the network as text.
func (v Version) String() string { return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Less reports whether v is older than w.
func (v Version) Less(w Version) bool {
	if v.Major != w.Major {
		return v.Major < w.Major
	}
	if v.Minor != w.Minor {
		return v.Minor < w.Minor
	}
	return v.Patch < w.Patch
}
