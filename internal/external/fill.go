package external

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/matcher"
	"github.com/sounddock/sounddock/internal/scapex"
)

func pickYouTubeID(hits []scapex.Hit, title string, artists []string) string {
	wantT := matcher.NormaliseTitle(title)
	if wantT == "" {
		return ""
	}
	wantA := ""
	if len(artists) > 0 {
		wantA = matcher.NormaliseTitle(artists[0])
	}
	for _, h := range hits {
		if h.ID == "" {
			continue
		}
		nt := matcher.NormaliseTitle(h.Title + " " + h.Artist)
		titleHit := strings.Contains(nt, wantT) || strings.Contains(wantT, matcher.NormaliseTitle(h.Title))
		artistHit := wantA == "" || strings.Contains(nt, wantA)
		if titleHit && artistHit {
			return h.ID
		}
	}
	return ""
}

func youtubeQuery(title string, artists []string) string {
	parts := make([]string, 0, len(artists)+1)
	for _, a := range artists {
		a = strings.TrimSpace(a)
		if a != "" {
			parts = append(parts, a)
		}
	}
	title = strings.TrimSpace(title)
	if title != "" {
		parts = append(parts, title)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func fillFromYouTube(ctx context.Context, sx *scapex.Client, title string, artists []string) (uuid.UUID, error) {
	if sx == nil {
		return uuid.Nil, nil
	}
	q := youtubeQuery(title, artists)
	if q == "" {
		return uuid.Nil, nil
	}
	hits, err := sx.Search(ctx, q, 8)
	if err != nil || len(hits) == 0 {
		return uuid.Nil, err
	}
	id := pickYouTubeID(hits, title, artists)
	if id == "" {
		return uuid.Nil, nil
	}
	got, err := sx.Fetch(ctx, []string{id})
	if err != nil || len(got) == 0 {
		return uuid.Nil, err
	}
	return got[0], nil
}

func FillTrack(ctx context.Context, sx *scapex.Client, provider, videoID, title string, artists []string) (uuid.UUID, error) {
	if provider == "youtube" {
		return fillFromVideo(ctx, sx, videoID)
	}
	return fillFromYouTube(ctx, sx, title, artists)
}

func fillFromVideo(ctx context.Context, sx *scapex.Client, videoID string) (uuid.UUID, error) {
	if sx == nil || strings.TrimSpace(videoID) == "" {
		return uuid.Nil, nil
	}
	got, err := sx.Fetch(ctx, []string{videoID})
	if err != nil || len(got) == 0 {
		return uuid.Nil, err
	}
	return got[0], nil
}
