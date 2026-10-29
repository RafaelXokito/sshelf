package domain

import (
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxLabelLen is the longest host label, in characters (FR-HOST-003).
const MaxLabelLen = 64

// Field names a form field that failed validation.
type Field string

// The host fields ValidateHost checks.
const (
	FieldLabel    Field = "label"
	FieldHostname Field = "hostname"
	FieldPort     Field = "port"
	FieldIdentity Field = "identity"
)

// The messages shown next to an invalid field (FR-HOST-003).
const (
	MsgLabelRequired    = "Label is required"
	MsgLabelTooLong     = "Label is longer than 64 characters"
	MsgLabelInUse       = "Label already in use"
	MsgHostnameRequired = "Hostname is required"
	MsgHostnameSpace    = "Hostname cannot contain spaces"
	MsgPortRange        = "Port must be 1 to 65535"
	MsgIdentityMissing  = "Identity not found"
)

// ValidationError lists the invalid fields of an entity, each with the
// message to show next to it.
type ValidationError struct {
	Fields map[Field]string
}

func (e *ValidationError) Error() string {
	names := make([]string, 0, len(e.Fields))
	for f := range e.Fields {
		names = append(names, string(f))
	}
	slices.Sort(names)
	for i, n := range names {
		names[i] = n + ": " + e.Fields[Field(n)]
	}
	return "invalid " + strings.Join(names, "; ")
}

// ValidateHost checks h against FR-HOST-003, using d for the other hosts
// (a host with h's ID is h itself) and the identities. Labels may contain
// spaces (D-105) and are unique case-insensitively. Port 0 means "inherit"
// and is valid here; ParsePort rejects a typed 0. It returns nil or a
// *ValidationError.
func ValidateHost(h Host, d Data) error {
	errs := map[Field]string{}
	label := strings.TrimSpace(h.Label)
	switch {
	case label == "":
		errs[FieldLabel] = MsgLabelRequired
	case utf8.RuneCountInString(label) > MaxLabelLen:
		errs[FieldLabel] = MsgLabelTooLong
	case slices.ContainsFunc(d.Hosts, func(o Host) bool {
		return o.ID != h.ID && strings.EqualFold(strings.TrimSpace(o.Label), label)
	}):
		errs[FieldLabel] = MsgLabelInUse
	}
	switch {
	case h.Hostname == "":
		errs[FieldHostname] = MsgHostnameRequired
	case strings.ContainsFunc(h.Hostname, unicode.IsSpace):
		errs[FieldHostname] = MsgHostnameSpace
	}
	if h.Port < 0 || h.Port > 65535 {
		errs[FieldPort] = MsgPortRange
	}
	if h.IdentityID != "" && !slices.ContainsFunc(d.Identities, func(i Identity) bool { return i.ID == h.IdentityID }) {
		errs[FieldIdentity] = MsgIdentityMissing
	}
	if len(errs) > 0 {
		return &ValidationError{Fields: errs}
	}
	return nil
}

// ParsePort parses a port typed in a form. An empty string is 0 (the
// default, 22); anything else must be a decimal number from 1 to 65535.
// The error's text is MsgPortRange.
func ParsePort(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	p, err := strconv.Atoi(s)
	if err != nil || p < 1 || p > 65535 || strings.HasPrefix(s, "+") {
		return 0, &ValidationError{Fields: map[Field]string{FieldPort: MsgPortRange}}
	}
	return p, nil
}

// The identity fields ValidateIdentity checks.
const FieldName Field = "name"

// The messages shown next to an invalid identity field (FR-IDENT-001).
const (
	MsgNameRequired = "Name is required"
	MsgNameTooLong  = "Name is longer than 64 characters"
	MsgNameInUse    = "Name already in use"
)

// ValidateIdentity checks i against FR-IDENT-001, using d for the other
// identities (one with i's ID is i itself). Names are required, at most
// MaxLabelLen characters, and unique case-insensitively among the
// identities the user sees: hidden per-host identities (FR-HOST-010) are
// named after their host and neither checked nor checked against. It
// returns nil or a *ValidationError.
func ValidateIdentity(i Identity, d Data) error {
	if i.Hidden {
		return nil
	}
	name := strings.TrimSpace(i.Label)
	var msg string
	switch {
	case name == "":
		msg = MsgNameRequired
	case utf8.RuneCountInString(name) > MaxLabelLen:
		msg = MsgNameTooLong
	case slices.ContainsFunc(d.Identities, func(o Identity) bool {
		return o.ID != i.ID && !o.Hidden && strings.EqualFold(strings.TrimSpace(o.Label), name)
	}):
		msg = MsgNameInUse
	}
	if msg != "" {
		return &ValidationError{Fields: map[Field]string{FieldName: msg}}
	}
	return nil
}
