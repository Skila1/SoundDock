package rbac

import (
	"testing"

	"github.com/sounddock/sounddock/internal/auth"
)

func TestHasPermAdmin(t *testing.T) {
	u := &auth.User{IsAdmin: true}
	if !auth.HasPerm(u, "library.import_url") {
		t.Fatal("admin")
	}
	u = &auth.User{Permissions: []string{"tracks.stream"}}
	if auth.HasPerm(u, "admin") {
		t.Fatal("user is not admin")
	}
	if !auth.HasPerm(u, "tracks.stream") {
		t.Fatal("stream")
	}
}
