package secrets

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Operation names for MemStore.FailNext and MemStore.Calls.
const (
	OpGet       = "get"
	OpSet       = "set"
	OpDelete    = "delete"
	OpAvailable = "available"
)

// MemStore is an in-memory Store for tests. It can inject failures and counts
// calls. It is safe for concurrent use.
type MemStore struct {
	mu    sync.Mutex
	items map[Ref][]byte
	fail  map[string][]error
	calls map[string]int
}

var _ Store = (*MemStore)(nil)

// NewMemStore returns an empty MemStore.
func NewMemStore() *MemStore {
	return &MemStore{
		items: map[Ref][]byte{},
		fail:  map[string][]error{},
		calls: map[string]int{},
	}
}

func checkOp(op string) {
	switch op {
	case OpGet, OpSet, OpDelete, OpAvailable:
	default:
		panic(fmt.Sprintf("secrets: unknown MemStore op %q", op))
	}
}

// FailNext makes the next call of op ("get", "set", "delete" or
// "available") return err without doing anything. Calls queue up: each
// FailNext fails one more call.
func (m *MemStore) FailNext(op string, err error) {
	checkOp(op)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail[op] = append(m.fail[op], err)
}

// Calls returns how many times op has been called, failed calls included.
func (m *MemStore) Calls(op string) int {
	checkOp(op)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[op]
}

// Keys returns the refs currently stored, sorted by account.
func (m *MemStore) Keys() []Ref {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]Ref, 0, len(m.items))
	for r := range m.items {
		keys = append(keys, r)
	}
	slices.SortFunc(keys, func(a, b Ref) int { return strings.Compare(a.Account(), b.Account()) })
	return keys
}

// begin counts a call of op and returns the injected or context error, if
// any. m.mu must be held.
func (m *MemStore) begin(ctx context.Context, op string) error {
	m.calls[op]++
	if q := m.fail[op]; len(q) > 0 {
		m.fail[op] = q[1:]
		return q[0]
	}
	return ctx.Err()
}

// Get implements Store. It returns a fresh copy that the caller owns.
func (m *MemStore) Get(ctx context.Context, ref Ref) (Secret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.begin(ctx, OpGet); err != nil {
		return Secret{}, err
	}
	if err := ref.validate(); err != nil {
		return Secret{}, err
	}
	b, ok := m.items[ref]
	if !ok {
		return Secret{}, ErrNotFound
	}
	return NewSecret(bytes.Clone(b)), nil
}

// Set implements Store. It stores a copy of s.
func (m *MemStore) Set(ctx context.Context, ref Ref, s Secret) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.begin(ctx, OpSet); err != nil {
		return err
	}
	if err := ref.validate(); err != nil {
		return err
	}
	if s.Len() > MaxSecretSize {
		return ErrTooLarge
	}
	if old, ok := m.items[ref]; ok {
		clear(old)
	}
	m.items[ref] = bytes.Clone(s.Bytes())
	return nil
}

// Delete implements Store.
func (m *MemStore) Delete(ctx context.Context, ref Ref) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.begin(ctx, OpDelete); err != nil {
		return err
	}
	if err := ref.validate(); err != nil {
		return err
	}
	if old, ok := m.items[ref]; ok {
		clear(old)
		delete(m.items, ref)
	}
	return nil
}

// Available implements Store.
func (m *MemStore) Available(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.begin(ctx, OpAvailable)
}
