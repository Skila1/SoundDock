package httpapi

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestLegacyDiscordLinkRoutesRemoved(t *testing.T) {
	h := (&Server{}).Router()
	r, ok := h.(*chi.Mux)
	if !ok {
		t.Fatalf("router type %T", h)
	}
	got := map[string]bool{}
	if err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[method+" "+route] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got["POST /api/v1/link/discord"] {
		t.Fatal("legacy POST /link/discord must be removed")
	}
	if got["POST /api/v1/me/identities/discord"] {
		t.Fatal("legacy POST /me/identities/discord must be removed")
	}
	if !got["POST /api/v1/me/discord/link"] {
		t.Fatal("live POST /me/discord/link must remain")
	}
}
