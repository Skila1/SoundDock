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
}

type Result struct {
	Kind       string      `json:"kind"`
	SeedID     uuid.UUID   `json:"seed_id"`
	TrackIDs   []uuid.UUID `json:"track_ids"`
	YoutubeIDs []string    `json:"youtube_ids,omitempty"`
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

// ClampFill is the 1–20 YouTube autoplay pull size.
func ClampFill(n int) int {
	if n <= 0 {
		return 12
	}
	if n > 20 {
		return 20
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
		ids, err = s.trackRadio(ctx, req.SeedID, libs, limit)
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

func (s *Service) trackRadio(ctx context.Context, trackID uuid.UUID, libs []uuid.UUID, limit int) ([]uuid.UUID, error) {
	meta, err := s.TrackMeta(ctx, trackID)
	if err != nil {
		return nil, err
	}
	ids, err := s.queryIDs(ctx, `
		SELECT t.id FROM tracks t
		JOIN track_artists ta ON ta.track_id=t.id
		WHERE t.library_id = ANY($1) AND t.id<>$2
		  AND ta.artist_id IN (SELECT artist_id FROM track_artists WHERE track_id=$2 AND role='primary')
		ORDER BY random() LIMIT $3`, libs, trackID, limit)
	if err != nil {
		return nil, err
	}
	if len(ids) < limit && meta.AlbumID != nil {
		more, err := s.queryIDs(ctx, `
			SELECT t.id FROM tracks t
			WHERE t.album_id=$1 AND t.library_id = ANY($2) AND t.id<>$3 AND t.id <> ALL($4)
			ORDER BY random() LIMIT $5`, *meta.AlbumID, libs, trackID, idsOrDummy(ids), limit-len(ids))
		if err != nil {
			return ids, err
		}
		ids = uniqueAppend(ids, more, limit)
	}
	if len(ids) < limit {
		more, err := s.queryIDs(ctx, `
			SELECT t.id FROM tracks t
			JOIN track_genres tg ON tg.track_id=t.id
			WHERE t.library_id = ANY($1) AND t.id<>$2 AND t.id <> ALL($3)
			  AND tg.genre_id IN (SELECT genre_id FROM track_genres WHERE track_id=$2)
			ORDER BY random() LIMIT $4`, libs, trackID, idsOrDummy(ids), limit-len(ids))
		if err != nil {
			return ids, err
		}
		ids = uniqueAppend(ids, more, limit)
	}
	genre := FirstGenre(meta.Genre)
	if len(ids) < limit && genre != "" {
		more, err := s.queryIDs(ctx, `
			SELECT t.id FROM tracks t
			WHERE t.library_id = ANY($1) AND t.id<>$2 AND t.id <> ALL($3)
			  AND (t.genre_text ILIKE $4 OR EXISTS (
				SELECT 1 FROM track_genres tg JOIN genres g ON g.id=tg.genre_id
				WHERE tg.track_id=t.id AND g.name ILIKE $4
			  ))
			ORDER BY random() LIMIT $5`, libs, trackID, idsOrDummy(ids), genre, limit-len(ids))
		if err != nil {
			return ids, err
		}
		ids = uniqueAppend(ids, more, limit)
	}
	if len(ids) < limit && meta.Year != nil {
		d := DecadeStart(*meta.Year)
		more, err := s.queryIDs(ctx, `
			SELECT t.id FROM tracks t
			WHERE t.library_id = ANY($1) AND t.id<>$2 AND t.id <> ALL($3)
			  AND t.year >= $4 AND t.year < $5
			ORDER BY random() LIMIT $6`, libs, trackID, idsOrDummy(ids), d, d+10, limit-len(ids))
		if err != nil {
			return ids, err
		}
		ids = uniqueAppend(ids, more, limit)
	}
	if len(ids) < limit && meta.Title != "" {
		more, err := s.queryIDs(ctx, `
			SELECT t.id FROM tracks t
			WHERE t.library_id = ANY($1) AND t.id<>$2 AND t.id <> ALL($3)
			  AND similarity(t.title, $4) > 0.18
			ORDER BY similarity(t.title, $4) DESC, random() LIMIT $5`, libs, trackID, idsOrDummy(ids), meta.Title, limit-len(ids))
		if err != nil {
			return ids, err
		}
		ids = uniqueAppend(ids, more, limit)
	}
	return ids, nil
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
	na, nb := normSong(a), normSong(b)
	return na != "" && na == nb
}

func normSong(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "’", "'")
	return s
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
