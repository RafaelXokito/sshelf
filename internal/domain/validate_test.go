package domain

import (
	"errors"
	"strings"
	"testing"
)

func fieldErr(t *testing.T, err error, f Field) string {
	t.Helper()
	if err == nil {
		return ""
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error %v is not a *ValidationError", err)
	}
	return ve.Fields[f]
}

var validData = Data{
	Hosts:      []Host{{Meta: Meta{ID: "h1"}, Label: "web-prod", Hostname: "10.0.0.10"}},
	Identities: []Identity{{Meta: Meta{ID: "i1"}, Label: "deploy"}},
}

func TestFR_HOST_003_LabelRules(t *testing.T) {
	for _, tc := range []struct {
		id, label, want string
	}{
		{"", "Web Prod EU", ""}, // spaces allowed (D-105)
		{"", "Web-Prod", MsgLabelInUse},
		{"", "  WEB-PROD ", MsgLabelInUse},
		{"h1", "Web-Prod", ""}, // the host itself
		{"", "", MsgLabelRequired},
		{"", "   ", MsgLabelRequired},
		{"", strings.Repeat("é", MaxLabelLen), ""},
		{"", strings.Repeat("a", MaxLabelLen+1), MsgLabelTooLong},
	} {
		err := ValidateHost(Host{Meta: Meta{ID: ID(tc.id)}, Label: tc.label, Hostname: "h"}, validData)
		if got := fieldErr(t, err, FieldLabel); got != tc.want {
			t.Errorf("label %q (id %q): %q, want %q", tc.label, tc.id, got, tc.want)
		}
	}
}

func TestFR_HOST_003_PortRules(t *testing.T) {
	for _, tc := range []struct {
		in   string
		port int
		ok   bool
	}{
		{"0", 0, false}, {"65536", 0, false}, {"abc", 0, false}, {"-1", 0, false}, {"+22", 0, false},
		{"65535", 65535, true}, {"1", 1, true}, {"2222", 2222, true}, {" 22 ", 22, true}, {"", 0, true},
	} {
		p, err := ParsePort(tc.in)
		if (err == nil) != tc.ok || p != tc.port {
			t.Errorf("ParsePort(%q) = %d, %v", tc.in, p, err)
		}
		if err != nil && fieldErr(t, err, FieldPort) != MsgPortRange {
			t.Errorf("ParsePort(%q) message %v", tc.in, err)
		}
	}
	for _, p := range []int{-1, 65536} {
		if fieldErr(t, ValidateHost(Host{Label: "x", Hostname: "h", Port: p}, Data{}), FieldPort) != MsgPortRange {
			t.Errorf("port %d accepted", p)
		}
	}
	if err := ValidateHost(Host{Label: "x", Hostname: "h", Port: 65535}, Data{}); err != nil {
		t.Errorf("port 65535: %v", err)
	}
}

func TestFR_HOST_003_HostnameRules(t *testing.T) {
	for _, tc := range []struct{ hostname, want string }{
		{"my host", MsgHostnameSpace},
		{"host\t", MsgHostnameSpace},
		{"", MsgHostnameRequired},
		{"2001:db8::1", ""},
		{"10.0.0.1", ""},
		{"web.example.com", ""},
	} {
		err := ValidateHost(Host{Label: "x", Hostname: tc.hostname}, Data{})
		if got := fieldErr(t, err, FieldHostname); got != tc.want {
			t.Errorf("hostname %q: %q, want %q", tc.hostname, got, tc.want)
		}
	}
}

func TestValidateHostIdentityMustExist(t *testing.T) {
	if err := ValidateHost(Host{Label: "x", Hostname: "h", IdentityID: "i1"}, validData); err != nil {
		t.Fatal(err)
	}
	err := ValidateHost(Host{Label: "x", Hostname: "h", IdentityID: "gone"}, validData)
	if fieldErr(t, err, FieldIdentity) != MsgIdentityMissing {
		t.Fatalf("missing identity: %v", err)
	}
	if !strings.Contains(err.Error(), "identity: Identity not found") {
		t.Fatalf("Error() = %q", err)
	}
}
