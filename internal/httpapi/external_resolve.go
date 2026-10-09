package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/matcher"
	"github.com/sounddock/sounddock/internal/minilib"
	"github.com/sounddock/sounddock/internal/playback"
	"github.com/sounddock/sounddock/internal/scapex"
)

const maxResolveBatch = 25

type resolveIn struct {
	Title      string   `json:"title"`
	Artists    []string `json:"artists"`
	DurationMS int      `json:"duration_ms"`
	ISRC       string   `json:"isrc"`
}

type resolveOut struct {
	Ref        string `json:"ref,omitempty"`
	Source     string `json:"source"` // library, youtube, none
	Title      string `json:"title,omitempty"`
	Artist     string `json:"artist,omitempty"`
	DurationMS int    `json:"duration_ms,omitempty"`
}

// resolveProviderTracks maps songs from a streaming-service playlist to
// something SoundDock can play: the library copy when there is one, otherwise
// the best matching song on YouTube (downloaded when played or added).
func (s *Server) resolveProviderTracks(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Tracks []resolveIn `json:"tracks"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	if len(body.Tracks) > maxResolveBatch {
		body.Tracks = body.Tracks[:maxResolveBatch]
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	libs := s.libraryIDs(ctx, currentUser(r))
	out := make([]resolveOut, len(body.Tracks))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, t := range body.Tracks {
		wg.Add(1)
		go func(i int, t resolveIn) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = s.resolveOne(ctx, libs, t)
		}(i, t)
	}
	wg.Wait()
	writeJSON(w, 200, map[string]any{"items": out})
}

func (s *Server) resolveOne(ctx context.Context, libs []uuid.UUID, t resolveIn) resolveOut {
	t.Title = strings.TrimSpace(t.Title)
	if t.Title == "" {
		return resolveOut{Source: "none"}
	}
	m := matcher.Match(ctx, s.Pool, libs, matcher.Query{Title: t.Title, Artists: t.Artists, DurationMS: t.DurationMS, ISRC: t.ISRC})
	if m.TrackID != nil && (m.Status == "exact" || m.Status == "high") {
		return resolveOut{Ref: m.TrackID.String(), Source: "library"}
	}
	if s.ScapeX == nil {
		return resolveOut{Source: "none"}
	}
	q := t.Title
	if len(t.Artists) > 0 {
		q = strings.TrimSpace(t.Artists[0]) + " - " + t.Title
	}
	hits, err := s.YouTube().Search(ctx, q, 6)
	if err != nil {
		return resolveOut{Source: "none"}
	}
	want := matcher.NormaliseTitle(t.Title)
	var best *scapex.Hit
	for i := range hits {
		h := hits[i]
		if !scapex.LooksLikeMusic(h) {
			continue
		}
		if want != "" && !strings.Contains(matcher.NormaliseTitle(h.Title), want) {
			continue
		}
		if best == nil {
			best = &hits[i]
		}
		// Prefer the upload whose length matches the original recording.
		if t.DurationMS > 0 && h.DurationMS > 0 && abs(h.DurationMS-t.DurationMS) <= 8000 {
			best = &hits[i]
			break
		}
	}
	if best == nil {
		return resolveOut{Source: "none"}
	}
	artist := strings.Join(t.Artists, ", ")
	if artist == "" {
		artist = best.Artist
	}
	return resolveOut{Ref: best.ID, Source: "youtube", Title: t.Title, Artist: artist, DurationMS: best.DurationMS}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// addToMyLibrary saves songs to the caller's personal library without playing
// them. YouTube refs are downloaded into the catalogue first.
func (s *Server) addToMyLibrary(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Refs   []string         `json:"refs"`
		Tracks []queueTrackHint `json:"tracks"`
	}
	if err := decodeJSON(r, &body); err != nil || len(body.Refs) == 0 {
		writeErr(w, 400, "invalid", "refs required")
		return
	}
	if len(body.Refs) > 200 {
		body.Refs = body.Refs[:200]
	}
	ctx, refs := s.acquirePlayCtx(r, body.Refs, body.Tracks)
	ids, err := s.resolvePlayTracks(ctx, refs)
	if err != nil {
		writeErr(w, 502, "resolve", err.Error())
		return
	}
	u := currentUser(r)
	if err := minilib.Record(r.Context(), s.Pool, playback.OriginUser, u.ID, s.discordUserID(r), ids); err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	_ = playback.Notify(context.WithoutCancel(r.Context()), s.Pool, playback.Signal{
		T: "resource.invalidate", Scope: "user", Actor: u.ID.String(), Keys: []string{"personal-library", "home"},
	})
	writeJSON(w, 200, map[string]any{"added": len(ids), "track_ids": ids})
}

// myJob reports progress for a job the caller started (imports, fetches), with
// whatever item counts the job recorded.
func (s *Server) myJob(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "id")
		return
	}
	var typ, status string
	var progress int
	var lastErr *string
	var result []byte
	err = s.Pool.QueryRow(r.Context(), `
		SELECT type, status, progress, last_error, coalesce(result,'{}'::jsonb)
		FROM jobs WHERE id=$1 AND payload->>'user_id' = $2`, id, currentUser(r).ID.String()).
		Scan(&typ, &status, &progress, &lastErr, &result)
	if err != nil {
		writeErr(w, 404, "not_found", "job not found")
		return
	}
	writeJSON(w, 200, map[string]any{
		"id": id, "type": typ, "status": status, "progress": progress,
		"error": lastErr, "result": json.RawMessage(result),
	})
}
