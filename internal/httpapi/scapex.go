package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/radio"
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
	hits, err := s.YouTube().Search(r.Context(), song, limit)
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
	return s.YouTube().Fetch(ctx, refs)
}

func (s *Server) similarYouTube(ctx context.Context, seed uuid.UUID, need int, have []uuid.UUID) []string {
	if need < 1 || s.ScapeX == nil {
		return nil
	}
	need = radio.ClampFill(need)
	meta, err := radio.New(s.Pool).TrackMeta(ctx, seed)
	if err != nil {
		return nil
	}
	q := radio.SimilarQuery(meta.Title, meta.Artist, meta.Genre)
	if q == "" {
		return nil
	}
	hits, err := s.YouTube().Search(ctx, q, need+8)
	if err != nil || len(hits) == 0 {
		return nil
	}
	ids := append([]uuid.UUID{seed}, have...)
	local := s.trackTitleArtist(ctx, ids)
	var out []string
	seen := map[string]struct{}{}
	for _, h := range hits {
		if h.ID == "" {
			continue
		}
		if _, ok := seen[h.ID]; ok {
			continue
		}
		if radio.SameSong(h.Title, meta.Title) {
			continue
		}
		if scapex.AlreadyInLibrary(h.Title, h.Artist, local) {
			continue
		}
		seen[h.ID] = struct{}{}
		out = append(out, h.ID)
		if len(out) >= need {
			break
		}
	}
	return out
}

func (s *Server) trackTitleArtist(ctx context.Context, ids []uuid.UUID) []map[string]any {
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT t.title, coalesce((SELECT string_agg(ar.name, ', ' ORDER BY ta.position)
		  FROM track_artists ta JOIN artists ar ON ar.id=ta.artist_id
		  WHERE ta.track_id=t.id AND ta.role='primary'),'')
		FROM tracks t WHERE t.id = ANY($1)`, ids)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var title, artist string
		if err := rows.Scan(&title, &artist); err != nil {
			continue
		}
		out = append(out, map[string]any{"type": "track", "title": title, "artist": artist})
	}
	return out
}

var errScapeXDown = errString("ScapeX is not running")

type errString string

func (e errString) Error() string { return string(e) }
