package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/stream"
)

func TestClassifyMediaState(t *testing.T) {
	intent := uuid.MustParse("00000000-0000-4000-8000-0000000000aa")
	cases := []struct {
		name  string
		probe mediaProbe
		state string
		hasID bool
	}{
		{name: "original file ready", probe: mediaProbe{Found: true, HasOriginal: true, Acquisition: "youtube"}, state: MediaStateReady},
		{name: "youtube stub restoring", probe: mediaProbe{Found: true, Acquisition: "youtube"}, state: MediaStateRestoring},
		{name: "scapex stub restoring", probe: mediaProbe{Found: true, Acquisition: "scapex"}, state: MediaStateRestoring},
		{name: "open intent restoring", probe: mediaProbe{Found: true, Acquisition: "local", OpenIntent: true, IntentID: &intent}, state: MediaStateRestoring, hasID: true},
		{name: "nas missing external", probe: mediaProbe{Found: true, Acquisition: ""}, state: MediaStateMissingExternal},
		{name: "local missing external", probe: mediaProbe{Found: true, Acquisition: "nas"}, state: MediaStateMissingExternal},
		{name: "intent omitted without id", probe: mediaProbe{Found: true, Acquisition: "youtube", OpenIntent: true}, state: MediaStateRestoring},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyMediaState(tc.probe)
			if got.State != tc.state {
				t.Fatalf("state %q want %q", got.State, tc.state)
			}
			if tc.hasID {
				if got.IntentID == nil || *got.IntentID != intent {
					t.Fatalf("intent %v", got.IntentID)
				}
			} else if got.IntentID != nil {
				t.Fatalf("unexpected intent %v", got.IntentID)
			}
		})
	}
}

func TestStreamMissingCodes(t *testing.T) {
	status, code, _ := streamMissingCodes("youtube")
	if status != http.StatusConflict || code != streamCodeUnavailable {
		t.Fatalf("youtube %d %s", status, code)
	}
	status, code, _ = streamMissingCodes("scapex")
	if status != http.StatusConflict || code != streamCodeUnavailable {
		t.Fatalf("scapex %d %s", status, code)
	}
	status, code, _ = streamMissingCodes("")
	if status != http.StatusNotFound || code != streamCodeUnavailableExternal {
		t.Fatalf("empty %d %s", status, code)
	}
	status, code, _ = streamMissingCodes("local")
	if status != http.StatusNotFound || code != streamCodeUnavailableExternal {
		t.Fatalf("local %d %s", status, code)
	}
}

func TestApplyMediaStateToQueue(t *testing.T) {
	tid := uuid.MustParse("00000000-0000-4000-8000-000000000050")
	iid := uuid.MustParse("00000000-0000-4000-8000-0000000000bb")
	q := map[string]any{
		"current_track_id": tid,
		"items": []map[string]any{
			{"id": uuid.New(), "position": 0, "track_id": tid, "origin": "user"},
		},
	}
	applyMediaStateToQueue(q, map[uuid.UUID]Playability{
		tid: {State: MediaStateRestoring, IntentID: &iid},
	})
	items, _ := q["items"].([]map[string]any)
	if items[0]["media_state"] != MediaStateRestoring {
		t.Fatalf("item media_state %v", items[0]["media_state"])
	}
	if items[0]["intent_id"] != iid {
		t.Fatalf("item intent %v", items[0]["intent_id"])
	}
	if q["current_media_state"] != MediaStateRestoring {
		t.Fatalf("current %v", q["current_media_state"])
	}
	if q["current_intent_id"] != iid {
		t.Fatalf("current intent %v", q["current_intent_id"])
	}

	missingID := uuid.MustParse("00000000-0000-4000-8000-000000000051")
	q2 := map[string]any{
		"current_track_id": missingID,
		"items":            []map[string]any{{"track_id": missingID}},
	}
	applyMediaStateToQueue(q2, map[uuid.UUID]Playability{})
	items2, _ := q2["items"].([]map[string]any)
	if items2[0]["media_state"] != MediaStateMissingExternal {
		t.Fatalf("missing item %v", items2[0]["media_state"])
	}
	if _, ok := items2[0]["intent_id"]; ok {
		t.Fatal("intent_id should be omitted")
	}
}

func TestPlayabilityJSONOmitsIntent(t *testing.T) {
	b, err := json.Marshal(Playability{State: MediaStateReady})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "intent_id") {
		t.Fatalf("should omit intent_id: %s", b)
	}
	id := uuid.New()
	b, err = json.Marshal(Playability{State: MediaStateRestoring, IntentID: &id})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), id.String()) {
		t.Fatalf("want intent: %s", b)
	}
}

func TestGetTrackPlayabilityNoDB(t *testing.T) {
	s := &Server{}
	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/"+id.String()+"/playability", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	s.getTrackPlayability(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestResolvePlayTracksYouTubeDoesNotUseBlockingResolve(t *testing.T) {
	s := &Server{}
	start := time.Now()
	_, err := s.resolvePlayTracks(context.Background(), []string{"https://www.youtube.com/watch?v=dQw4w9wgXcQ"})
	if time.Since(start) > time.Second {
		t.Fatal("blocked")
	}
	if err == nil {
		t.Fatal("expected error without database")
	}
	if errors.Is(err, errScapeXDown) || err.Error() == string(errScapeXDown) {
		t.Fatalf("called blocking resolveQueueTracks: %v", err)
	}
}

func TestResolvePlayTracksLibraryUUIDs(t *testing.T) {
	s := &Server{}
	id := uuid.MustParse("00000000-0000-4000-8000-000000000021")
	got, err := s.resolvePlayTracks(context.Background(), []string{id.String()})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != id {
		t.Fatalf("got %v", got)
	}
}

func TestStreamUnavailableHTTPCodes(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Server{Pool: pool, SignKey: []byte("test-sign-key-32-bytes-long!!!!")}

	sid, libID := uuid.New(), uuid.New()
	ytID, nasID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc)
		VALUES ($1, $2, 'local', $3)`, sid, "w6-"+sid.String()[:8], []byte(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id, root_prefix, read_only)
		VALUES ($1, $2, 'music', $3, '', false)`, libID, "w6-"+libID.String()[:8], sid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title, duration_ms, acquisition)
		VALUES ($1,$2,'yt stub',0,'youtube'), ($3,$4,'nas hole',0,'')`, ytID, libID, nasID, libID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id IN ($1,$2)`, ytID, nasID)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, libID)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, sid)
	})

	h := s.Router()
	get := func(id uuid.UUID) *httptest.ResponseRecorder {
		tok := stream.Sign(s.SignKey, id, time.Hour, "")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/"+id.String()+"/stream?token="+tok, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	yt := get(ytID)
	if yt.Code != http.StatusConflict {
		t.Fatalf("youtube stub status %d body %s", yt.Code, yt.Body.String())
	}
	var ytBody map[string]any
	if err := json.Unmarshal(yt.Body.Bytes(), &ytBody); err != nil {
		t.Fatal(err)
	}
	if ytBody["code"] != streamCodeUnavailable {
		t.Fatalf("youtube code %v", ytBody["code"])
	}

	nas := get(nasID)
	if nas.Code != http.StatusNotFound {
		t.Fatalf("nas status %d body %s", nas.Code, nas.Body.String())
	}
	var nasBody map[string]any
	if err := json.Unmarshal(nas.Body.Bytes(), &nasBody); err != nil {
		t.Fatal(err)
	}
	if nasBody["code"] != streamCodeUnavailableExternal {
		t.Fatalf("nas code %v", nasBody["code"])
	}

	p, err := s.lookupPlayability(ctx, ytID)
	if err != nil {
		t.Fatal(err)
	}
	if p.State != MediaStateRestoring {
		t.Fatalf("playability state %s", p.State)
	}
	nasP, err := s.lookupPlayability(ctx, nasID)
	if err != nil {
		t.Fatal(err)
	}
	if nasP.State != MediaStateMissingExternal {
		t.Fatalf("nas playability %s", nasP.State)
	}
}
