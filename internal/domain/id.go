package domain

import "github.com/google/uuid"

// ID identifies an entity. It is a lowercase UUIDv7 string (ADR-008) and
// never changes, so entities reference each other only by ID.
type ID string

// NewID returns a new UUIDv7 ID.
func NewID() ID {
	return ID(uuid.Must(uuid.NewV7()).String())
}
