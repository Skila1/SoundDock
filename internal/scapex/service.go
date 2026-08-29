package scapex

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type Service struct {
	yt   YouTube
	dock *Dock
}

func NewService(yt YouTube, dock *Dock) *Service {
	if yt == nil {
		yt = &ytDLP{
			bin:     getenv("SCAPEX_YTDLP"),
			cookies: getenv("SCAPEX_YT_COOKIES"),
			browser: getenv("SCAPEX_YT_BROWSER"),
		}
	}
	return &Service{yt: yt, dock: dock}
}

func (s *Service) Search(ctx context.Context, q string, limit int) ([]Hit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, fmt.Errorf("query required")
	}
	if IsPlaylistQuery(q) {
		listing, err := s.ListPlaylist(ctx, q, limit)
		return listing.Hits, err
	}
	if limit <= 0 {
		limit = 8
	}
	if limit > 25 {
		limit = 25
	}
	if WatchURL(q) != "" {
		limit = 1
	}
	hits, err := s.yt.Search(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	normalizeHits(hits)
	return hits, nil
}

func (s *Service) ListPlaylist(ctx context.Context, raw string, limit int) (PlaylistListing, error) {
	raw = strings.TrimSpace(raw)
	if !IsPlaylistQuery(raw) {
		return PlaylistListing{}, fmt.Errorf("not a YouTube playlist URL")
	}
	if limit <= 0 {
		limit = MaxPlaylistQueue
	}
	if limit > MaxPlaylistQueue {
		limit = MaxPlaylistQueue
	}
	var listing PlaylistListing
	var err error
	if lister, ok := s.yt.(interface {
		ListPlaylist(context.Context, string, int) (PlaylistListing, error)
	}); ok {
		listing, err = lister.ListPlaylist(ctx, raw, limit)
	} else {
		var hits []Hit
		hits, err = s.yt.Search(ctx, raw, limit)
		listing = PlaylistListing{ID: PlaylistID(raw), Hits: hits, Total: len(hits)}
	}
	if err != nil {
		return PlaylistListing{}, err
	}
	normalizeHits(listing.Hits)
	if listing.ID == "" {
		listing.ID = PlaylistID(raw)
	}
	if listing.Total < len(listing.Hits) {
		listing.Total = len(listing.Hits)
	}
	if len(listing.Hits) >= limit && listing.Total > limit {
		listing.Truncated = true
	}
	return listing, nil
}

func normalizeHits(hits []Hit) {
	for i := range hits {
		if hits[i].Type == "" {
			hits[i].Type = "youtube"
		}
		if hits[i].ArtworkURL == "" {
			hits[i].ArtworkURL = ytThumb(hits[i].ID)
		}
		if hits[i].StreamURL == "" && hits[i].ID != "" {
			hits[i].StreamURL = "https://www.youtube.com/watch?v=" + hits[i].ID
		}
	}
}

func (s *Service) Fetch(ctx context.Context, raw string) ([]uuid.UUID, error) {
	return s.RunFetchJob(ctx, FetchOpts{
		JobID:  uuid.New(),
		URLs:   []string{raw},
		Policy: DefaultMediaPolicy,
	})
}
