package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/scapex"
)

func (s *Server) searchYouTube(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	song := scapex.SongQuery(q)
	if song == "" || s.ScapeX == nil {
		writeJSON(w, 200, map[string]any{"query": q, "results": []any{}})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 8
	}
	if limit > 16 {
		limit = 16
	}
	hits, err := s.ScapeX.Search(r.Context(), song, limit)
	if err != nil || len(hits) == 0 {
		writeJSON(w, 200, map[string]any{"query": q, "results": []any{}})
		return
	}
	out := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		out = append(out, map[string]any{
			"type":        "youtube",
			"id":          h.ID,
			"title":       h.Title,
			"artist":      h.Artist,
			"album":       h.Album,
			"duration_ms": h.DurationMS,
			"source":      "youtube",
			"artwork_url": h.ArtworkURL,
			"stream_url":  h.StreamURL,
		})
	}
	writeJSON(w, 200, map[string]any{"query": q, "results": out})
}

func (s *Server) resolveQueueTracks(ctx context.Context, refs []string) ([]uuid.UUID, error) {
	tracks, youtube := scapex.ParseTrackRefs(refs)
	if len(youtube) == 0 {
		return tracks, nil
	}
	got, err := s.fetchYouTube(ctx, youtube)
	if err != nil {
		return nil, err
	}
	return append(tracks, got...), nil
}

func (s *Server) fetchYouTube(ctx context.Context, refs []string) ([]uuid.UUID, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	if s.ScapeX == nil {
		return nil, errScapeXDown
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	return s.ScapeX.Fetch(ctx, refs)
}

var errScapeXDown = errString("ScapeX is not running")

type errString string

func (e errString) Error() string { return string(e) }
