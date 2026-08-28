package mediabusy

import (
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Set is an in-process refcount of tracks that currently have an open HTTP
// stream handler or Discord decoder. Retention uses this; HMAC tokens and
// AcquireStreamSlot are not leases.
type Set struct {
	mu       sync.Mutex
	n        map[uuid.UUID]int
	pool     *pgxpool.Pool
	instance string
}

func New() *Set {
	return &Set{n: map[uuid.UUID]int{}}
}

// Hold increments the refcount for id and returns a release func. Release is
// idempotent and safe after panics via defer.
func (s *Set) Hold(id uuid.UUID) func() {
	if s == nil || id == uuid.Nil {
		return func() {}
	}
	s.mu.Lock()
	if s.n == nil {
		s.n = map[uuid.UUID]int{}
	}
	s.n[id]++
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() { s.release(id) })
	}
}

func (s *Set) release(id uuid.UUID) {
	if s == nil || id == uuid.Nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.n[id] - 1
	if n <= 0 {
		delete(s.n, id)
		return
	}
	s.n[id] = n
}

func (s *Set) IDs() []uuid.UUID {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.n) == 0 {
		return nil
	}
	out := make([]uuid.UUID, 0, len(s.n))
	for id, n := range s.n {
		if n > 0 {
			out = append(out, id)
		}
	}
	return out
}

func (s *Set) Contains(id uuid.UUID) bool {
	if s == nil || id == uuid.Nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n[id] > 0
}
