package httpapi

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/playback"
)

func TestHandleLivePayloadIgnoresLibraryScope(t *testing.T) {
	s := &Server{}
	s.handleLivePayload(nil, `{"t":"resource.invalidate","scope":"library","ids":["00000000-0000-4000-8000-000000000050"]}`)
}

func TestLibraryInvalidateDoesNotLeakHiddenIDs(t *testing.T) {
	s := &Server{}
	h := s.sessionHub()
	sid := uuid.New()
	sub := h.subscribe(sid)
	leaked := "00000000-0000-4000-8000-0000000000bb"
	s.handleLivePayload(nil, `{"t":"resource.invalidate","scope":"library","ids":["`+leaked+`"]}`)
	select {
	case ev := <-sub.ch:
		if strings.Contains(string(ev.data), leaked) {
			t.Fatalf("cross-library SSE IDOR: %s", ev.data)
		}
	default:
	}
}

func TestFilterInvalidateIDsDropsOversized(t *testing.T) {
	ids := make([]string, 40)
	for i := range ids {
		ids[i] = "00000000-0000-4000-8000-0000000000aa"
	}
	if got := filterInvalidateIDs(playback.Signal{IDs: ids}); got != nil {
		t.Fatal("oversized id lists must be stripped")
	}
}

func TestHandleLivePayloadUserInvalidate(t *testing.T) {
	s := &Server{}
	s.handleLivePayload(nil, `{"t":"resource.invalidate","scope":"user","actor":"00000000-0000-4000-8000-000000000001","keys":["playlists","playlist","unmatched","me-providers"]}`)
	s.handleLivePayload(nil, `{"t":"job.progress","scope":"user","actor":"00000000-0000-4000-8000-000000000001","rid":"00000000-0000-4000-8000-000000000002","rev":40}`)
}

func TestEncodeLiveSignalHasNoQueue(t *testing.T) {
	b, err := playback.EncodeSignal(playback.Signal{T: "session.state", SID: "s", Rev: 1, Scope: "session"})
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > 2048 {
		t.Fatalf("%d", len(b))
	}
}
