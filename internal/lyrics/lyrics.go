// Package lyrics looks up cached track lyrics and optionally fetches from a
// documented provider (LRCLIB) when an admin has configured a provider URL.
// Genius/Musixmatch HTML scrape is not used.
package lyrics

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/metadata"
)

const (
	// PermConfigure is the admin permission for GET|PUT /api/v1/admin/lyrics.
	PermConfigure = "lyrics.configure"
	// SettingKey is the server_settings key for local + optional LRCLIB.
	SettingKey = "lyrics_provider"

	SourceManual   = "manual"
	SourceUser     = "user" // metadata editor (never overwrite)
	SourceEmbedded = "embedded"
	SourceLocal    = "local"
	SourceLRCLIB   = "lrclib"

	AllowedHost = "lrclib.net"
)

// Meta identifies a track for cache lookup and provider search.
type Meta struct {
	Title      string
	Artist     string
	Album      string
	DurationMS int
	TrackID    uuid.UUID
}

// Result is a cached or fetched lyrics payload. Empty Body means none.
type Result struct {
	Body   string
	Timed  bool
	Source string
}

// Word is one timed token inside a Line (enhanced LRC, A2, or interpolated).
type Word struct {
	Tms  int    `json:"t_ms"`
	Text string `json:"text"`
}

// Line is one synced lyric cue.
type Line struct {
	Tms   int    `json:"t_ms"`
	Text  string `json:"text"`
	Words []Word `json:"words,omitempty"`
}

// Config is the admin lyrics setting. Local (embedded + on-disk) is the default.
// External LRCLIB is optional.
type Config struct {
	LocalEnabled    bool   `json:"local_enabled"`
	ExternalEnabled bool   `json:"external_enabled"`
	Enabled         bool   `json:"enabled"` // same as ExternalEnabled for older clients
	ProviderURL     string `json:"provider_url"`
}

// Service reads the lyrics table and, when enabled, LRCLIB.
type Service struct {
	pool   *pgxpool.Pool
	log    *slog.Logger
	client *http.Client

	localDir string

	listFn  func(ctx context.Context, trackID uuid.UUID) ([]Result, error)
	saveFn  func(ctx context.Context, trackID uuid.UUID, source, body string, timed bool) error
	urlFn   func(ctx context.Context) string
	lockFn  func(ctx context.Context, trackID uuid.UUID) bool
	fetchFn func(ctx context.Context, origin string, meta Meta) (body string, timed bool, err error)
}

// WithLocalDir sets the on-disk lyrics folder ({artist}/{title}.lrc).
func (s *Service) WithLocalDir(dir string) *Service {
	if s != nil {
		s.localDir = dir
	}
	return s
}

// New builds a Service. log and pool may be nil (GetLyrics then returns empty).
func New(pool *pgxpool.Pool, log *slog.Logger) *Service {
	return &Service{
		pool: pool,
		log:  log,
		client: &http.Client{
			Timeout: 8 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if hostAllowed(req.URL.Hostname()) {
					return nil
				}
				return http.ErrUseLastResponse
			},
		},
	}
}

// GetLyrics returns lyrics for meta. Lookup order: manual/user (never overwritten)
// > local (embedded + on-disk files) > cached provider (only when ExternalEnabled)
// > optional LRCLIB fetch. Provider cache is left in place when external is off.
func (s *Service) GetLyrics(ctx context.Context, meta Meta) Result {
	if s == nil {
		return Result{}
	}
	rows, err := s.list(ctx, meta.TrackID)
	if err != nil {
		s.warn("lyrics cache lookup failed", "err", err, "track_id", meta.TrackID)
		return Result{}
	}
	if hit := pickProtected(rows); hit.Body != "" || hit.Source != "" {
		return hit
	}
	if s.localOn(ctx) {
		if hit := pickEmbedded(rows); hit.Source != "" {
			return hit
		}
		if hit := s.lookupLocal(meta); hit.Body != "" {
			return hit
		}
	}
	if !s.externalOn(ctx) {
		return Result{}
	}
	if hit := pickProvider(rows); hit.Source != "" {
		return hit
	}
	if s.lyricsLocked(ctx, meta.TrackID) {
		return Result{}
	}
	origin := strings.TrimSpace(s.providerURL(ctx))
	if origin == "" {
		return Result{}
	}
	if strings.TrimSpace(meta.Title) == "" || strings.TrimSpace(meta.Artist) == "" {
		return Result{}
	}
	body, timed, err := s.fetch(ctx, origin, meta)
	if err != nil {
		s.warn("lyrics provider fetch failed", "err", err, "track_id", meta.TrackID)
		return Result{}
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return Result{}
	}
	if !timed {
		timed = metadata.LyricsTimed(body)
	}
	if err := s.saveProvider(ctx, meta.TrackID, SourceLRCLIB, body, timed); err != nil {
		s.warn("lyrics cache save failed", "err", err, "track_id", meta.TrackID)
	}
	return Result{Body: body, Timed: timed, Source: SourceLRCLIB}
}

func pickProtected(rows []Result) Result {
	for _, row := range rows {
		if isProtected(row.Source) && (strings.TrimSpace(row.Body) != "" || row.Source != "") {
			return row
		}
	}
	return Result{}
}

func pickEmbedded(rows []Result) Result {
	for _, row := range rows {
		if row.Source == SourceEmbedded || row.Source == SourceLocal {
			return row
		}
	}
	return Result{}
}

func pickProvider(rows []Result) Result {
	for _, row := range rows {
		if !isProtected(row.Source) && row.Source != SourceEmbedded && row.Source != SourceLocal && row.Source != "" {
			return row
		}
	}
	return Result{}
}

func pickCached(rows []Result) Result {
	if hit := pickProtected(rows); hit.Source != "" {
		return hit
	}
	if hit := pickEmbedded(rows); hit.Source != "" {
		return hit
	}
	return pickProvider(rows)
}

func isProtected(source string) bool {
	return source == SourceManual || source == SourceUser
}

func (s *Service) warn(msg string, args ...any) {
	if s == nil || s.log == nil {
		return
	}
	s.log.Warn(msg, args...)
}
