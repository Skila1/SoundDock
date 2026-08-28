package lyrics

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EnsurePerm seeds lyrics.configure and attaches it to Administrator.
// No numbered migration — first-use INSERT like other waves.
func EnsurePerm(ctx context.Context, pool *pgxpool.Pool) {
	if pool == nil {
		return
	}
	_, _ = pool.Exec(ctx, `
		INSERT INTO permissions (name, description)
		VALUES ($1, 'Configure the lyrics provider URL')
		ON CONFLICT DO NOTHING`, PermConfigure)
	_, _ = pool.Exec(ctx, `
		INSERT INTO role_permissions (role_id, permission_id)
		SELECT r.id, p.id FROM roles r, permissions p
		WHERE r.name = 'Administrator' AND p.name = $1
		ON CONFLICT DO NOTHING`, PermConfigure)
}

// LoadConfig reads server_settings.lyrics_provider. Missing key is disabled / empty.
func LoadConfig(ctx context.Context, pool *pgxpool.Pool) Config {
	cfg := Config{}
	if pool == nil {
		return cfg
	}
	var raw []byte
	err := pool.QueryRow(ctx, `SELECT value FROM server_settings WHERE key=$1`, SettingKey).Scan(&raw)
	if err != nil || len(raw) == 0 {
		return cfg
	}
	_ = json.Unmarshal(raw, &cfg)
	return normalizeConfig(cfg)
}

// StoreConfig writes the allowlisted provider setting. Empty URL disables the provider.
func StoreConfig(ctx context.Context, pool *pgxpool.Pool, cfg Config) error {
	if pool == nil {
		return nil
	}
	cfg = normalizeConfig(cfg)
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO server_settings (key, value) VALUES ($1, $2::jsonb)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, SettingKey, b)
	return err
}

func normalizeConfig(cfg Config) Config {
	url := strings.TrimSpace(cfg.ProviderURL)
	if !cfg.Enabled {
		return Config{Enabled: false, ProviderURL: ""}
	}
	canon, err := NormalizeProviderURL(url)
	if err != nil || canon == "" {
		return Config{Enabled: false, ProviderURL: ""}
	}
	return Config{Enabled: true, ProviderURL: canon}
}

func (s *Service) list(ctx context.Context, trackID uuid.UUID) ([]Result, error) {
	if s.listFn != nil {
		return s.listFn(ctx, trackID)
	}
	if s.pool == nil || trackID == uuid.Nil {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT source, timed, body FROM lyrics WHERE track_id=$1`, trackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.Source, &r.Timed, &r.Body); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) saveProvider(ctx context.Context, trackID uuid.UUID, source, body string, timed bool) error {
	if s.saveFn != nil {
		return s.saveFn(ctx, trackID, source, body, timed)
	}
	if s.pool == nil || trackID == uuid.Nil || source == "" {
		return nil
	}
	if isProtected(source) || source == SourceEmbedded {
		return nil
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE lyrics SET body=$3, timed=$4
		WHERE track_id=$1 AND source=$2
		  AND NOT EXISTS (
		    SELECT 1 FROM lyrics p
		    WHERE p.track_id=$1 AND p.source IN ('manual','user')
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM metadata_locks
		    WHERE entity_type='track' AND entity_id=$1 AND field='lyrics'
		  )`, trackID, source, body, timed)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO lyrics (track_id, source, timed, body)
		SELECT $1, $2, $3, $4
		WHERE NOT EXISTS (
		    SELECT 1 FROM lyrics p
		    WHERE p.track_id=$1 AND p.source IN ('manual','user')
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM lyrics e
		    WHERE e.track_id=$1 AND e.source=$2
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM metadata_locks
		    WHERE entity_type='track' AND entity_id=$1 AND field='lyrics'
		  )`, trackID, source, timed, body)
	return err
}

func (s *Service) providerURL(ctx context.Context) string {
	if s.urlFn != nil {
		return s.urlFn(ctx)
	}
	cfg := LoadConfig(ctx, s.pool)
	if !cfg.Enabled {
		return ""
	}
	return cfg.ProviderURL
}

func (s *Service) lyricsLocked(ctx context.Context, trackID uuid.UUID) bool {
	if s.lockFn != nil {
		return s.lockFn(ctx, trackID)
	}
	if s.pool == nil || trackID == uuid.Nil {
		return false
	}
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT 1 FROM metadata_locks
		WHERE entity_type='track' AND entity_id=$1 AND field='lyrics'`, trackID).Scan(&n)
	return err == nil
}
