package httpapi

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/matcher"
	"github.com/sounddock/sounddock/internal/radio"
	"github.com/sounddock/sounddock/internal/scapex"
)

// maxPerArtist keeps one fill from turning into a single artist's discography.
const maxPerArtist = 2

// relatedYouTubeHits picks songs to follow seed from YouTube's own radio mix
// for it. Keyword searches for the artist or genre returned interviews,
// reactions, compilations and other videos that are not songs; the mix is
// built from what people actually play next, and every hit still has to pass
// the music filter.
func (s *Server) relatedYouTubeHits(ctx context.Context, seed uuid.UUID, need int, have []uuid.UUID) []scapex.Hit {
	if need < 1 || s.ScapeX == nil {
		return nil
	}
	meta, err := radio.New(s.Pool).TrackMeta(ctx, seed)
	if err != nil {
		return nil
	}
	vid := s.seedVideoID(ctx, seed, meta.Title, meta.Artist)
	if vid == "" {
		return nil
	}
	var hits []scapex.Hit
	run := func(ctx context.Context) error {
		var e error
		hits, e = s.ScapeX.Related(ctx, vid, 30)
		return e
	}
	if s.Jobs != nil && s.Jobs.Started() {
		err = s.Jobs.Do(ctx, jobs.PoolSearch, run)
	} else {
		err = run(ctx)
	}
	if err != nil || len(hits) == 0 {
		return nil
	}
	return s.pickMusicHits(ctx, hits, vid, meta.Title, need, have)
}

// pickMusicHits keeps hits that are songs, not the seed or anything already
// queued, with at most maxPerArtist per artist.
func (s *Server) pickMusicHits(ctx context.Context, hits []scapex.Hit, seedVID, seedTitle string, need int, have []uuid.UUID) []scapex.Hit {
	local := s.trackTitleArtist(ctx, have)
	seenID := map[string]struct{}{seedVID: {}}
	seenTitle := map[string]struct{}{}
	perArtist := map[string]int{}
	var out []scapex.Hit
	for _, h := range hits {
		if _, dup := seenID[h.ID]; dup || !scapex.LooksLikeMusic(h) {
			continue
		}
		if radio.SameSong(h.Title, seedTitle) || scapex.AlreadyInLibrary(h.Title, h.Artist, local) {
			continue
		}
		title := matcher.NormaliseTitle(h.Title)
		if _, dup := seenTitle[title]; dup && title != "" {
			continue
		}
		artist := artistKey(h.Artist)
		if artist != "" && perArtist[artist] >= maxPerArtist {
			continue
		}
		seenID[h.ID] = struct{}{}
		seenTitle[title] = struct{}{}
		perArtist[artist]++
		out = append(out, h)
		if len(out) >= need {
			break
		}
	}
	return out
}

func artistKey(a string) string {
	a = strings.ToLower(strings.TrimSpace(a))
	a = strings.TrimSuffix(a, " - topic")
	a = strings.TrimSuffix(a, "vevo")
	return strings.TrimSpace(a)
}

// seedVideoID finds the YouTube video for a track: the one it was downloaded
// from, or the best song-like search match for "artist - title".
func (s *Server) seedVideoID(ctx context.Context, seed uuid.UUID, title, artist string) string {
	var ref string
	_ = s.Pool.QueryRow(ctx, `SELECT coalesce(acquisition_ref,'') FROM tracks WHERE id=$1`, seed).Scan(&ref)
	if v := scapex.VideoID(ref); v != "" {
		return v
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	q := title
	if a := strings.TrimSpace(strings.SplitN(artist, ",", 2)[0]); a != "" {
		q = a + " - " + title
	}
	hits, err := s.YouTube().Search(ctx, q, 5)
	if err != nil {
		return ""
	}
	want := matcher.NormaliseTitle(title)
	for _, h := range hits {
		if !scapex.LooksLikeMusic(h) {
			continue
		}
		if want == "" || strings.Contains(matcher.NormaliseTitle(h.Title), want) {
			return h.ID
		}
	}
	return ""
}
