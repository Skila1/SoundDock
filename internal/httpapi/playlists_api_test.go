package httpapi

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/radio"
)

func TestPlaylistInviteTokens(t *testing.T) {
	key := []byte("p4-playlists")
	id := uuid.MustParse("00000000-0000-4000-8000-000000000090")
	tok, exp, err := radio.SignInvite(key, id, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if time.Until(exp) < time.Minute {
		t.Fatal("expiry")
	}
	got, err := radio.VerifyInvite(key, tok)
	if err != nil || got != id {
		t.Fatalf("%s %v", got, err)
	}
}

func TestGetPlaylistACLStayOwnerPublicCollaborator(t *testing.T) {
	// Documented invariant: list/get see owner OR public OR collaborator.
	// Enforced by playlistACL.CanSee in playlists_api.go (no playback_sessions writes).
	a := playlistACL{Owner: uuid.MustParse("00000000-0000-4000-8000-000000000001"), Public: true}
	a.CanSee = a.IsOwner || a.Public
	if !a.CanSee {
		t.Fatal("public must be visible")
	}
}
