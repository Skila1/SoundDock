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
	return hits, nil
}

func (s *Service) Fetch(ctx context.Context, raw string) ([]uuid.UUID, error) {
	return s.RunFetchJob(ctx, FetchOpts{
		JobID:  uuid.New(),
		URLs:   []string{raw},
		Policy: DefaultMediaPolicy,
	})
}
