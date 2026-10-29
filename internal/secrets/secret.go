package secrets

import (
	"fmt"
	"io"
	"log/slog"
)

const redacted = "[REDACTED]"

// Secret holds secret bytes (SEC-MEM-001). It never prints its contents
// (SEC-LOG-002). Copies of a Secret share one buffer, so Destroy on any copy
// destroys them all. The bytes sit behind two pointers so that fmt's
// reflection on an unexported Secret field (which cannot call the methods
// below) shows only an address, even for verbs that fmt reports as bad
// (%!s(...)) and prints one level deeper.
//
// The zero Secret is empty.
type Secret struct {
	h *holder
}

type holder struct {
	b *[]byte
}

// NewSecret wraps b and takes ownership of it: the caller must not use b
// afterwards, and Destroy zeroes it.
func NewSecret(b []byte) Secret {
	return Secret{h: &holder{b: &b}}
}

// Bytes returns the secret. The slice is borrowed and valid until Destroy;
// callers must not keep or modify it.
func (s Secret) Bytes() []byte {
	if s.h == nil {
		return nil
	}
	return *s.h.b
}

// Len returns the secret's length in bytes.
func (s Secret) Len() int {
	return len(s.Bytes())
}

// Destroy zeroes the backing array and empties s and every copy of it.
func (s *Secret) Destroy() {
	if s.h == nil {
		return
	}
	clear(*s.h.b)
	*s.h.b = nil
}

// String implements fmt.Stringer.
func (Secret) String() string { return redacted }

// GoString implements fmt.GoStringer.
func (Secret) GoString() string { return redacted }

// Format implements fmt.Formatter, so every verb (%s %v %+v %#v %q %x ...)
// prints [REDACTED].
func (Secret) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, redacted)
}

// MarshalJSON implements json.Marshaler.
func (Secret) MarshalJSON() ([]byte, error) {
	return []byte(`"` + redacted + `"`), nil
}

// MarshalText implements encoding.TextMarshaler, which covers map keys and
// text-based encoders.
func (Secret) MarshalText() ([]byte, error) {
	return []byte(redacted), nil
}

// LogValue implements slog.LogValuer.
func (Secret) LogValue() slog.Value {
	return slog.StringValue(redacted)
}
