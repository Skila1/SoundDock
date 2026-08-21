package httpapi

import (
	"testing"

	"github.com/google/uuid"
)

func TestJSONCellUUID(t *testing.T) {
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	if jsonCell(id) != id.String() {
		t.Fatal("uuid.UUID")
	}
	if jsonCell([16]byte(id)) != id.String() {
		t.Fatal("[16]byte")
	}
	if jsonCell([]byte(id[:])) != id.String() {
		t.Fatal("[]byte")
	}
}
