package mediabusy

import (
	"testing"

	"github.com/google/uuid"
)

func TestHoldReleaseRefcount(t *testing.T) {
	s := New()
	id := uuid.New()
	a := s.Hold(id)
	b := s.Hold(id)
	if !s.Contains(id) || len(s.IDs()) != 1 {
		t.Fatal("held track missing")
	}
	a()
	if !s.Contains(id) {
		t.Fatal("second holder still live")
	}
	b()
	if s.Contains(id) || len(s.IDs()) != 0 {
		t.Fatal("should be empty after both releases")
	}
	b() // idempotent
}

func TestNilSet(t *testing.T) {
	var s *Set
	rel := s.Hold(uuid.New())
	rel()
	if s.Contains(uuid.New()) || len(s.IDs()) != 0 {
		t.Fatal("nil set")
	}
}
