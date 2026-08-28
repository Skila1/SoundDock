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
	// SettingKey is the server_settings key for {enabled, provider_url}.
	SettingKey = "lyrics_provider"

	SourceManual   = "manual"
	SourceUser     = "user" // metadata editor (never overwrite)
	SourceEmbedded = "embedded"
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

// Line is one synced lyric cue.
type Line struct {
	Tms  int    `json:"t_ms"`
	Text string `json:"text"`
}

// Config is the admin lyrics-provider setting. Empty URL / enabled=false means no network.
type Config struct {
	Enabled     bool   `json:"enabled"`
	ProviderURL string `json:"provider_url"`
}

// Service reads the lyrics table and, when enabled, LRCLIB.
type Service struct {
	pool   *pgxpool.Pool
	log    *slog.Logger
	client *http.Client

	listFn  func(ctx context.Context, trackID uuid.UUID) ([]Result, error)
	saveFn  func(ctx context.Context, trackID uuid.UUID, source, body string, timed bool) error
	urlFn   func(ctx context.Context) string
	lockFn  func(ctx context.Context, trackID uuid.UUID) bool
	fetchFn func(ctx context.Context, origin string, meta Meta) (body string, timed bool, err error)
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
// > embedded > cached provider > provider fetch. Failure is non-fatal.
func (s *Service) GetLyrics(ctx context.Context, meta Meta) Result {
	if s == nil {
		return Result{}
	}
	rows, err := s.list(ctx, meta.TrackID)
	if err != nil {
		s.warn("lyrics cache lookup failed", "err", err, "track_id", meta.TrackID)
		return Result{}
	}
	if hit := pickCached(rows); hit.Body != "" || hit.Source != "" {
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

func pickCached(rows []Result) Result {
	var embedded, provider Result
	for _, row := range rows {
		switch {
		case isProtected(row.Source):
			if strings.TrimSpace(row.Body) != "" || row.Source != "" {
				return row
			}
		case row.Source == SourceEmbedded && embedded.Source == "":
			embedded = row
		case provider.Source == "":
			provider = row
		}
	}
	if embedded.Source != "" {
		return embedded
	}
	return provider
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
