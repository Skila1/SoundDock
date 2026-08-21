package httpapi

import (
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
)

func TestCanEditMeta(t *testing.T) {
	if canEditMeta(nil) {
		t.Fatal("nil user")
	}
	if canEditMeta(&auth.User{ID: uuid.New()}) {
		t.Fatal("plain user")
	}
	if !canEditMeta(&auth.User{IsAdmin: true}) {
		t.Fatal("admin")
	}
	if !canEditMeta(&auth.User{Permissions: []string{"admin"}}) {
		t.Fatal("admin perm")
	}
}
