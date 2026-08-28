package metadata

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const SettingMetadataExternal = "metadata_external_enabled"

const (
	WeightTitle          = 0.40
	WeightArtist         = 0.30
	WeightDuration       = 0.30
	MinEnrichConfidence  = 0.65
	maxDurationSkewMS    = 10000
	neutralDurationScore = 0.5
)

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

func foldMeta(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func stringScore(got, want string) float64 {
	w := foldMeta(want)
	g := foldMeta(got)
	if w == "" {
		return 1
	}
	if g == "" {
		return 0
	}
	if g == w {
		return 1
	}
	if strings.Contains(g, w) || strings.Contains(w, g) {
		return 0.75
	}
	return 0
}

func durationScore(probeMS, recMS int) float64 {
	if probeMS <= 0 || recMS <= 0 {
		return neutralDurationScore
	}
	diff := absInt(probeMS - recMS)
	switch {
	case diff <= 2000:
		return 1
	case diff <= 5000:
		return 0.7
	case diff <= 15000:
		return 0.25
	default:
		return 0
	}
}

// MatchConfidence scores a MusicBrainz candidate using title, artist, and duration.
func MatchConfidence(title, artist string, durationMS int, recTitle, recArtist string, recDurationMS int) float64 {
	c := WeightTitle*stringScore(recTitle, title) +
		WeightArtist*stringScore(recArtist, artist) +
		WeightDuration*durationScore(durationMS, recDurationMS)
	if durationMS > 0 && recDurationMS > 0 && absInt(durationMS-recDurationMS) > maxDurationSkewMS {
		if c > 0.5 {
			c = 0.5
		}
	}
	return c
}

func jsonInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		i, _ := strconv.Atoi(n)
		return i
	default:
		return 0
	}
}

func recordingArtist(rec map[string]any) string {
	credits, _ := rec["artist-credit"].([]any)
	for _, c := range credits {
		m, _ := c.(map[string]any)
		if m == nil {
			continue
		}
		if name, _ := m["name"].(string); name != "" {
			return name
		}
		if art, _ := m["artist"].(map[string]any); art != nil {
			if name, _ := art["name"].(string); name != "" {
				return name
			}
		}
	}
	return ""
}

func firstRelease(rec map[string]any) map[string]any {
	releases, _ := rec["releases"].([]any)
	for _, r := range releases {
		if m, _ := r.(map[string]any); m != nil {
			return m
		}
	}
	return nil
}

func bestRecording(p *Probe, recordings []any) (map[string]any, float64) {
	var best map[string]any
	bestConf := 0.0
	for _, raw := range recordings {
		rec, _ := raw.(map[string]any)
		if rec == nil {
			continue
		}
		title, _ := rec["title"].(string)
		conf := MatchConfidence(p.Title, p.Artist, p.DurationMS, title, recordingArtist(rec), jsonInt(rec["length"]))
		if mb, ok := rec["score"].(float64); ok && mb > 0 {
			conf = 0.7*conf + 0.3*(mb/100)
		}
		if conf > bestConf {
			bestConf = conf
			best = rec
		}
	}
	return best, bestConf
}

func applyReleaseFields(p *Probe, rel map[string]any, conf float64) {
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
	p.Confidence = conf
	p.Source = "musicbrainz"
}

// applyMusicBrainz fills missing tags from a MusicBrainz payload when confidence is high.
// Low-confidence hits are ignored so existing tags are not overwritten.
func applyMusicBrainz(p *Probe, raw map[string]any) {
	if p == nil || raw == nil {
		return
	}
	if recordings, _ := raw["recordings"].([]any); len(recordings) > 0 {
		rec, conf := bestRecording(p, recordings)
		if rec == nil || conf < MinEnrichConfidence {
			return
		}
		rel := firstRelease(rec)
		if rel == nil {
			return
		}
		applyReleaseFields(p, rel, conf)
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
	relTitle, _ := rel["title"].(string)
	conf := MatchConfidence(p.Title, p.Artist, p.DurationMS, relTitle, recordingArtist(rel), 0)
	if score, ok := rel["score"].(float64); ok && score > 0 {
		conf = 0.7*conf + 0.3*(score/100)
	}
	if conf < MinEnrichConfidence {
		return
	}
	applyReleaseFields(p, rel, conf)
}

// EnrichMusicBrainz fills MBID / missing tags only when metadata_external_enabled is true.
func EnrichMusicBrainz(ctx context.Context, pool *pgxpool.Pool, p *Probe) {
	if p == nil || pool == nil || !ExternalEnabled(ctx, pool) {
		return
	}
	if p.Artist == "" && p.Album == "" && p.Title == "" {
		return
	}
	raw, err := (MusicBrainz{DurationMS: p.DurationMS}).Lookup(ctx, p.Artist, p.Album, p.Title)
	if err != nil || raw == nil {
		return
	}
	applyMusicBrainz(p, raw)
}

// EnrichCoverArt fetches Cover Art Archive front art when the probe has an MBID and no picture.
func EnrichCoverArt(ctx context.Context, pool *pgxpool.Pool, p *Probe) {
	if p == nil || pool == nil || !ExternalEnabled(ctx, pool) {
		return
	}
	if len(p.Picture) > 0 || strings.TrimSpace(p.MBID) == "" {
		return
	}
	img, err := (CoverArt{}).FetchFront(ctx, p.MBID)
	if err != nil || len(img) == 0 {
		return
	}
	p.Picture = img
}
