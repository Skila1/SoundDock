package search

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type userCtxKey struct{}

// WithUser scopes played:never / last_played filters to this listener.
// Integrator should wrap Server.search: search.WithUser(r.Context(), currentUser(r).ID).
func WithUser(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, userCtxKey{}, id)
}

func userFrom(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(userCtxKey{}).(uuid.UUID)
	return id
}

type Engine struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Engine { return &Engine{pool: pool} }

type Hit struct {
	Type     string         `json:"type"`
	ID       uuid.UUID      `json:"id"`
	Title    string         `json:"title"`
	Artist   string         `json:"artist,omitempty"`
	Album    string         `json:"album,omitempty"`
	Duration int            `json:"duration_ms,omitempty"`
	Artwork  string         `json:"artwork_url,omitempty"`
	Codec    string         `json:"codec,omitempty"`
	Explicit *bool          `json:"explicit,omitempty"`
	Year     *int           `json:"year,omitempty"`
	Score    float64        `json:"score"`
	Extra    map[string]any `json:"-"`
}

func (e *Engine) Search(ctx context.Context, q string, types []string, libs []uuid.UUID, limit int) ([]Hit, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	pq := Parse(q)
	if len(types) == 0 {
		types = []string{"track", "album", "artist", "playlist"}
	}
	var hits []Hit
	for _, t := range types {
		switch t {
		case "track":
			h, err := e.tracks(ctx, pq, libs, limit)
			if err != nil {
				return nil, err
			}
			hits = append(hits, h...)
		case "album":
			h, err := e.albums(ctx, pq, libs, limit)
			if err != nil {
				return nil, err
			}
			hits = append(hits, h...)
		case "artist":
			h, err := e.artists(ctx, pq, limit)
			if err != nil {
				return nil, err
			}
			hits = append(hits, h...)
		case "playlist":
			h, err := e.playlists(ctx, pq, limit)
			if err != nil {
				return nil, err
			}
			hits = append(hits, h...)
		}
	}
	return hits, nil
}

func (e *Engine) Suggest(ctx context.Context, q string, libs []uuid.UUID, limit int) ([]Hit, error) {
	if limit <= 0 || limit > 25 {
		limit = 8
	}
	return e.Search(ctx, q, []string{"track", "album", "artist"}, libs, limit)
}

func libFilter(libs []uuid.UUID, alias string) (string, []any) {
	if len(libs) == 0 {
		return "TRUE", nil
	}
	ph := make([]string, len(libs))
	args := make([]any, len(libs))
	for i, id := range libs {
		ph[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	return alias + ` IN (` + strings.Join(ph, ",") + `)`, args
}

// textMatchSQL requires every significant query token to appear in title, album, or artist.
// A strong trigram on the full title or artist is allowed for typos. Weak similarity
// (the old 0.15 cutoff) is not, because it ranked unrelated tracks as hits.
func textMatchSQL(text, titleCol, albumCol, trackIDCol string, args []any) (string, []any) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "TRUE", args
	}
	tokens := SignificantTokens(text)
	args = append(args, text)
	textArg := len(args)
	if len(tokens) == 0 {
		args = append(args, likeContains(text))
		n := len(args)
		return fmt.Sprintf("unaccent(lower(%s)) LIKE unaccent(lower($%d)) ESCAPE '\\\\'", titleCol, n), args
	}
	parts := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		args = append(args, likeContains(tok))
		n := len(args)
		parts = append(parts, fmt.Sprintf(`(
			unaccent(lower(%s)) LIKE unaccent(lower($%d)) ESCAPE '\\'
			OR unaccent(lower(coalesce(%s,''))) LIKE unaccent(lower($%d)) ESCAPE '\\'
			OR EXISTS (
				SELECT 1 FROM track_artists ta_s JOIN artists ar_s ON ar_s.id=ta_s.artist_id
				WHERE ta_s.track_id=%s AND unaccent(lower(ar_s.name)) LIKE unaccent(lower($%d)) ESCAPE '\\'
			)
		)`, titleCol, n, albumCol, n, trackIDCol, n))
	}
	fuzzy := fmt.Sprintf(`similarity(unaccent(lower(%s)), unaccent(lower($%d))) >= 0.4
		OR word_similarity(unaccent(lower($%d)), unaccent(lower(%s))) >= 0.5
		OR EXISTS (
			SELECT 1 FROM track_artists ta_f JOIN artists ar_f ON ar_f.id=ta_f.artist_id
			WHERE ta_f.track_id=%s AND similarity(unaccent(lower(ar_f.name)), unaccent(lower($%d))) >= 0.45
		)`, titleCol, textArg, textArg, titleCol, trackIDCol, textArg)
	return "(" + strings.Join(parts, " AND ") + ") OR (" + fuzzy + ")", args
}

func (e *Engine) tracks(ctx context.Context, q Query, libs []uuid.UUID, limit int) ([]Hit, error) {
	lf, args := libFilter(libs, "t.library_id")
	text := q.Text
	if q.Title != "" {
		text = strings.TrimSpace(text + " " + q.Title)
	}
	textArg := 0
	if strings.TrimSpace(text) != "" {
		args = append(args, text)
		textArg = len(args)
	}
	matchSQL, args := textMatchSQL(text, "t.title", "al.title", "t.id", args)
	artistF := ""
	if q.Artist != "" {
		args = append(args, likeContains(q.Artist))
		artistF = fmt.Sprintf(" AND EXISTS (SELECT 1 FROM track_artists ta JOIN artists ar ON ar.id=ta.artist_id WHERE ta.track_id=t.id AND unaccent(lower(ar.name)) LIKE unaccent(lower($%d)) ESCAPE '\\')", len(args))
	}
	albumF := ""
	if q.Album != "" {
		args = append(args, likeContains(q.Album))
		albumF = fmt.Sprintf(" AND unaccent(lower(al.title)) LIKE unaccent(lower($%d)) ESCAPE '\\'", len(args))
	}
	playF, args := playFilterSQL(ctx, q, args)
	args = append(args, limit)
	rankSQL := "0"
	if textArg > 0 {
		rankSQL = fmt.Sprintf("ts_rank(t.search_vec, websearch_to_tsquery('simple', unaccent($%d))) + similarity(t.title, $%d)", textArg, textArg)
	}
	sql := fmt.Sprintf(`
		SELECT t.id, t.title, coalesce(string_agg(DISTINCT ar.name, ', ') FILTER (WHERE ta.role='primary'), ''),
		       coalesce(al.title,''), t.duration_ms, coalesce(tf.codec,''), t.explicit, t.year,
		       %s AS score
		FROM tracks t
		LEFT JOIN track_artists ta ON ta.track_id=t.id
		LEFT JOIN artists ar ON ar.id=ta.artist_id
		LEFT JOIN albums al ON al.id=t.album_id
		LEFT JOIN LATERAL (SELECT codec FROM track_files WHERE track_id=t.id LIMIT 1) tf ON TRUE
		WHERE (%s)
		  AND (%s)
		  AND NOT (
		    coalesce(t.acquisition,'') IN ('youtube','scapex')
		    AND NOT EXISTS (
		      SELECT 1 FROM track_files tf0
		      WHERE tf0.track_id=t.id AND tf0.deleted_at IS NULL
		    )
		  )
		  %s %s %s
		GROUP BY t.id, al.title, tf.codec
		ORDER BY score DESC
		LIMIT $%d`, rankSQL, lf, matchSQL, artistF, albumF, playF, len(args))
	rows, err := e.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var h Hit
		h.Type = "track"
		if err := rows.Scan(&h.ID, &h.Title, &h.Artist, &h.Album, &h.Duration, &h.Codec, &h.Explicit, &h.Year, &h.Score); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (e *Engine) albums(ctx context.Context, q Query, libs []uuid.UUID, limit int) ([]Hit, error) {
	lf, args := libFilter(libs, "a.library_id")
	text := q.Text
	if q.Album != "" {
		text = q.Album
	}
	textArg := 0
	matchSQL := "TRUE"
	if strings.TrimSpace(text) != "" {
		args = append(args, text)
		textArg = len(args)
		var parts []string
		for _, tok := range SignificantTokens(text) {
			args = append(args, likeContains(tok))
			n := len(args)
			parts = append(parts, fmt.Sprintf(`(
				unaccent(lower(a.title)) LIKE unaccent(lower($%d)) ESCAPE '\\'
				OR EXISTS (
					SELECT 1 FROM album_artists aa_s JOIN artists ar_s ON ar_s.id=aa_s.artist_id
					WHERE aa_s.album_id=a.id AND unaccent(lower(ar_s.name)) LIKE unaccent(lower($%d)) ESCAPE '\\'
				)
			)`, n, n))
		}
		if len(parts) > 0 {
			matchSQL = "(" + strings.Join(parts, " AND ") + fmt.Sprintf(`) OR similarity(unaccent(lower(a.title)), unaccent(lower($%d))) >= 0.4`, textArg)
		}
	}
	args = append(args, limit)
	rankSQL := "0"
	if textArg > 0 {
		rankSQL = fmt.Sprintf("ts_rank(a.search_vec, websearch_to_tsquery('simple', unaccent($%d))) + similarity(a.title, $%d)", textArg, textArg)
	}
	sql := fmt.Sprintf(`
		SELECT a.id, a.title, coalesce(string_agg(ar.name, ', '), ''), a.year,
		       %s
		FROM albums a
		LEFT JOIN album_artists aa ON aa.album_id=a.id
		LEFT JOIN artists ar ON ar.id=aa.artist_id
		WHERE (%s) AND (%s)
		GROUP BY a.id
		ORDER BY 4 DESC
		LIMIT $%d`, rankSQL, lf, matchSQL, len(args))
	rows, err := e.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		h := Hit{Type: "album"}
		if err := rows.Scan(&h.ID, &h.Title, &h.Artist, &h.Year, &h.Score); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (e *Engine) artists(ctx context.Context, q Query, limit int) ([]Hit, error) {
	text := q.Text
	if q.Artist != "" {
		text = q.Artist
	}
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	args := []any{text}
	match := "similarity(unaccent(lower(name)), unaccent(lower($1))) >= 0.4"
	var parts []string
	for _, tok := range SignificantTokens(text) {
		args = append(args, likeContains(tok))
		parts = append(parts, fmt.Sprintf("unaccent(lower(name)) LIKE unaccent(lower($%d)) ESCAPE '\\'", len(args)))
	}
	if len(parts) > 0 {
		match = "(" + strings.Join(parts, " AND ") + ") OR " + match
	}
	args = append(args, limit)
	rows, err := e.pool.Query(ctx, fmt.Sprintf(`
		SELECT id, name, ts_rank(search_vec, websearch_to_tsquery('simple', unaccent($1))) + similarity(name, $1)
		FROM artists
		WHERE %s
		ORDER BY 3 DESC LIMIT $%d`, match, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		h := Hit{Type: "artist"}
		if err := rows.Scan(&h.ID, &h.Title, &h.Score); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (e *Engine) playlists(ctx context.Context, q Query, limit int) ([]Hit, error) {
	text := strings.TrimSpace(q.Text)
	if text == "" {
		return nil, nil
	}
	args := []any{text}
	match := "similarity(unaccent(lower(name)), unaccent(lower($1))) >= 0.4"
	var parts []string
	for _, tok := range SignificantTokens(text) {
		args = append(args, likeContains(tok))
		parts = append(parts, fmt.Sprintf("unaccent(lower(name)) LIKE unaccent(lower($%d)) ESCAPE '\\'", len(args)))
	}
	if len(parts) > 0 {
		match = "(" + strings.Join(parts, " AND ") + ") OR " + match
	}
	args = append(args, limit)
	rows, err := e.pool.Query(ctx, fmt.Sprintf(`
		SELECT id, name FROM playlists
		WHERE %s
		ORDER BY similarity(name,$1) DESC LIMIT $%d`, match, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		h := Hit{Type: "playlist"}
		if err := rows.Scan(&h.ID, &h.Title); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func playFilterSQL(ctx context.Context, q Query, args []any) (string, []any) {
	uid := userFrom(ctx)
	var b strings.Builder
	userPred := func() string {
		if uid == uuid.Nil {
			return ""
		}
		args = append(args, uid)
		return fmt.Sprintf(" AND pc.user_id=$%d", len(args))
	}
	if q.NeverPlayed {
		u := userPred()
		b.WriteString(" AND NOT EXISTS (SELECT 1 FROM play_counts pc WHERE pc.track_id=t.id AND pc.count>0" + u + ")")
	}
	if q.HasPlayed {
		u := userPred()
		b.WriteString(" AND EXISTS (SELECT 1 FROM play_counts pc WHERE pc.track_id=t.id AND pc.count>0" + u + ")")
	}
	if q.LastPlayedWithin > 0 {
		args = append(args, time.Now().Add(-q.LastPlayedWithin))
		ph := len(args)
		u := userPred()
		b.WriteString(fmt.Sprintf(" AND EXISTS (SELECT 1 FROM play_counts pc WHERE pc.track_id=t.id AND pc.last_played_at >= $%d%s)", ph, u))
	}
	if q.LastPlayedBefore > 0 {
		args = append(args, time.Now().Add(-q.LastPlayedBefore))
		ph := len(args)
		u := userPred()
		b.WriteString(fmt.Sprintf(" AND EXISTS (SELECT 1 FROM play_counts pc WHERE pc.track_id=t.id AND pc.last_played_at IS NOT NULL AND pc.last_played_at < $%d%s)", ph, u))
	}
	if q.LastPlayedAfter != nil {
		args = append(args, *q.LastPlayedAfter)
		ph := len(args)
		u := userPred()
		b.WriteString(fmt.Sprintf(" AND EXISTS (SELECT 1 FROM play_counts pc WHERE pc.track_id=t.id AND pc.last_played_at >= $%d%s)", ph, u))
	}
	return b.String(), args
}
