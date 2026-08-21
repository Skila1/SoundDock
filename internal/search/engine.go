package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

func (e *Engine) tracks(ctx context.Context, q Query, libs []uuid.UUID, limit int) ([]Hit, error) {
	lf, args := libFilter(libs, "t.library_id")
	off := len(args)
	text := q.Text
	if q.Title != "" {
		text = strings.TrimSpace(text + " " + q.Title)
	}
	args = append(args, text, "%"+text+"%", limit)
	artistF := ""
	if q.Artist != "" {
		off++
		args = append(args, "%"+q.Artist+"%")
		artistF = fmt.Sprintf(" AND EXISTS (SELECT 1 FROM track_artists ta JOIN artists ar ON ar.id=ta.artist_id WHERE ta.track_id=t.id AND ar.name ILIKE $%d)", len(args))
	}
	albumF := ""
	if q.Album != "" {
		args = append(args, "%"+q.Album+"%")
		albumF = fmt.Sprintf(" AND al.title ILIKE $%d", len(args))
	}
	sql := fmt.Sprintf(`
		SELECT t.id, t.title, coalesce(string_agg(DISTINCT ar.name, ', ') FILTER (WHERE ta.role='primary'), ''),
		       coalesce(al.title,''), t.duration_ms, coalesce(tf.codec,''), t.explicit, t.year,
		       ts_rank(t.search_vec, websearch_to_tsquery('simple', unaccent($%d))) + similarity(t.title, $%d) AS score
		FROM tracks t
		LEFT JOIN track_artists ta ON ta.track_id=t.id
		LEFT JOIN artists ar ON ar.id=ta.artist_id
		LEFT JOIN albums al ON al.id=t.album_id
		LEFT JOIN LATERAL (SELECT codec FROM track_files WHERE track_id=t.id LIMIT 1) tf ON TRUE
		WHERE (%s)
		  AND (t.search_vec @@ websearch_to_tsquery('simple', unaccent($%d)) OR t.title ILIKE $%d OR similarity(t.title, $%d) > 0.15)
		  %s %s
		GROUP BY t.id, al.title, tf.codec
		ORDER BY score DESC
		LIMIT $%d`, off+1, off+1, lf, off+1, off+2, off+1, artistF, albumF, off+3)
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
	off := len(args)
	text := q.Text
	if q.Album != "" {
		text = q.Album
	}
	args = append(args, text, "%"+text+"%", limit)
	sql := fmt.Sprintf(`
		SELECT a.id, a.title, coalesce(string_agg(ar.name, ', '), ''), a.year,
		       ts_rank(a.search_vec, websearch_to_tsquery('simple', unaccent($%d))) + similarity(a.title, $%d)
		FROM albums a
		LEFT JOIN album_artists aa ON aa.album_id=a.id
		LEFT JOIN artists ar ON ar.id=aa.artist_id
		WHERE (%s) AND (a.search_vec @@ websearch_to_tsquery('simple', unaccent($%d)) OR a.title ILIKE $%d)
		GROUP BY a.id
		ORDER BY 4 DESC
		LIMIT $%d`, off+1, off+1, lf, off+1, off+2, off+3)
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
	rows, err := e.pool.Query(ctx, `
		SELECT id, name, ts_rank(search_vec, websearch_to_tsquery('simple', unaccent($1))) + similarity(name, $1)
		FROM artists
		WHERE search_vec @@ websearch_to_tsquery('simple', unaccent($1)) OR name ILIKE $2
		ORDER BY 3 DESC LIMIT $3`, text, "%"+text+"%", limit)
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
	rows, err := e.pool.Query(ctx, `
		SELECT id, name FROM playlists
		WHERE search_vec @@ websearch_to_tsquery('simple', unaccent($1)) OR name ILIKE $2
		ORDER BY similarity(name,$1) DESC LIMIT $3`, q.Text, "%"+q.Text+"%", limit)
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
