package domain

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestARCH_DATA_001_NewIDIsUUIDv7(t *testing.T) {
	seen := make(map[ID]bool)
	for range 1000 {
		id := NewID()
		u, err := uuid.Parse(string(id))
		if err != nil {
			t.Fatalf("NewID() = %q: %v", id, err)
		}
		if u.Version() != 7 {
			t.Fatalf("NewID() = %q: version %d, want 7", id, u.Version())
		}
		if u.Variant() != uuid.RFC4122 {
			t.Fatalf("NewID() = %q: variant %v, want RFC4122", id, u.Variant())
		}
		if s := string(id); s != strings.ToLower(s) || len(s) != 36 {
			t.Fatalf("NewID() = %q: want a lowercase 36-character string", id)
		}
		if seen[id] {
			t.Fatalf("NewID() returned %q twice", id)
		}
		seen[id] = true
	}
}
