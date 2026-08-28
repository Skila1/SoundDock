package mediabusy

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestNewHolderDistinct(t *testing.T) {
	a := NewHolder(KindHTTPStream)
	b := NewHolder(KindHTTPStream)
	if a == b || !strings.HasPrefix(a, "http:") {
		t.Fatalf("%s %s", a, b)
	}
	if !strings.HasPrefix(NewHolder(KindHMACStream), "hmac:") {
		t.Fatal("hmac")
	}
}

func TestTwoHTTPHoldsAreDistinctHolders(t *testing.T) {
	a := NewHolder(KindHTTPStream)
	b := NewHolder(KindHTTPStream)
	if a == b {
		t.Fatal("two HTTP ranges must not share holder_id")
	}
	d := NewHolder(KindDiscord)
	if !strings.HasPrefix(d, "discord:") {
		t.Fatalf("%s", d)
	}
}

func TestAcquireWithoutPoolIsLocalOnly(t *testing.T) {
	s := New()
	id := uuid.New()
	rel := s.Acquire(nil, id, KindHTTPStream, NewHolder(KindHTTPStream))
	if !s.Contains(id) {
		t.Fatal("local hold")
	}
	rel()
	if s.Contains(id) {
		t.Fatal("released")
	}
}
