package matcher

import (
	"context"
	"regexp"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Query struct {
	Provider   string
	ID         string
	Title      string
	Artists    []string
	DurationMS int
	ISRC       string
}

type Result struct {
	TrackID    *uuid.UUID
	Status     string // exact, high, possible, unmatched, ambiguous
	Confidence float64
	Source     string
}

func Match(ctx context.Context, pool *pgxpool.Pool, libs []uuid.UUID, t Query) Result {
	if len(libs) == 0 {
		return Result{Status: "unmatched"}
	}
	if t.ISRC != "" {
		var id uuid.UUID
		err := pool.QueryRow(ctx, `SELECT id FROM tracks WHERE isrc=$1 AND library_id = ANY($2) LIMIT 2`, strings.ToUpper(strings.TrimSpace(t.ISRC)), libs).Scan(&id)
		if err == nil {
			return Result{TrackID: &id, Status: "exact", Confidence: 1, Source: "isrc"}
		}
	}
	var mapped uuid.UUID
	err := pool.QueryRow(ctx, `SELECT sounddock_track_id FROM external_track_mappings WHERE provider=$1 AND provider_track_id=$2`, t.Provider, t.ID).Scan(&mapped)
	if err == nil {
		return Result{TrackID: &mapped, Status: "exact", Confidence: 1, Source: "provider_id"}
	}

	title := NormaliseTitle(t.Title)
	artist := ""
	if len(t.Artists) > 0 {
		artist = NormaliseTitle(t.Artists[0])
	}
	if title == "" {
		return Result{Status: "unmatched"}
	}

	rows, err := pool.Query(ctx, `
		SELECT t.id, t.title, t.duration_ms, coalesce(string_agg(ar.name, ' '), '')
		FROM tracks t
		LEFT JOIN track_artists ta ON ta.track_id=t.id AND ta.role='primary'
		LEFT JOIN artists ar ON ar.id=ta.artist_id
		WHERE t.library_id = ANY($1) AND lower(t.title) % $2
		GROUP BY t.id
		ORDER BY similarity(lower(t.title), $2) DESC
		LIMIT 8`, libs, title)
	if err != nil {
		return Result{Status: "unmatched"}
	}
	defer rows.Close()
	type cand struct {
		id   uuid.UUID
		tit  string
		dur  int
		art  string
		score float64
	}
	var cs []cand
	for rows.Next() {
		var c cand
		_ = rows.Scan(&c.id, &c.tit, &c.dur, &c.art)
		nt, na := NormaliseTitle(c.tit), NormaliseTitle(c.art)
		c.score = 0
		if nt == title {
			c.score += 0.55
		} else if strings.Contains(nt, title) || strings.Contains(title, nt) {
			c.score += 0.25
		}
		if artist != "" && na == artist {
			c.score += 0.35
		} else if artist != "" && (strings.Contains(na, artist) || strings.Contains(artist, na)) {
			c.score += 0.15
		}
		if t.DurationMS > 0 && c.dur > 0 {
			d := t.DurationMS - c.dur
			if d < 0 {
				d = -d
			}
			if d <= 3000 {
				c.score += 0.15
			} else if d <= 8000 {
				c.score += 0.05
			} else {
				c.score -= 0.2
			}
		}
		if c.score >= 0.45 {
			cs = append(cs, c)
		}
	}
	if len(cs) == 0 {
		return Result{Status: "unmatched"}
	}
	best := cs[0]
	for _, c := range cs[1:] {
		if c.score > best.score {
			best = c
		}
	}
	closeCount := 0
	for _, c := range cs {
		if best.score-c.score < 0.08 {
			closeCount++
		}
	}
	id := best.id
	if closeCount > 1 && best.score < 0.95 {
		return Result{Status: "ambiguous", Confidence: best.score, Source: "fuzzy"}
	}
	if best.score >= 0.9 {
		return Result{TrackID: &id, Status: "high", Confidence: best.score, Source: "artist_title"}
	}
	if best.score >= 0.7 {
		return Result{TrackID: &id, Status: "possible", Confidence: best.score, Source: "fuzzy"}
	}
	return Result{Status: "unmatched", Confidence: best.score}
}

var featRe = regexp.MustCompile(`(?i)\s*[\(\[]?(feat\.?|ft\.?|featuring)\s+.*?[\)\]]?`)
var parenRe = regexp.MustCompile(`[\(\[].*?[\)\]]`)
var remasterRe = regexp.MustCompile(`(?i)\b(remaster(ed)?|explicit|clean version|radio edit)\b`)

func NormaliseTitle(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = remasterRe.ReplaceAllString(s, "")
	s = parenRe.ReplaceAllString(s, "")
	s = featRe.ReplaceAllString(s, "")
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
