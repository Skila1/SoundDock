package external

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/matcher"
	"github.com/sounddock/sounddock/internal/scapex"
)

// Filler is YouTube search + fetch. Callers should pass an isolated
// implementation so yt-dlp runs on the acquisition/search pools.
type Filler interface {
	Searcher
	Fetch(ctx context.Context, refs []string) ([]uuid.UUID, error)
}

type inlineSearchKey struct{}

// WithInlineSearch lets a Filler run searches on the caller's worker
// instead of the interactive search pool (playlist import jobs).
func WithInlineSearch(ctx context.Context) context.Context {
	return context.WithValue(ctx, inlineSearchKey{}, true)
}

func InlineSearch(ctx context.Context) bool {
	v, _ := ctx.Value(inlineSearchKey{}).(bool)
	return v
}

const (
	ytSearchLimit   = 10
	ytInlineTimeout = 45 * time.Second
	ytPickThreshold = 0.65
)

// Words in a video title that mean a different recording, unless the
// requested title has them too.
var ytNoise = []string{"live", "remix", "cover", "karaoke", "sped up", "speed up", "nightcore", "slowed", "8d", "instrumental", "reverb", "bass boosted"}

func containsWord(hay, needle string) bool {
	return strings.Contains(" "+hay+" ", " "+needle+" ")
}

func artistInHit(artists []string, title, channel string) bool {
	ch := matcher.CleanArtist(channel)
	chFlat := strings.ReplaceAll(ch, " ", "")
	for _, a := range artists {
		ca := matcher.CleanArtist(a)
		if ca == "" {
			continue
		}
		if containsWord(title, ca) || ch == ca {
			return true
		}
		if flat := strings.ReplaceAll(ca, " ", ""); chFlat != "" && strings.Contains(chFlat, flat) {
			return true
		}
	}
	return false
}

func scoreYouTubeHit(h scapex.Hit, want []string, rawTitle string, artists []string, durationMS int) float64 {
	vt := matcher.Fold(h.Title)
	have := map[string]bool{}
	for _, w := range strings.Fields(vt) {
		if w == "pt" {
			w = "part"
		}
		have[w] = true
	}
	n := 0
	for _, w := range want {
		if have[w] {
			n++
		}
	}
	overlap := float64(n) / float64(len(want))
	if overlap < 0.6 {
		return 0
	}
	score := overlap * 0.6
	hasArtists := false
	for _, a := range artists {
		if strings.TrimSpace(a) != "" {
			hasArtists = true
		}
	}
	artistHit := artistInHit(artists, vt, h.Artist)
	if artistHit {
		score += 0.3
	} else if !hasArtists {
		score += 0.1
	} else if hasArtists && len(want) <= 2 {
		return 0
	}
	if strings.HasSuffix(strings.TrimSpace(h.Artist), " - Topic") {
		score += 0.05
	}
	if durationMS > 0 && h.DurationMS > 0 {
		d := durationMS - h.DurationMS
		if d < 0 {
			d = -d
		}
		switch {
		case d <= 8000:
			score += 0.15
		case d <= 20000:
			score += 0.05
		case d > 45000:
			score -= 0.3
		}
	}
	rt := matcher.Fold(rawTitle)
	for _, w := range ytNoise {
		if containsWord(vt, w) && !containsWord(rt, w) {
			score -= 0.35
		}
	}
	return score
}

func pickYouTubeID(hits []scapex.Hit, title string, artists []string, durationMS int) (string, float64) {
	want := matcher.Words(title)
	if len(want) == 0 {
		return "", 0
	}
	best, bestScore := "", 0.0
	for _, h := range hits {
		if h.ID == "" {
			continue
		}
		if s := scoreYouTubeHit(h, want, title, artists, durationMS); s >= ytPickThreshold && s > bestScore {
			best, bestScore = h.ID, s
		}
	}
	return best, bestScore
}

func youtubeQueries(title string, artists []string, album string) []string {
	base := matcher.BaseTitle(title)
	if base == "" {
		base = strings.TrimSpace(title)
	}
	if base == "" {
		return nil
	}
	first := ""
	for _, a := range artists {
		if a = strings.TrimSpace(a); a != "" {
			first = a
			break
		}
	}
	var qs []string
	add := func(q string) {
		q = strings.TrimSpace(q)
		for _, x := range qs {
			if strings.EqualFold(x, q) {
				return
			}
		}
		if q != "" {
			qs = append(qs, q)
		}
	}
	if first != "" {
		add(first + " - " + base)
		add(base + " " + first)
	} else {
		add(base)
	}
	if album = strings.TrimSpace(album); album != "" && matcher.CleanTitle(album) != matcher.CleanTitle(base) {
		add(base + " " + album)
	}
	return qs
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "timeout")
}

func searchYouTube(ctx context.Context, sx Searcher, q string) ([]scapex.Hit, error) {
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		sctx, cancel := ctx, context.CancelFunc(func() {})
		if InlineSearch(ctx) {
			sctx, cancel = context.WithTimeout(ctx, ytInlineTimeout)
		}
		var hits []scapex.Hit
		hits, err = sx.Search(sctx, q, ytSearchLimit)
		cancel()
		if err == nil {
			return hits, nil
		}
		if ctx.Err() != nil || !isTimeout(err) {
			break
		}
	}
	slog.Warn("youtube fill search", "query", q, "err", err)
	return nil, err
}

// Searcher is the search half of Filler.
type Searcher interface {
	Search(ctx context.Context, q string, limit int) ([]scapex.Hit, error)
}

// FindYouTube picks the YouTube upload for a streaming-service song without
// downloading it. Tries each query in turn; ok is false when nothing fits.
func FindYouTube(ctx context.Context, sx Searcher, title string, artists []string, album string, durationMS int) (scapex.Hit, bool, error) {
	if sx == nil {
		return scapex.Hit{}, false, nil
	}
	var lastErr error
	searched := false
	for _, q := range youtubeQueries(title, artists, album) {
		hits, err := searchYouTube(ctx, sx, q)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return scapex.Hit{}, false, ctx.Err()
			}
			continue
		}
		searched = true
		ranked := scapex.RankHits(q, hits)
		id, _ := pickYouTubeID(ranked, title, artists, durationMS)
		if id == "" {
			continue
		}
		for _, h := range ranked {
			if h.ID == id {
				return h, true, nil
			}
		}
	}
	if !searched {
		return scapex.Hit{}, false, lastErr
	}
	return scapex.Hit{}, false, nil
}

func fillFromYouTube(ctx context.Context, sx Filler, title string, artists []string, album string, durationMS int) (uuid.UUID, error) {
	if sx == nil {
		return uuid.Nil, nil
	}
	h, ok, err := FindYouTube(ctx, sx, title, artists, album, durationMS)
	if err != nil || !ok {
		return uuid.Nil, err
	}
	got, err := sx.Fetch(ctx, []string{h.ID})
	if err != nil || len(got) == 0 {
		return uuid.Nil, err
	}
	return got[0], nil
}

func FillTrack(ctx context.Context, sx Filler, provider, videoID, title string, artists []string) (uuid.UUID, error) {
	return FillTrackInfo(ctx, sx, provider, Track{ID: videoID, Title: title, Artists: artists})
}

// FillTrackInfo is FillTrack with album and duration, which improve the
// YouTube pick.
func FillTrackInfo(ctx context.Context, sx Filler, provider string, tr Track) (uuid.UUID, error) {
	if provider == "youtube" {
		return fillFromVideo(ctx, sx, tr.ID)
	}
	return fillFromYouTube(ctx, sx, tr.Title, tr.Artists, tr.Album, tr.DurationMS)
}

func fillFromVideo(ctx context.Context, sx Filler, videoID string) (uuid.UUID, error) {
	if sx == nil || strings.TrimSpace(videoID) == "" {
		return uuid.Nil, nil
	}
	got, err := sx.Fetch(ctx, []string{videoID})
	if err != nil || len(got) == 0 {
		return uuid.Nil, err
	}
	return got[0], nil
}
