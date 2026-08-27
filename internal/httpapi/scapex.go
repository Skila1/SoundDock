package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/scapex"
)

func (s *Server) appendScapeX(r *http.Request, q, typ string, local []map[string]any) []map[string]any {
	if s.ScapeX == nil {
		return local
	}
	if typ != "" && !strings.Contains(strings.ToLower(typ), "track") {
		return local
	}
	song := scapex.SongQuery(q)
	if song == "" {
		return local
	}
	if !s.ScapeX.Ready(r.Context()) {
		return local
	}
	hits, err := s.ScapeX.Search(r.Context(), song, 8)
	if err != nil || len(hits) == 0 {
		return local
	}
	for _, h := range hits {
		if scapex.AlreadyInLibrary(h.Title, h.Artist, local) {
			continue
		}
		item := map[string]any{
			"type":        "youtube",
			"id":          h.ID,
			"title":       h.Title,
			"artist":      h.Artist,
			"album":       h.Album,
			"duration_ms": h.DurationMS,
			"source":      "youtube",
			"artwork_url": h.ArtworkURL,
			"stream_url":  h.StreamURL,
		}
		local = append(local, item)
	}
	return local
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
