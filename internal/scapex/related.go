package scapex

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ValidVideoID reports whether id looks like a YouTube video ID.
func ValidVideoID(id string) bool { return videoIDRe.MatchString(strings.TrimSpace(id)) }

// Mix lists YouTube's radio for a video: the "up next" mix YouTube Music
// builds from listening data, so entries are songs that actually go with the
// seed rather than keyword matches.
func (y *ytDLP) Mix(ctx context.Context, videoID string, limit int) ([]Hit, error) {
	if !ValidVideoID(videoID) {
		return nil, fmt.Errorf("invalid video id")
	}
	if limit <= 0 {
		limit = 25
	}
	var lastErr error
	// RDAMVM is the YouTube Music radio (songs only); RD is the general mix.
	for _, list := range []string{"RDAMVM" + videoID, "RD" + videoID} {
		out, err := y.run(ctx,
			"--skip-download", "--no-warnings", "--no-progress",
			"--flat-playlist", "--yes-playlist",
			"--playlist-end", strconv.Itoa(limit+1),
			"-J", "https://www.youtube.com/watch?v="+videoID+"&list="+list,
		)
		if err != nil {
			lastErr = err
			continue
		}
		if hits := parsePlaylistDump(out).Hits; len(hits) > 1 {
			return hits, nil
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no mix for %s", videoID)
	}
	return nil, lastErr
}

// Related returns songs YouTube recommends after videoID.
func (s *Service) Related(ctx context.Context, videoID string, limit int) ([]Hit, error) {
	m, ok := s.yt.(interface {
		Mix(context.Context, string, int) ([]Hit, error)
	})
	if !ok {
		return nil, fmt.Errorf("related songs are not supported by this YouTube backend")
	}
	hits, err := m.Mix(ctx, videoID, limit)
	if err != nil {
		return nil, err
	}
	normalizeHits(hits)
	return hits, nil
}

// Related is only available with the in-process ScapeX service.
func (c *Client) Related(ctx context.Context, videoID string, limit int) ([]Hit, error) {
	if c == nil || c.svc == nil {
		return nil, fmt.Errorf("related songs need the local ScapeX service")
	}
	return c.svc.Related(ctx, videoID, limit)
}

// nonMusic matches video titles that are clearly not a song: talk, reaction
// and commentary content, long compilations, and novelty edits.
var nonMusic = regexp.MustCompile(`(?i)\b(` + strings.Join([]string{
	`interview`, `reaction`, `reacts to`, `podcast`, `documentary`, `vlog`, `trailer`, `teaser`,
	`behind the scenes`, `making of`, `explained`, `song breakdown`, `tutorial`, `how to play`, `guitar lesson`,
	`unboxing`, `full album`, `full ep`, `compilation`, `mixtape`, `dj set`, `live stream`, `livestream`,
	`\d+\s*(hours?|hrs?)`, `hour mix`, `nonstop`, `non-stop`, `megamix`, `greatest hits`, `top \d+`,
	`karaoke`, `nightcore`, `slowed`, `sped up`, `8d audio`, `bass boosted`,
	`#shorts`, `tiktok compilation`, `parody`, `prank`, `skit`, `sermon`, `asmr`,
	`audiobook`, `gameplay`, `walkthrough`, `ep\.\s*\d+`,
}, "|") + `)\b`)

// LooksLikeMusic is a conservative check that a YouTube hit is a single song:
// a plausible song length and no talk/compilation/edit markers in the title.
func LooksLikeMusic(h Hit) bool {
	if h.ID == "" || strings.TrimSpace(h.Title) == "" {
		return false
	}
	if h.DurationMS > 0 && (h.DurationMS < 60_000 || h.DurationMS > 10*60_000) {
		return false
	}
	return !nonMusic.MatchString(h.Title)
}
