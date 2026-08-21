package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const listenArtistSQL = `coalesce((SELECT string_agg(ar.name, ', ' ORDER BY ta.position)
	FROM track_artists ta JOIN artists ar ON ar.id=ta.artist_id
	WHERE ta.track_id=t.id AND ta.role='primary'),'')`

var localListenSources = []string{"web", "discord"}

func listenQuerySources(r *http.Request, defaults []string) []string {
	raw := strings.TrimSpace(r.URL.Query().Get("sources"))
	if raw == "" {
		return defaults
	}
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(strings.ToLower(p))
		switch p {
		case "web", "discord", "import":
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	if len(out) == 0 {
		return defaults
	}
	return out
}

func includeImport(r *http.Request) bool {
	v := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("include_import")))
	return v == "1" || v == "true" || v == "yes"
}

func parseLimit(r *http.Request, def, max int) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func parsePeriod(r *http.Request) (name string, from, to time.Time) {
	now := time.Now().UTC()
	name = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("period")))
	if name == "" {
		name = "year"
	}
	switch name {
	case "week":
		from = now.AddDate(0, 0, -7)
		to = now.Add(time.Second)
	case "month":
		from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		to = now.Add(time.Second)
	case "year":
		from = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		to = now.Add(time.Second)
	case "all":
		from = time.Time{}
		to = now.Add(24 * time.Hour)
	default:
		name = "year"
		from = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		to = now.Add(time.Second)
	}
	return name, from, to
}

func parseWrappedWindow(r *http.Request) (year int, month int, from, to time.Time) {
	now := time.Now().UTC()
	year = now.Year()
	if y, err := strconv.Atoi(r.URL.Query().Get("year")); err == nil && y >= 2000 && y <= 2100 {
		year = y
	}
	month = 0
	if m, err := strconv.Atoi(r.URL.Query().Get("month")); err == nil && m >= 1 && m <= 12 {
		month = m
	}
	if month > 0 {
		from = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
		to = from.AddDate(0, 1, 0)
		return year, month, from, to
	}
	from = time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	to = from.AddDate(1, 0, 0)
	return year, 0, from, to
}

type listenTotals struct {
	Plays        int64 `json:"plays"`
	UniqueTracks int64 `json:"unique_tracks"`
	Minutes      int64 `json:"minutes"`
}

func queryListenTotals(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, sources []string, from, to time.Time) (listenTotals, error) {
	var t listenTotals
	var ms int64
	err := pool.QueryRow(ctx, `
		SELECT count(*), count(DISTINCT track_id), coalesce(sum(duration_ms), 0)
		FROM listen_history
		WHERE user_id=$1 AND source = ANY($2)
			AND ($3::timestamptz IS NULL OR played_at >= $3)
			AND played_at < $4`,
		userID, sources, nilTime(from), to).Scan(&t.Plays, &t.UniqueTracks, &ms)
	if err != nil {
		return t, err
	}
	t.Minutes = ms / 60000
	return t, nil
}

func nilTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// historyRecent is the dedicated recently-played page (joins track metadata).
// Imported rows are included so the UI can label them; recap totals live on listeningStats.
func (s *Server) historyRecent(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	limit := parseLimit(r, 200, 500)
	sources := listenQuerySources(r, []string{"web", "discord", "import"})
	rows, err := s.Pool.Query(r.Context(), `
		SELECT h.track_id, h.played_at, h.duration_ms, h.source,
			t.id, t.title, t.duration_ms, t.album_id, coalesce(al.title,''), `+listenArtistSQL+`
		FROM listen_history h
		JOIN tracks t ON t.id = h.track_id
		LEFT JOIN albums al ON al.id = t.album_id
		WHERE h.user_id=$1 AND h.source = ANY($2)
		ORDER BY h.played_at DESC
		LIMIT $3`, u.ID, sources, limit)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "track_id", "played_at", "listened_ms", "source", "id", "title", "duration_ms", "album_id", "album", "artist"))
}

func (s *Server) neverPlayed(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	libs := s.libraryIDs(r.Context(), u)
	limit := parseLimit(r, 200, 500)
	rows, err := s.Pool.Query(r.Context(), `
		SELECT t.id, t.title, t.duration_ms, t.album_id, coalesce(al.title,''), `+listenArtistSQL+`
		FROM tracks t
		LEFT JOIN albums al ON al.id = t.album_id
		LEFT JOIN play_counts pc ON pc.track_id = t.id AND pc.user_id = $1
		WHERE t.library_id = ANY($2)
			AND (pc.track_id IS NULL OR pc.count = 0)
		ORDER BY t.created_at DESC
		LIMIT $3`, u.ID, libs, limit)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "title", "duration_ms", "album_id", "album", "artist"))
}

func (s *Server) rediscovery(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	libs := s.libraryIDs(r.Context(), u)
	limit := parseLimit(r, 100, 500)
	days := 60
	if n, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && n >= 14 && n <= 365 {
		days = n
	}
	rows, err := s.Pool.Query(r.Context(), `
		SELECT t.id, t.title, t.duration_ms, t.album_id, coalesce(al.title,''), `+listenArtistSQL+`,
			pc.count, pc.skip_count, pc.last_played_at
		FROM play_counts pc
		JOIN tracks t ON t.id = pc.track_id
		LEFT JOIN albums al ON al.id = t.album_id
		WHERE pc.user_id=$1 AND pc.count > 0 AND t.library_id = ANY($2)
			AND (pc.last_played_at IS NULL OR pc.last_played_at < now() - make_interval(days => $3))
		ORDER BY pc.count DESC, pc.last_played_at ASC NULLS FIRST
		LIMIT $4`, u.ID, libs, days, limit)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "title", "duration_ms", "album_id", "album", "artist", "count", "skip_count", "last_played_at"))
}

func (s *Server) listeningStats(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	period, from, to := parsePeriod(r)
	mixImport := includeImport(r)
	sources := append([]string{}, localListenSources...)
	if mixImport {
		sources = append(sources, "import")
	}
	totals, err := queryListenTotals(r.Context(), s.Pool, u.ID, sources, from, to)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	imported, err := queryListenTotals(r.Context(), s.Pool, u.ID, []string{"import"}, from, to)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	var skips int64
	_ = s.Pool.QueryRow(r.Context(), `SELECT coalesce(sum(skip_count), 0) FROM play_counts WHERE user_id=$1`, u.ID).Scan(&skips)

	topN := parseLimit(r, 20, 50)
	topTracks := s.listenTopTracks(r.Context(), u.ID, sources, from, to, topN)
	topArtists := s.listenTopArtists(r.Context(), u.ID, sources, from, to, topN)
	topAlbums := s.listenTopAlbums(r.Context(), u.ID, sources, from, to, topN)
	bucket := "day"
	if period == "year" || period == "all" {
		bucket = "month"
	}
	trend := s.listenTrend(r.Context(), u.ID, sources, from, to, bucket)

	writeJSON(w, 200, map[string]any{
		"period":         period,
		"from":           nilTime(from),
		"to":             to,
		"sources":        sources,
		"include_import": mixImport,
		"totals": map[string]any{
			"plays":         totals.Plays,
			"unique_tracks": totals.UniqueTracks,
			"minutes":       totals.Minutes,
			"skips":         skips,
		},
		"imported": map[string]any{
			"plays":         imported.Plays,
			"unique_tracks": imported.UniqueTracks,
			"minutes":       imported.Minutes,
			"labelled":      true,
		},
		"top_tracks":  topTracks,
		"top_artists": topArtists,
		"top_albums":  topAlbums,
		"by_bucket":   trend,
	})
}

func (s *Server) wrapped(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	year, month, from, to := parseWrappedWindow(r)
	mixImport := includeImport(r)
	sources := append([]string{}, localListenSources...)
	if mixImport {
		sources = append(sources, "import")
	}
	totals, err := queryListenTotals(r.Context(), s.Pool, u.ID, sources, from, to)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	imported, err := queryListenTotals(r.Context(), s.Pool, u.ID, []string{"import"}, from, to)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}

	var uniqueArtists, uniqueAlbums int64
	_ = s.Pool.QueryRow(r.Context(), `
		SELECT count(DISTINCT ta.artist_id)
		FROM listen_history h
		JOIN track_artists ta ON ta.track_id = h.track_id AND ta.role = 'primary'
		WHERE h.user_id=$1 AND h.source = ANY($2) AND h.played_at >= $3 AND h.played_at < $4`,
		u.ID, sources, from, to).Scan(&uniqueArtists)
	_ = s.Pool.QueryRow(r.Context(), `
		SELECT count(DISTINCT t.album_id)
		FROM listen_history h
		JOIN tracks t ON t.id = h.track_id
		WHERE h.user_id=$1 AND h.source = ANY($2) AND h.played_at >= $3 AND h.played_at < $4
			AND t.album_id IS NOT NULL`,
		u.ID, sources, from, to).Scan(&uniqueAlbums)

	var skips int64
	_ = s.Pool.QueryRow(r.Context(), `
		SELECT coalesce(sum(pc.skip_count), 0)
		FROM play_counts pc
		WHERE pc.user_id=$1 AND pc.skip_count > 0
			AND EXISTS (
				SELECT 1 FROM listen_history h
				WHERE h.user_id=$1 AND h.track_id=pc.track_id
					AND h.source = ANY($2) AND h.played_at >= $3 AND h.played_at < $4
			)`, u.ID, sources, from, to).Scan(&skips)

	bucket := "month"
	if month > 0 {
		bucket = "day"
	}
	writeJSON(w, 200, map[string]any{
		"year":           year,
		"month":          month,
		"from":           from,
		"to":             to,
		"sources":        sources,
		"include_import": mixImport,
		"totals": map[string]any{
			"plays":          totals.Plays,
			"unique_tracks":  totals.UniqueTracks,
			"unique_artists": uniqueArtists,
			"unique_albums":  uniqueAlbums,
			"minutes":        totals.Minutes,
			"skips":          skips,
		},
		"imported": map[string]any{
			"plays":         imported.Plays,
			"unique_tracks": imported.UniqueTracks,
			"minutes":       imported.Minutes,
			"labelled":      true,
		},
		"top_tracks":   s.listenTopTracks(r.Context(), u.ID, sources, from, to, 10),
		"top_artists":  s.listenTopArtists(r.Context(), u.ID, sources, from, to, 10),
		"top_albums":   s.listenTopAlbums(r.Context(), u.ID, sources, from, to, 10),
		"top_genres":   s.listenTopGenres(r.Context(), u.ID, sources, from, to, 8),
		"most_skipped": s.listenMostSkipped(r.Context(), u.ID, sources, from, to, 10),
		"first_listen": s.listenFirst(r.Context(), u.ID, sources, from, to),
		"peak_day":     s.listenPeakDay(r.Context(), u.ID, sources, from, to),
		"by_bucket":    s.listenTrend(r.Context(), u.ID, sources, from, to, bucket),
	})
}

func (s *Server) listenTopTracks(ctx context.Context, userID uuid.UUID, sources []string, from, to time.Time, limit int) []map[string]any {
	rows, err := s.Pool.Query(ctx, `
		SELECT t.id, t.title, t.duration_ms, t.album_id, coalesce(al.title,''), `+listenArtistSQL+`,
			count(*) AS plays, coalesce(sum(h.duration_ms), 0) AS listened_ms
		FROM listen_history h
		JOIN tracks t ON t.id = h.track_id
		LEFT JOIN albums al ON al.id = t.album_id
		WHERE h.user_id=$1 AND h.source = ANY($2)
			AND ($3::timestamptz IS NULL OR h.played_at >= $3) AND h.played_at < $4
		GROUP BY t.id, t.title, t.duration_ms, t.album_id, al.title
		ORDER BY plays DESC
		LIMIT $5`, userID, sources, nilTime(from), to, limit)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	return scanMaps(rows, "id", "title", "duration_ms", "album_id", "album", "artist", "plays", "listened_ms")
}

func (s *Server) listenTopArtists(ctx context.Context, userID uuid.UUID, sources []string, from, to time.Time, limit int) []map[string]any {
	rows, err := s.Pool.Query(ctx, `
		SELECT ar.id, ar.name, count(*) AS plays, coalesce(sum(h.duration_ms), 0) AS listened_ms
		FROM listen_history h
		JOIN track_artists ta ON ta.track_id = h.track_id AND ta.role = 'primary'
		JOIN artists ar ON ar.id = ta.artist_id
		WHERE h.user_id=$1 AND h.source = ANY($2)
			AND ($3::timestamptz IS NULL OR h.played_at >= $3) AND h.played_at < $4
		GROUP BY ar.id, ar.name
		ORDER BY plays DESC
		LIMIT $5`, userID, sources, nilTime(from), to, limit)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	return scanMaps(rows, "id", "name", "plays", "listened_ms")
}

func (s *Server) listenTopAlbums(ctx context.Context, userID uuid.UUID, sources []string, from, to time.Time, limit int) []map[string]any {
	rows, err := s.Pool.Query(ctx, `
		SELECT al.id, al.title, count(*) AS plays, coalesce(sum(h.duration_ms), 0) AS listened_ms,
			coalesce((SELECT string_agg(ar.name, ', ' ORDER BY aa.position)
				FROM album_artists aa JOIN artists ar ON ar.id=aa.artist_id
				WHERE aa.album_id=al.id AND aa.role='album_artist'),'') AS artist
		FROM listen_history h
		JOIN tracks t ON t.id = h.track_id
		JOIN albums al ON al.id = t.album_id
		WHERE h.user_id=$1 AND h.source = ANY($2)
			AND ($3::timestamptz IS NULL OR h.played_at >= $3) AND h.played_at < $4
		GROUP BY al.id, al.title
		ORDER BY plays DESC
		LIMIT $5`, userID, sources, nilTime(from), to, limit)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	return scanMaps(rows, "id", "title", "plays", "listened_ms", "artist")
}

func (s *Server) listenTopGenres(ctx context.Context, userID uuid.UUID, sources []string, from, to time.Time, limit int) []map[string]any {
	rows, err := s.Pool.Query(ctx, `
		SELECT t.genre_text AS genre, count(*) AS plays
		FROM listen_history h
		JOIN tracks t ON t.id = h.track_id
		WHERE h.user_id=$1 AND h.source = ANY($2)
			AND ($3::timestamptz IS NULL OR h.played_at >= $3) AND h.played_at < $4
			AND t.genre_text <> ''
		GROUP BY t.genre_text
		ORDER BY plays DESC
		LIMIT $5`, userID, sources, nilTime(from), to, limit)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	return scanMaps(rows, "genre", "plays")
}

func (s *Server) listenMostSkipped(ctx context.Context, userID uuid.UUID, sources []string, from, to time.Time, limit int) []map[string]any {
	rows, err := s.Pool.Query(ctx, `
		SELECT t.id, t.title, t.duration_ms, t.album_id, coalesce(al.title,''), `+listenArtistSQL+`, pc.skip_count
		FROM play_counts pc
		JOIN tracks t ON t.id = pc.track_id
		LEFT JOIN albums al ON al.id = t.album_id
		WHERE pc.user_id=$1 AND pc.skip_count > 0
			AND EXISTS (
				SELECT 1 FROM listen_history h
				WHERE h.user_id=$1 AND h.track_id=pc.track_id
					AND h.source = ANY($2)
					AND ($3::timestamptz IS NULL OR h.played_at >= $3) AND h.played_at < $4
			)
		ORDER BY pc.skip_count DESC
		LIMIT $5`, userID, sources, nilTime(from), to, limit)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	return scanMaps(rows, "id", "title", "duration_ms", "album_id", "album", "artist", "skip_count")
}

func (s *Server) listenFirst(ctx context.Context, userID uuid.UUID, sources []string, from, to time.Time) map[string]any {
	rows, err := s.Pool.Query(ctx, `
		SELECT h.track_id, h.played_at, h.source, t.id, t.title, t.duration_ms, t.album_id,
			coalesce(al.title,''), `+listenArtistSQL+`
		FROM listen_history h
		JOIN tracks t ON t.id = h.track_id
		LEFT JOIN albums al ON al.id = t.album_id
		WHERE h.user_id=$1 AND h.source = ANY($2) AND h.played_at >= $3 AND h.played_at < $4
		ORDER BY h.played_at ASC
		LIMIT 1`, userID, sources, from, to)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := scanMaps(rows, "track_id", "played_at", "source", "id", "title", "duration_ms", "album_id", "album", "artist")
	if len(out) == 0 {
		return nil
	}
	return out[0]
}

func (s *Server) listenPeakDay(ctx context.Context, userID uuid.UUID, sources []string, from, to time.Time) map[string]any {
	var day time.Time
	var plays, ms int64
	err := s.Pool.QueryRow(ctx, `
		SELECT date_trunc('day', played_at) AS day, count(*) AS plays, coalesce(sum(duration_ms), 0)
		FROM listen_history
		WHERE user_id=$1 AND source = ANY($2)
			AND ($3::timestamptz IS NULL OR played_at >= $3) AND played_at < $4
		GROUP BY 1
		ORDER BY plays DESC, day ASC
		LIMIT 1`, userID, sources, nilTime(from), to).Scan(&day, &plays, &ms)
	if err != nil {
		return nil
	}
	return map[string]any{"day": day, "plays": plays, "minutes": ms / 60000}
}

func (s *Server) listenTrend(ctx context.Context, userID uuid.UUID, sources []string, from, to time.Time, bucket string) []map[string]any {
	trunc := "day"
	if bucket == "month" {
		trunc = "month"
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT date_trunc($5, played_at) AS bucket, count(*) AS plays, coalesce(sum(duration_ms), 0) AS listened_ms
		FROM listen_history
		WHERE user_id=$1 AND source = ANY($2)
			AND ($3::timestamptz IS NULL OR played_at >= $3) AND played_at < $4
		GROUP BY 1
		ORDER BY 1`, userID, sources, nilTime(from), to, trunc)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	return scanMaps(rows, "bucket", "plays", "listened_ms")
}
