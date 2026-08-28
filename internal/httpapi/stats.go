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
	Plays            int64  `json:"plays"`
	UniqueTracks     int64  `json:"unique_tracks"`
	Minutes          int64  `json:"minutes"`
	MinutesEstimated bool   `json:"minutes_estimated,omitempty"`
	MinutesSource    string `json:"minutes_source,omitempty"`
}

// listenEventsMinutesSource documents events minutes. Backfill rows have
// listened_ms NULL; coalesce to track_duration_ms is an estimate, not silence.
const listenEventsMinutesSource = "coalesce(listened_ms, track_duration_ms)/60000; backfill rows have listened_ms NULL so minutes are estimated from track_duration_ms"

type listenRead struct {
	table    string
	timeCol  string
	durExpr  string
	qualPred string
	events   bool
}

func listenReadFor(events bool) listenRead {
	if events {
		return listenRead{
			table:    "listen_events",
			timeCol:  "started_at",
			durExpr:  "coalesce(h.listened_ms, h.track_duration_ms)",
			qualPred: " AND h.kind = 'qualify'",
			events:   true,
		}
	}
	return listenRead{
		table:    "listen_history",
		timeCol:  "played_at",
		durExpr:  "h.duration_ms",
		qualPred: "",
		events:   false,
	}
}

func listenTotalsSQL(events bool) string {
	r := listenReadFor(events)
	return `SELECT count(*), count(DISTINCT h.track_id), coalesce(sum(` + r.durExpr + `), 0)
		FROM ` + r.table + ` h
		WHERE h.user_id=$1 AND h.source = ANY($2)` + r.qualPred + `
			AND ($3::timestamptz IS NULL OR h.` + r.timeCol + ` >= $3)
			AND h.` + r.timeCol + ` < $4`
}

func queryListenTotals(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, sources []string, from, to time.Time, events bool) (listenTotals, error) {
	var t listenTotals
	var ms int64
	err := pool.QueryRow(ctx, listenTotalsSQL(events),
		userID, sources, nilTime(from), to).Scan(&t.Plays, &t.UniqueTracks, &ms)
	if err != nil {
		return t, err
	}
	t.Minutes = ms / 60000
	if events {
		t.MinutesEstimated = true
		t.MinutesSource = listenEventsMinutesSource
	}
	return t, nil
}

func listenTotalsJSON(t listenTotals, extra map[string]any) map[string]any {
	m := map[string]any{
		"plays":         t.Plays,
		"unique_tracks": t.UniqueTracks,
		"minutes":       t.Minutes,
	}
	for k, v := range extra {
		m[k] = v
	}
	if t.MinutesEstimated {
		m["minutes_estimated"] = true
		m["minutes_source"] = t.MinutesSource
	}
	return m
}

// queryListenPlayCountsSeparate runs history and events counts independently.
// Callers must not add the two numbers; they are a dual-read compare only.
func queryListenPlayCountsSeparate(ctx context.Context, pool *pgxpool.Pool) (historyPlays, eventsQualified int64, err error) {
	if pool == nil {
		return 0, 0, nil
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM listen_history`).Scan(&historyPlays); err != nil {
		return 0, 0, err
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM listen_events WHERE kind = 'qualify'`).Scan(&eventsQualified); err != nil {
		return historyPlays, 0, err
	}
	return historyPlays, eventsQualified, nil
}

func homeContinueSQL(events bool) string {
	r := listenReadFor(events)
	return `SELECT id, title, duration_ms, album_id, album, artist FROM (
			SELECT DISTINCT ON (h.track_id) t.id, t.title, t.duration_ms, t.album_id, coalesce(al.title,'') AS album,
				` + listenArtistSQL + ` AS artist, h.` + r.timeCol + ` AS played_at
			FROM ` + r.table + ` h
			JOIN tracks t ON t.id=h.track_id
			LEFT JOIN albums al ON al.id=t.album_id
			WHERE h.user_id=$1` + r.qualPred + `
			ORDER BY h.track_id, h.` + r.timeCol + ` DESC
		) x ORDER BY played_at DESC LIMIT 15`
}

func historyListSQL(events bool, limit int) string {
	r := listenReadFor(events)
	lim := "200"
	if limit > 0 {
		lim = strconv.Itoa(limit)
	}
	return `SELECT h.track_id, h.` + r.timeCol + ` AS played_at, ` + r.durExpr + ` AS duration_ms, h.source
		FROM ` + r.table + ` h
		WHERE h.user_id=$1` + r.qualPred + `
		ORDER BY h.` + r.timeCol + ` DESC LIMIT ` + lim
}

func historyRecentSQL(events bool) string {
	r := listenReadFor(events)
	return `SELECT h.track_id, h.` + r.timeCol + ` AS played_at, ` + r.durExpr + ` AS listened_ms, h.source,
			t.id, t.title, t.duration_ms, t.album_id, coalesce(al.title,''), ` + listenArtistSQL + `
		FROM ` + r.table + ` h
		JOIN tracks t ON t.id = h.track_id
		LEFT JOIN albums al ON al.id = t.album_id
		WHERE h.user_id=$1 AND h.source = ANY($2)` + r.qualPred + `
		ORDER BY h.` + r.timeCol + ` DESC
		LIMIT $3`
}

func neverPlayedSQL() string {
	return `SELECT t.id, t.title, t.duration_ms, t.album_id, coalesce(al.title,''), ` + listenArtistSQL + `
		FROM tracks t
		LEFT JOIN albums al ON al.id = t.album_id
		LEFT JOIN play_counts pc ON pc.track_id = t.id AND pc.user_id = $1
		WHERE t.library_id = ANY($2)
			AND (pc.track_id IS NULL OR pc.count = 0)
		ORDER BY t.created_at DESC
		LIMIT $3`
}

func rediscoverySQL() string {
	return `SELECT t.id, t.title, t.duration_ms, t.album_id, coalesce(al.title,''), ` + listenArtistSQL + `,
			pc.count, pc.skip_count, pc.last_played_at
		FROM play_counts pc
		JOIN tracks t ON t.id = pc.track_id
		LEFT JOIN albums al ON al.id = t.album_id
		WHERE pc.user_id=$1 AND pc.count > 0 AND t.library_id = ANY($2)
			AND (pc.last_played_at IS NULL OR pc.last_played_at < now() - make_interval(days => $3))
		ORDER BY pc.count DESC, pc.last_played_at ASC NULLS FIRST
		LIMIT $4`
}

func listenTopTracksSQL(events bool) string {
	r := listenReadFor(events)
	return `SELECT t.id, t.title, t.duration_ms, t.album_id, coalesce(al.title,''), ` + listenArtistSQL + `,
			count(*) AS plays, coalesce(sum(` + r.durExpr + `), 0) AS listened_ms
		FROM ` + r.table + ` h
		JOIN tracks t ON t.id = h.track_id
		LEFT JOIN albums al ON al.id = t.album_id
		WHERE h.user_id=$1 AND h.source = ANY($2)` + r.qualPred + `
			AND ($3::timestamptz IS NULL OR h.` + r.timeCol + ` >= $3) AND h.` + r.timeCol + ` < $4
		GROUP BY t.id, t.title, t.duration_ms, t.album_id, al.title
		ORDER BY plays DESC
		LIMIT $5`
}

func listenTopArtistsSQL(events bool) string {
	r := listenReadFor(events)
	return `SELECT ar.id, ar.name, count(*) AS plays, coalesce(sum(` + r.durExpr + `), 0) AS listened_ms
		FROM ` + r.table + ` h
		JOIN track_artists ta ON ta.track_id = h.track_id AND ta.role = 'primary'
		JOIN artists ar ON ar.id = ta.artist_id
		WHERE h.user_id=$1 AND h.source = ANY($2)` + r.qualPred + `
			AND ($3::timestamptz IS NULL OR h.` + r.timeCol + ` >= $3) AND h.` + r.timeCol + ` < $4
		GROUP BY ar.id, ar.name
		ORDER BY plays DESC
		LIMIT $5`
}

func listenTopAlbumsSQL(events bool) string {
	r := listenReadFor(events)
	return `SELECT al.id, al.title, count(*) AS plays, coalesce(sum(` + r.durExpr + `), 0) AS listened_ms,
			coalesce((SELECT string_agg(ar.name, ', ' ORDER BY aa.position)
				FROM album_artists aa JOIN artists ar ON ar.id=aa.artist_id
				WHERE aa.album_id=al.id AND aa.role='album_artist'),'') AS artist
		FROM ` + r.table + ` h
		JOIN tracks t ON t.id = h.track_id
		JOIN albums al ON al.id = t.album_id
		WHERE h.user_id=$1 AND h.source = ANY($2)` + r.qualPred + `
			AND ($3::timestamptz IS NULL OR h.` + r.timeCol + ` >= $3) AND h.` + r.timeCol + ` < $4
		GROUP BY al.id, al.title
		ORDER BY plays DESC
		LIMIT $5`
}

func listenTopGenresSQL(events bool) string {
	r := listenReadFor(events)
	return `SELECT t.genre_text AS genre, count(*) AS plays
		FROM ` + r.table + ` h
		JOIN tracks t ON t.id = h.track_id
		WHERE h.user_id=$1 AND h.source = ANY($2)` + r.qualPred + `
			AND ($3::timestamptz IS NULL OR h.` + r.timeCol + ` >= $3) AND h.` + r.timeCol + ` < $4
			AND t.genre_text <> ''
		GROUP BY t.genre_text
		ORDER BY plays DESC
		LIMIT $5`
}

func listenMostSkippedSQL(events bool) string {
	r := listenReadFor(events)
	return `SELECT t.id, t.title, t.duration_ms, t.album_id, coalesce(al.title,''), ` + listenArtistSQL + `, pc.skip_count
		FROM play_counts pc
		JOIN tracks t ON t.id = pc.track_id
		LEFT JOIN albums al ON al.id = t.album_id
		WHERE pc.user_id=$1 AND pc.skip_count > 0
			AND EXISTS (
				SELECT 1 FROM ` + r.table + ` h
				WHERE h.user_id=$1 AND h.track_id=pc.track_id
					AND h.source = ANY($2)` + r.qualPred + `
					AND ($3::timestamptz IS NULL OR h.` + r.timeCol + ` >= $3) AND h.` + r.timeCol + ` < $4
			)
		ORDER BY pc.skip_count DESC
		LIMIT $5`
}

func listenFirstSQL(events bool) string {
	r := listenReadFor(events)
	return `SELECT h.track_id, h.` + r.timeCol + ` AS played_at, h.source, t.id, t.title, t.duration_ms, t.album_id,
			coalesce(al.title,''), ` + listenArtistSQL + `
		FROM ` + r.table + ` h
		JOIN tracks t ON t.id = h.track_id
		LEFT JOIN albums al ON al.id = t.album_id
		WHERE h.user_id=$1 AND h.source = ANY($2)` + r.qualPred + `
			AND h.` + r.timeCol + ` >= $3 AND h.` + r.timeCol + ` < $4
		ORDER BY h.` + r.timeCol + ` ASC
		LIMIT 1`
}

func listenPeakDaySQL(events bool) string {
	r := listenReadFor(events)
	return `SELECT date_trunc('day', h.` + r.timeCol + `) AS day, count(*) AS plays, coalesce(sum(` + r.durExpr + `), 0)
		FROM ` + r.table + ` h
		WHERE h.user_id=$1 AND h.source = ANY($2)` + r.qualPred + `
			AND ($3::timestamptz IS NULL OR h.` + r.timeCol + ` >= $3) AND h.` + r.timeCol + ` < $4
		GROUP BY 1
		ORDER BY plays DESC, day ASC
		LIMIT 1`
}

func listenTrendSQL(events bool) string {
	r := listenReadFor(events)
	return `SELECT date_trunc($5, h.` + r.timeCol + `) AS bucket, count(*) AS plays, coalesce(sum(` + r.durExpr + `), 0) AS listened_ms
		FROM ` + r.table + ` h
		WHERE h.user_id=$1 AND h.source = ANY($2)` + r.qualPred + `
			AND ($3::timestamptz IS NULL OR h.` + r.timeCol + ` >= $3) AND h.` + r.timeCol + ` < $4
		GROUP BY 1
		ORDER BY 1`
}

func listenUniqueArtistsSQL(events bool) string {
	r := listenReadFor(events)
	return `SELECT count(DISTINCT ta.artist_id)
		FROM ` + r.table + ` h
		JOIN track_artists ta ON ta.track_id = h.track_id AND ta.role = 'primary'
		WHERE h.user_id=$1 AND h.source = ANY($2)` + r.qualPred + `
			AND h.` + r.timeCol + ` >= $3 AND h.` + r.timeCol + ` < $4`
}

func listenUniqueAlbumsSQL(events bool) string {
	r := listenReadFor(events)
	return `SELECT count(DISTINCT t.album_id)
		FROM ` + r.table + ` h
		JOIN tracks t ON t.id = h.track_id
		WHERE h.user_id=$1 AND h.source = ANY($2)` + r.qualPred + `
			AND h.` + r.timeCol + ` >= $3 AND h.` + r.timeCol + ` < $4
			AND t.album_id IS NOT NULL`
}

func listenWrappedSkipsSQL(events bool) string {
	r := listenReadFor(events)
	return `SELECT coalesce(sum(pc.skip_count), 0)
		FROM play_counts pc
		WHERE pc.user_id=$1 AND pc.skip_count > 0
			AND EXISTS (
				SELECT 1 FROM ` + r.table + ` h
				WHERE h.user_id=$1 AND h.track_id=pc.track_id
					AND h.source = ANY($2)` + r.qualPred + `
					AND h.` + r.timeCol + ` >= $3 AND h.` + r.timeCol + ` < $4
			)`
}

func recapReaderSQLs(events bool) []string {
	return []string{
		homeContinueSQL(events),
		historyListSQL(events, 200),
		historyListSQL(events, 5000),
		historyRecentSQL(events),
		listenTotalsSQL(events),
		listenTopTracksSQL(events),
		listenTopArtistsSQL(events),
		listenTopAlbumsSQL(events),
		listenTopGenresSQL(events),
		listenMostSkippedSQL(events),
		listenFirstSQL(events),
		listenPeakDaySQL(events),
		listenTrendSQL(events),
		listenUniqueArtistsSQL(events),
		listenUniqueAlbumsSQL(events),
		listenWrappedSkipsSQL(events),
	}
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
	events := s.listenReaderEvents(r.Context())
	rows, err := s.Pool.Query(r.Context(), historyRecentSQL(events), u.ID, sources, limit)
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
	rows, err := s.Pool.Query(r.Context(), neverPlayedSQL(), u.ID, libs, limit)
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
	rows, err := s.Pool.Query(r.Context(), rediscoverySQL(), u.ID, libs, days, limit)
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
	events := s.listenReaderEvents(r.Context())
	totals, err := queryListenTotals(r.Context(), s.Pool, u.ID, sources, from, to, events)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	imported, err := queryListenTotals(r.Context(), s.Pool, u.ID, []string{"import"}, from, to, events)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	var skips int64
	_ = s.Pool.QueryRow(r.Context(), `SELECT coalesce(sum(skip_count), 0) FROM play_counts WHERE user_id=$1`, u.ID).Scan(&skips)

	topN := parseLimit(r, 20, 50)
	topTracks := s.listenTopTracks(r.Context(), u.ID, sources, from, to, topN, events)
	topArtists := s.listenTopArtists(r.Context(), u.ID, sources, from, to, topN, events)
	topAlbums := s.listenTopAlbums(r.Context(), u.ID, sources, from, to, topN, events)
	bucket := "day"
	if period == "year" || period == "all" {
		bucket = "month"
	}
	trend := s.listenTrend(r.Context(), u.ID, sources, from, to, bucket, events)

	writeJSON(w, 200, map[string]any{
		"period":         period,
		"from":           nilTime(from),
		"to":             to,
		"sources":        sources,
		"include_import": mixImport,
		"totals": listenTotalsJSON(totals, map[string]any{
			"skips": skips,
		}),
		"imported": listenTotalsJSON(imported, map[string]any{
			"labelled": true,
		}),
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
	events := s.listenReaderEvents(r.Context())
	totals, err := queryListenTotals(r.Context(), s.Pool, u.ID, sources, from, to, events)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	imported, err := queryListenTotals(r.Context(), s.Pool, u.ID, []string{"import"}, from, to, events)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}

	var uniqueArtists, uniqueAlbums int64
	_ = s.Pool.QueryRow(r.Context(), listenUniqueArtistsSQL(events), u.ID, sources, from, to).Scan(&uniqueArtists)
	_ = s.Pool.QueryRow(r.Context(), listenUniqueAlbumsSQL(events), u.ID, sources, from, to).Scan(&uniqueAlbums)

	var skips int64
	_ = s.Pool.QueryRow(r.Context(), listenWrappedSkipsSQL(events), u.ID, sources, from, to).Scan(&skips)

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
		"totals": listenTotalsJSON(totals, map[string]any{
			"unique_artists": uniqueArtists,
			"unique_albums":  uniqueAlbums,
			"skips":          skips,
		}),
		"imported": listenTotalsJSON(imported, map[string]any{
			"labelled": true,
		}),
		"top_tracks":   s.listenTopTracks(r.Context(), u.ID, sources, from, to, 10, events),
		"top_artists":  s.listenTopArtists(r.Context(), u.ID, sources, from, to, 10, events),
		"top_albums":   s.listenTopAlbums(r.Context(), u.ID, sources, from, to, 10, events),
		"top_genres":   s.listenTopGenres(r.Context(), u.ID, sources, from, to, 8, events),
		"most_skipped": s.listenMostSkipped(r.Context(), u.ID, sources, from, to, 10, events),
		"first_listen": s.listenFirst(r.Context(), u.ID, sources, from, to, events),
		"peak_day":     s.listenPeakDay(r.Context(), u.ID, sources, from, to, events),
		"by_bucket":    s.listenTrend(r.Context(), u.ID, sources, from, to, bucket, events),
	})
}

func (s *Server) listenTopTracks(ctx context.Context, userID uuid.UUID, sources []string, from, to time.Time, limit int, events bool) []map[string]any {
	rows, err := s.Pool.Query(ctx, listenTopTracksSQL(events), userID, sources, nilTime(from), to, limit)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	return scanMaps(rows, "id", "title", "duration_ms", "album_id", "album", "artist", "plays", "listened_ms")
}

func (s *Server) listenTopArtists(ctx context.Context, userID uuid.UUID, sources []string, from, to time.Time, limit int, events bool) []map[string]any {
	rows, err := s.Pool.Query(ctx, listenTopArtistsSQL(events), userID, sources, nilTime(from), to, limit)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	return scanMaps(rows, "id", "name", "plays", "listened_ms")
}

func (s *Server) listenTopAlbums(ctx context.Context, userID uuid.UUID, sources []string, from, to time.Time, limit int, events bool) []map[string]any {
	rows, err := s.Pool.Query(ctx, listenTopAlbumsSQL(events), userID, sources, nilTime(from), to, limit)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	return scanMaps(rows, "id", "title", "plays", "listened_ms", "artist")
}

func (s *Server) listenTopGenres(ctx context.Context, userID uuid.UUID, sources []string, from, to time.Time, limit int, events bool) []map[string]any {
	rows, err := s.Pool.Query(ctx, listenTopGenresSQL(events), userID, sources, nilTime(from), to, limit)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	return scanMaps(rows, "genre", "plays")
}

func (s *Server) listenMostSkipped(ctx context.Context, userID uuid.UUID, sources []string, from, to time.Time, limit int, events bool) []map[string]any {
	rows, err := s.Pool.Query(ctx, listenMostSkippedSQL(events), userID, sources, nilTime(from), to, limit)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	return scanMaps(rows, "id", "title", "duration_ms", "album_id", "album", "artist", "skip_count")
}

func (s *Server) listenFirst(ctx context.Context, userID uuid.UUID, sources []string, from, to time.Time, events bool) map[string]any {
	rows, err := s.Pool.Query(ctx, listenFirstSQL(events), userID, sources, from, to)
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

func (s *Server) listenPeakDay(ctx context.Context, userID uuid.UUID, sources []string, from, to time.Time, events bool) map[string]any {
	var day time.Time
	var plays, ms int64
	err := s.Pool.QueryRow(ctx, listenPeakDaySQL(events), userID, sources, nilTime(from), to).Scan(&day, &plays, &ms)
	if err != nil {
		return nil
	}
	return map[string]any{"day": day, "plays": plays, "minutes": ms / 60000}
}

func (s *Server) listenTrend(ctx context.Context, userID uuid.UUID, sources []string, from, to time.Time, bucket string, events bool) []map[string]any {
	trunc := "day"
	if bucket == "month" {
		trunc = "month"
	}
	rows, err := s.Pool.Query(ctx, listenTrendSQL(events), userID, sources, nilTime(from), to, trunc)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	return scanMaps(rows, "bucket", "plays", "listened_ms")
}
