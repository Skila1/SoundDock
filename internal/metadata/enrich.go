package metadata

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

const SettingMetadataExternal = "metadata_external_enabled"

// ExternalEnabled is false unless an admin turned MusicBrainz on.
func ExternalEnabled(ctx context.Context, pool *pgxpool.Pool) bool {
	if pool == nil {
		return false
	}
	var v bool
	err := pool.QueryRow(ctx, `SELECT (value)::boolean FROM server_settings WHERE key=$1`, SettingMetadataExternal).Scan(&v)
	if err != nil {
		return false
	}
	return v
}

// EnrichMusicBrainz fills MBID / missing tags only when metadata_external_enabled is true.
func EnrichMusicBrainz(ctx context.Context, pool *pgxpool.Pool, p *Probe) {
	if p == nil || pool == nil || !ExternalEnabled(ctx, pool) {
		return
	}
	if p.Artist == "" && p.Album == "" && p.Title == "" {
		return
	}
	raw, err := (MusicBrainz{}).Lookup(ctx, p.Artist, p.Album, p.Title)
	if err != nil || raw == nil {
		return
	}
	releases, _ := raw["releases"].([]any)
	if len(releases) == 0 {
		return
	}
	rel, _ := releases[0].(map[string]any)
	if rel == nil {
		return
	}
	if id, _ := rel["id"].(string); id != "" && p.MBID == "" {
		p.MBID = id
	}
	if p.Album == "" {
		if title, _ := rel["title"].(string); title != "" {
			p.Album = title
		}
	}
	if p.Year == 0 {
		if date, _ := rel["date"].(string); len(date) >= 4 {
			if y, err := strconv.Atoi(date[:4]); err == nil {
				p.Year = y
			}
		}
	}
	if score, ok := rel["score"].(float64); ok {
		p.Confidence = score / 100
	} else {
		p.Confidence = 0.7
	}
	p.Source = "musicbrainz"
}
