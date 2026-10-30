package domain

import "testing"

func TestFR_HOST_004_CopyLabel(t *testing.T) {
	hosts := func(labels ...string) []Host {
		var out []Host
		for _, l := range labels {
			out = append(out, Host{Label: l})
		}
		return out
	}
	for _, tc := range []struct {
		label  string
		hosts  []Host
		want   string
		reason string
	}{
		{"web-prod", hosts("web-prod"), "web-prod Copy", "free"},
		{"web-prod", hosts("web-prod", "WEB-PROD copy"), "web-prod Copy 2", "taken in another case"},
		{"web-prod", hosts("web-prod", "web-prod Copy", "web-prod copy 2"), "web-prod Copy 3", "Copy 2 taken"},
		{"web-prod", hosts("web-prod", "web-prod Copy 2"), "web-prod Copy", "Copy free, Copy 2 taken"},
		{"Web Prod EU", hosts("Web Prod EU", "web prod eu copy"), "Web Prod EU Copy 2", "label with spaces (D-105)"},
		{"x Copy", hosts("x Copy"), "x Copy Copy", "a copy of a copy"},
		{"web", nil, "web Copy", "no hosts"},
	} {
		if got := CopyLabel(tc.label, tc.hosts); got != tc.want {
			t.Errorf("%s: CopyLabel(%q) = %q, want %q", tc.reason, tc.label, got, tc.want)
		}
	}
}
