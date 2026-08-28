package radio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/matcher"
	"github.com/sounddock/sounddock/internal/scapex"
)

var (
	ErrUnknownKind = errors.New("unknown radio kind")
	ErrSeed        = errors.New("seed_id required")
	ErrDecade      = errors.New("decade required")
)

// Kinds matches contracts/radio.json.
var Kinds = []string{"library", "artist", "album", "track", "genre", "decade", "quick_mix"}

type Service struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

type Request struct {
	Kind    string
	SeedID  uuid.UUID
	Genre   string
	Decade  int
	Limit   int
	UserID  uuid.UUID
	Libs    []uuid.UUID
	AllLibs bool
	Exclude []uuid.UUID `json:"exclude,omitempty"`
	Recent  int         `json:"recent,omitempty"`
}

type Result struct {
	Kind       string      `json:"kind"`
	SeedID     uuid.UUID   `json:"seed_id"`
	TrackIDs   []uuid.UUID  `json:"track_ids"`
	YoutubeIDs []string     `json:"youtube_ids,omitempty"`
	Hits       []scapex.Hit `json:"hits,omitempty"`
}

type RefreshPayload struct {
	Kind   string    `json:"kind"`
	SeedID uuid.UUID `json:"seed_id"`
	Limit  int       `json:"limit"`
	UserID uuid.UUID `json:"user_id,omitempty"`
	Decade int       `json:"decade,omitempty"`
}

type SmartPayload struct {
	PlaylistID uuid.UUID `json:"playlist_id"`
}

func ClampLimit(n int) int {
	if n <= 0 {
		return 20
	}
	if n > 100 {
		return 100
	}
	return n
}

// ClampFill is the YouTube autoplay pull size.
func ClampFill(n int) int {
	if n <= 0 {
		return 6
	}
	if n > 10 {
		return 10
	}
	return n
}

func ValidKind(kind string) bool {
	k := strings.ToLower(strings.TrimSpace(kind))
	for _, x := range Kinds {
		if x == k {
			return true
		}
	}
	return false
}

func DecadeStart(year int) int {
	if year <= 0 {
		return 0
	}
	return (year / 10) * 10
}

func ParseDecade(s string) int {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(strings.ToLower(s), "s")
	n, err := strconv.Atoi(s)
	if err != nil || n < 1000 || n > 2100 {
		return 0
	}
	return DecadeStart(n)
}

func (s *Service) Select(ctx context.Context, req Request) (Result, error) {
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if !ValidKind(kind) {
		return Result{}, ErrUnknownKind
	}
	limit := ClampLimit(req.Limit)
	libs := req.Libs
	if len(libs) == 0 && req.AllLibs {
		libs = s.allLibraries(ctx)
	}
	if len(libs) == 0 {
		libs = []uuid.UUID{uuid.Nil}
	}
	out := Result{Kind: kind, SeedID: req.SeedID, TrackIDs: []uuid.UUID{}}
	var ids []uuid.UUID
	var err error
	switch kind {
	case "library":
		if req.SeedID == uuid.Nil {
			return Result{}, ErrSeed
		}
		ids, err = s.queryIDs(ctx, `
			SELECT t.id FROM tracks t
			WHERE t.library_id=$1 AND t.library_id = ANY($2)
			ORDER BY random() LIMIT $3`, req.SeedID, libs, limit)
	case "artist":
		if req.SeedID == uuid.Nil {
			return Result{}, ErrSeed
		}
		ids, err = s.artistRadio(ctx, req.SeedID, libs, limit)
	case "album":
		if req.SeedID == uuid.Nil {
			return Result{}, ErrSeed
		}
		ids, err = s.albumRadio(ctx, req.SeedID, libs, limit)
	case "track":
		if req.SeedID == uuid.Nil {
			return Result{}, ErrSeed
		}
		recent := s.recentListenIDs(ctx, req.UserID, ClampRecent(req.Recent))
		ids, err = s.trackRadio(ctx, req.SeedID, libs, limit, req.Exclude, recent)
	case "genre":
		ids, err = s.genreRadio(ctx, req.SeedID, req.Genre, libs, limit)
	case "decade":
		d := req.Decade
		if d == 0 {
			return Result{}, ErrDecade
		}
		d = DecadeStart(d)
		ids, err = s.queryIDs(ctx, `
			SELECT t.id FROM tracks t
			WHERE t.library_id = ANY($1) AND t.year >= $2 AND t.year < $3
			ORDER BY random() LIMIT $4`, libs, d, d+10, limit)
	case "quick_mix":
		ids, err = s.quickMix(ctx, req.UserID, libs, limit)
	}
	if err != nil {
		return Result{}, err
	}
	ids = subtractIDs(ids, req.Exclude)
	out.TrackIDs = idsOrEmpty(ids)
	return out, nil
}

func (s *Service) artistRadio(ctx context.Context, artistID uuid.UUID, libs []uuid.UUID, limit int) ([]uuid.UUID, error) {
	ids, err := s.queryIDs(ctx, `
		SELECT t.id FROM tracks t
		JOIN track_artists ta ON ta.track_id=t.id
		WHERE ta.artist_id=$1 AND t.library_id = ANY($2)
		ORDER BY random() LIMIT $3`, artistID, libs, limit)
	if err != nil || len(ids) >= limit {
		return ids, err
	}
	more, err := s.queryIDs(ctx, `
		SELECT t.id FROM tracks t
		JOIN track_genres tg ON tg.track_id=t.id
		WHERE t.library_id = ANY($1)
		  AND tg.genre_id IN (
			SELECT tg2.genre_id FROM track_genres tg2
			JOIN track_artists ta ON ta.track_id=tg2.track_id
			WHERE ta.artist_id=$2
		  )
		  AND t.id <> ALL($3)
		ORDER BY random() LIMIT $4`, libs, artistID, idsOrDummy(ids), limit-len(ids))
	if err != nil {
		return ids, err
	}
	return uniqueAppend(ids, more, limit), nil
}

func (s *Service) albumRadio(ctx context.Context, albumID uuid.UUID, libs []uuid.UUID, limit int) ([]uuid.UUID, error) {
	ids, err := s.queryIDs(ctx, `
		SELECT t.id FROM tracks t
		WHERE t.album_id=$1 AND t.library_id = ANY($2)
		ORDER BY t.disc_number, t.track_number, random() LIMIT $3`, albumID, libs, limit)
	if err != nil || len(ids) >= limit {
		return ids, err
	}
	more, err := s.queryIDs(ctx, `
		SELECT t.id FROM tracks t
		JOIN track_artists ta ON ta.track_id=t.id
		WHERE t.library_id = ANY($1)
		  AND ta.artist_id IN (SELECT artist_id FROM album_artists WHERE album_id=$2)
		  AND t.id <> ALL($3)
		ORDER BY random() LIMIT $4`, libs, albumID, idsOrDummy(ids), limit-len(ids))
	if err != nil {
		return ids, err
	}
	return uniqueAppend(ids, more, limit), nil
}

func ClampRecent(n int) int {
	if n <= 0 {
		return 40
	}
	if n > 200 {
		return 200
	}
	return n
}

func (s *Service) recentListenIDs(ctx context.Context, userID uuid.UUID, n int) []uuid.UUID {
	if userID == uuid.Nil || n <= 0 {
		return nil
	}
	ids, err := s.queryIDs(ctx, `
		SELECT track_id FROM listen_history
		WHERE user_id=$1
		ORDER BY played_at DESC
		LIMIT $2`, userID, n)
	if err != nil {
		return nil
	}
	return ids
}

func (s *Service) trackRadio(ctx context.Context, trackID uuid.UUID, libs []uuid.UUID, limit int, queue, recent []uuid.UUID) ([]uuid.UUID, error) {
	if _, err := s.TrackMeta(ctx, trackID); err != nil {
		return nil, err
	}
	seeds := uniqueAppend([]uuid.UUID{trackID}, recent, 6)
	queueSkip := uniqueAppend([]uuid.UUID{trackID}, queue, len(queue)+1)
	strict := uniqueAppend(append([]uuid.UUID{}, queueSkip...), recent, len(queueSkip)+len(recent))

	ids, err := s.fillFromSeeds(ctx, seeds, libs, limit, strict, true)
	if err != nil {
		return nil, err
	}
	need := limit / 2
	if need < 3 {
		need = 3
	}
	if len(ids) < need {
		ids, err = s.fillFromSeeds(ctx, seeds, libs, limit, queueSkip, true)
		if err != nil {
			return ids, err
		}
	}
	if len(ids) < 3 {
		ids, err = s.fillFromSeeds(ctx, seeds, libs, limit, queueSkip, false)
	}
	return ids, err
}

func (s *Service) fillFromSeeds(ctx context.Context, seeds []uuid.UUID, libs []uuid.UUID, limit int, skip []uuid.UUID, dropSame bool) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	blocked := append([]uuid.UUID{}, skip...)
	for _, seed := range seeds {
		if len(ids) >= limit {
			break
		}
		more, err := s.similarFromSeed(ctx, seed, libs, limit-len(ids), uniqueAppend(blocked, ids, 4096))
		if err != nil {
			return ids, err
		}
		if dropSame {
			meta, merr := s.TrackMeta(ctx, seed)
			if merr == nil {
				more, _ = s.dropSameSongs(ctx, meta.Title, more)
			}
		}
		ids = uniqueAppend(ids, more, limit)
	}
	if dropSame {
		ids, _ = s.dedupeSameSongs(ctx, ids)
	}
	return ids, nil
}

func (s *Service) similarFromSeed(ctx context.Context, trackID uuid.UUID, libs []uuid.UUID, limit int, skip []uuid.UUID) ([]uuid.UUID, error) {
	meta, err := s.TrackMeta(ctx, trackID)
	if err != nil {
		return nil, err
	}
	blocked := idsOrDummy(uniqueAppend([]uuid.UUID{trackID}, skip, len(skip)+1))
	ids, err := s.queryIDs(ctx, `
		SELECT t.id FROM tracks t
		JOIN track_artists ta ON ta.track_id=t.id
		WHERE t.library_id = ANY($1) AND t.id <> ALL($2)
		  AND ta.artist_id IN (SELECT artist_id FROM track_artists WHERE track_id=$3 AND role='primary')
		ORDER BY random() LIMIT $4`, libs, blocked, trackID, limit)
	if err != nil {
		return nil, err
	}
	if len(ids) < limit && meta.AlbumID != nil {
		more, err := s.queryIDs(ctx, `
			SELECT t.id FROM tracks t
			WHERE t.album_id=$1 AND t.library_id = ANY($2) AND t.id <> ALL($3)
			ORDER BY random() LIMIT $4`, *meta.AlbumID, libs, idsOrDummy(append(blocked, ids...)), limit-len(ids))
		if err != nil {
			return ids, err
		}
		ids = uniqueAppend(ids, more, limit)
	}
	if len(ids) < limit {
		more, err := s.queryIDs(ctx, `
			SELECT t.id FROM tracks t
			JOIN track_genres tg ON tg.track_id=t.id
			WHERE t.library_id = ANY($1) AND t.id <> ALL($2)
			  AND tg.genre_id IN (SELECT genre_id FROM track_genres WHERE track_id=$3)
			ORDER BY random() LIMIT $4`, libs, idsOrDummy(append(blocked, ids...)), trackID, limit-len(ids))
		if err != nil {
			return ids, err
		}
		ids = uniqueAppend(ids, more, limit)
	}
	genre := FirstGenre(meta.Genre)
	if len(ids) < limit && genre != "" {
		more, err := s.queryIDs(ctx, `
			SELECT t.id FROM tracks t
			WHERE t.library_id = ANY($1) AND t.id <> ALL($2)
			  AND (t.genre_text ILIKE $3 OR EXISTS (
				SELECT 1 FROM track_genres tg JOIN genres g ON g.id=tg.genre_id
				WHERE tg.track_id=t.id AND g.name ILIKE $3
			  ))
			ORDER BY random() LIMIT $4`, libs, idsOrDummy(append(blocked, ids...)), genre, limit-len(ids))
		if err != nil {
			return ids, err
		}
		ids = uniqueAppend(ids, more, limit)
	}
	return ids, nil
}

func (s *Service) dropSameSongs(ctx context.Context, seedTitle string, ids []uuid.UUID) ([]uuid.UUID, error) {
	key := matcher.NormaliseTitle(seedTitle)
	if key == "" || len(ids) == 0 {
		return ids, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id, title FROM tracks WHERE id = ANY($1)`, ids)
	if err != nil {
		return ids, err
	}
	defer rows.Close()
	drop := map[uuid.UUID]struct{}{}
	for rows.Next() {
		var id uuid.UUID
		var title string
		if err := rows.Scan(&id, &title); err != nil {
			continue
		}
		if matcher.NormaliseTitle(title) == key {
			drop[id] = struct{}{}
		}
	}
	var out []uuid.UUID
	for _, id := range ids {
		if _, ok := drop[id]; ok {
			continue
		}
		out = append(out, id)
	}
	return out, nil
}

func (s *Service) dedupeSameSongs(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return ids, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id, title FROM tracks WHERE id = ANY($1)`, ids)
	if err != nil {
		return ids, err
	}
	defer rows.Close()
	titleOf := map[uuid.UUID]string{}
	for rows.Next() {
		var id uuid.UUID
		var title string
		if err := rows.Scan(&id, &title); err != nil {
			continue
		}
		titleOf[id] = title
	}
	seen := map[string]struct{}{}
	var out []uuid.UUID
	for _, id := range ids {
		key := matcher.NormaliseTitle(titleOf[id])
		if key != "" {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
		}
		out = append(out, id)
	}
	return out, nil
}

type TrackMeta struct {
	Title   string
	Artist  string
	Genre   string
	Year    *int
	AlbumID *uuid.UUID
}

func (s *Service) TrackMeta(ctx context.Context, id uuid.UUID) (TrackMeta, error) {
	var m TrackMeta
	err := s.pool.QueryRow(ctx, `
		SELECT t.title, coalesce(t.genre_text,''), t.year, t.album_id,
		       coalesce((SELECT string_agg(ar.name, ', ' ORDER BY ta.position)
		         FROM track_artists ta JOIN artists ar ON ar.id=ta.artist_id
		         WHERE ta.track_id=t.id AND ta.role='primary'),'')
		FROM tracks t WHERE t.id=$1`, id).Scan(&m.Title, &m.Genre, &m.Year, &m.AlbumID, &m.Artist)
	if err != nil {
		return m, err
	}
	if strings.TrimSpace(m.Genre) == "" {
		_ = s.pool.QueryRow(ctx, `
			SELECT coalesce(g.name,'') FROM track_genres tg
			JOIN genres g ON g.id=tg.genre_id
			WHERE tg.track_id=$1 LIMIT 1`, id).Scan(&m.Genre)
	}
	return m, nil
}

func FirstGenre(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, ",;/|"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func SimilarQuery(title, artist, genre string) string {
	title = strings.TrimSpace(title)
	artist = strings.TrimSpace(artist)
	genre = FirstGenre(genre)
	switch {
	case artist != "" && genre != "":
		return artist + " " + genre + " songs"
	case artist != "":
		return artist + " songs"
	case genre != "":
		return genre + " mix"
	default:
		return title
	}
}

func SameSong(a, b string) bool {
	na, nb := matcher.NormaliseTitle(a), matcher.NormaliseTitle(b)
	return na != "" && na == nb
}

func (s *Service) genreRadio(ctx context.Context, seed uuid.UUID, name string, libs []uuid.UUID, limit int) ([]uuid.UUID, error) {
	name = strings.TrimSpace(name)
	if seed == uuid.Nil && name == "" {
		return nil, ErrSeed
	}
	if seed != uuid.Nil {
		ids, err := s.queryIDs(ctx, `
			SELECT t.id FROM tracks t
			JOIN track_genres tg ON tg.track_id=t.id
			WHERE tg.genre_id=$1 AND t.library_id = ANY($2)
			ORDER BY random() LIMIT $3`, seed, libs, limit)
		if err != nil || len(ids) > 0 {
			return ids, err
		}
		_ = s.pool.QueryRow(ctx, `SELECT name FROM genres WHERE id=$1`, seed).Scan(&name)
	}
	if name == "" {
		return []uuid.UUID{}, nil
	}
	return s.queryIDs(ctx, `
		SELECT t.id FROM tracks t
		WHERE t.library_id = ANY($1)
		  AND (t.genre_text ILIKE $2 OR EXISTS (
			SELECT 1 FROM track_genres tg JOIN genres g ON g.id=tg.genre_id
			WHERE tg.track_id=t.id AND g.name ILIKE $2
		  ))
		ORDER BY random() LIMIT $3`, libs, name, limit)
}

func (s *Service) quickMix(ctx context.Context, userID uuid.UUID, libs []uuid.UUID, limit int) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	if userID != uuid.Nil {
		fav, err := s.queryIDs(ctx, `
			SELECT t.id FROM favourites f
			JOIN tracks t ON t.id=f.entity_id
			WHERE f.user_id=$1 AND f.entity_type='track' AND t.library_id = ANY($2)
			ORDER BY random() LIMIT $3`, userID, libs, limit)
		if err != nil {
			return nil, err
		}
		ids = uniqueAppend(ids, fav, limit)
		if len(ids) < limit {
			played, err := s.queryIDs(ctx, `
				SELECT t.id FROM play_counts pc
				JOIN tracks t ON t.id=pc.track_id
				WHERE pc.user_id=$1 AND t.library_id = ANY($2) AND t.id <> ALL($3)
				ORDER BY pc.count DESC, random() LIMIT $4`, userID, libs, idsOrDummy(ids), limit-len(ids))
			if err != nil {
				return ids, err
			}
			ids = uniqueAppend(ids, played, limit)
		}
	}
	if len(ids) < limit {
		recent, err := s.queryIDs(ctx, `
			SELECT t.id FROM tracks t
			WHERE t.library_id = ANY($1) AND t.id <> ALL($2)
			ORDER BY t.created_at DESC, random() LIMIT $3`, libs, idsOrDummy(ids), limit-len(ids))
		if err != nil {
			return ids, err
		}
		ids = uniqueAppend(ids, recent, limit)
	}
	if len(ids) < limit {
		more, err := s.queryIDs(ctx, `
			SELECT t.id FROM tracks t
			WHERE t.library_id = ANY($1) AND t.id <> ALL($2)
			ORDER BY random() LIMIT $3`, libs, idsOrDummy(ids), limit-len(ids))
		if err != nil {
			return ids, err
		}
		ids = uniqueAppend(ids, more, limit)
	}
	return ids, nil
}

func (s *Service) Seeds(ctx context.Context, libs []uuid.UUID) (map[string]any, error) {
	if len(libs) == 0 {
		libs = []uuid.UUID{uuid.Nil}
	}
	libRows, err := s.pool.Query(ctx, `SELECT id, name FROM libraries WHERE id = ANY($1) ORDER BY name`, libs)
	if err != nil {
		return nil, err
	}
	defer libRows.Close()
	libraries := scanPairs(libRows)
	gRows, err := s.pool.Query(ctx, `
		SELECT g.id::text, g.name FROM genres g
		WHERE EXISTS (SELECT 1 FROM track_genres tg JOIN tracks t ON t.id=tg.track_id WHERE tg.genre_id=g.id AND t.library_id = ANY($1))
		UNION
		SELECT '', genre_text FROM tracks WHERE library_id = ANY($1) AND genre_text<>''
		ORDER BY 2`, libs)
	if err != nil {
		return nil, err
	}
	defer gRows.Close()
	genres := scanPairs(gRows)
	dRows, err := s.pool.Query(ctx, `
		SELECT DISTINCT (year/10)*10 AS decade FROM tracks
		WHERE library_id = ANY($1) AND year IS NOT NULL
		ORDER BY 1`, libs)
	if err != nil {
		return nil, err
	}
	defer dRows.Close()
	var decades []int
	for dRows.Next() {
		var d int
		if err := dRows.Scan(&d); err == nil {
			decades = append(decades, d)
		}
	}
	if decades == nil {
		decades = []int{}
	}
	return map[string]any{
		"kinds":     Kinds,
		"libraries": libraries,
		"genres":    genres,
		"decades":   decades,
	}, nil
}

func (s *Service) queryIDs(ctx context.Context, q string, args ...any) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Service) allLibraries(ctx context.Context) []uuid.UUID {
	rows, err := s.pool.Query(ctx, `SELECT id FROM libraries`)
	if err != nil {
		return []uuid.UUID{}
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return idsOrDummy(ids)
}

func (s *Service) userLibraries(ctx context.Context, userID uuid.UUID) []uuid.UUID {
	rows, err := s.pool.Query(ctx, `
		SELECT library_id FROM library_grants WHERE user_id=$1
		UNION
		SELECT lg.library_id FROM library_grants lg JOIN user_roles ur ON ur.role_id=lg.role_id WHERE ur.user_id=$1
		UNION
		SELECT l.id FROM libraries l
		WHERE EXISTS (
			SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id
			WHERE ur.user_id=$1 AND r.name='Administrator'
		)`, userID)
	if err != nil {
		return []uuid.UUID{}
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return idsOrDummy(ids)
}

func uniqueAppend(dst, src []uuid.UUID, limit int) []uuid.UUID {
	seen := map[uuid.UUID]struct{}{}
	for _, id := range dst {
		seen[id] = struct{}{}
	}
	for _, id := range src {
		if len(dst) >= limit {
			break
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		dst = append(dst, id)
	}
	return dst
}

func idsOrEmpty(ids []uuid.UUID) []uuid.UUID {
	if ids == nil {
		return []uuid.UUID{}
	}
	return ids
}

func idsOrDummy(ids []uuid.UUID) []uuid.UUID {
	if len(ids) == 0 {
		return []uuid.UUID{uuid.Nil}
	}
	return ids
}

func subtractIDs(ids, exclude []uuid.UUID) []uuid.UUID {
	if len(exclude) == 0 {
		return ids
	}
	skip := map[uuid.UUID]struct{}{}
	for _, id := range exclude {
		skip[id] = struct{}{}
	}
	var out []uuid.UUID
	for _, id := range ids {
		if _, ok := skip[id]; ok {
			continue
		}
		out = append(out, id)
	}
	return out
}

type pairRow interface {
	Next() bool
	Scan(dest ...any) error
}

func scanPairs(rows pairRow) []map[string]any {
	var out []map[string]any
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			continue
		}
		out = append(out, map[string]any{"id": id, "name": name})
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out
}

func RefreshHandler(pool *pgxpool.Pool) jobs.Handler {
	svc := New(pool)
	return func(ctx context.Context, job jobs.Job) error {
		var p RefreshPayload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return err
		}
		req := Request{Kind: p.Kind, SeedID: p.SeedID, Limit: p.Limit, UserID: p.UserID, Decade: p.Decade, AllLibs: p.UserID == uuid.Nil}
		if p.UserID != uuid.Nil {
			req.Libs = svc.userLibraries(ctx, p.UserID)
		}
		_, err := svc.Select(ctx, req)
		return err
	}
}

func SmartRefreshHandler(pool *pgxpool.Pool) jobs.Handler {
	svc := New(pool)
	return func(ctx context.Context, job jobs.Job) error {
		var p SmartPayload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return err
		}
		if p.PlaylistID == uuid.Nil {
			return fmt.Errorf("playlist_id required")
		}
		return svc.RefreshSmart(ctx, p.PlaylistID)
	}
}
