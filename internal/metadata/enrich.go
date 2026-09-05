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
	credits := parseArtistCredits(rec)
	if len(credits) == 0 {
		return ""
	}
	return credits[0].Name
}

func isFeaturedJoin(join string) bool {
	j := strings.ToLower(join)
	return strings.Contains(j, "feat") || strings.Contains(j, "ft.") || strings.Contains(j, "featuring")
}

func parseArtistCredits(rec map[string]any) []ArtistCredit {
	if rec == nil {
		return nil
	}
	raw, _ := rec["artist-credit"].([]any)
	type row struct{ name, sort, mbid, join string }
	var rows []row
	for _, c := range raw {
		m, _ := c.(map[string]any)
		if m == nil {
			continue
		}
		name, _ := m["name"].(string)
		join, _ := m["joinphrase"].(string)
		sort, mbid := "", ""
		if art, _ := m["artist"].(map[string]any); art != nil {
			if strings.TrimSpace(name) == "" {
				name, _ = art["name"].(string)
			}
			sort, _ = art["sort-name"].(string)
			mbid, _ = art["id"].(string)
		}
		if strings.TrimSpace(name) == "" {
			continue
		}
		rows = append(rows, row{name: name, sort: sort, mbid: mbid, join: join})
	}
	var out []ArtistCredit
	prevJoin := ""
	for i, r := range rows {
		role := "primary"
		if i > 0 && isFeaturedJoin(prevJoin) {
			role = "featured"
		}
		out = append(out, ArtistCredit{Name: r.name, SortName: r.sort, MBID: r.mbid, Role: role})
		prevJoin = r.join
	}
	return out
}

var junkGenre = map[string]bool{
	"seen live": true, "favorite": true, "favourite": true, "albums i own": true,
	"my music": true, "awesome": true, "beautiful": true, "check out": true,
	"love": true, "sexy": true,
}

func titleGenre(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	if lower == "r&b" || lower == "rnb" {
		return "R&B"
	}
	parts := strings.Fields(lower)
	for i, p := range parts {
		segs := strings.Split(p, "-")
		for j, seg := range segs {
			if seg == "" {
				continue
			}
			segs[j] = strings.ToUpper(seg[:1]) + seg[1:]
		}
		parts[i] = strings.Join(segs, "-")
	}
	return strings.Join(parts, " ")
}

func namedCounts(raw []any) []string {
	type hit struct {
		name  string
		count int
	}
	var hits []hit
	for _, item := range raw {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		name, _ := m["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" || junkGenre[strings.ToLower(name)] {
			continue
		}
		hits = append(hits, hit{name: titleGenre(name), count: jsonInt(m["count"])})
	}
	for i := 0; i < len(hits); i++ {
		for j := i + 1; j < len(hits); j++ {
			if hits[j].count > hits[i].count {
				hits[i], hits[j] = hits[j], hits[i]
			}
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, h := range hits {
		key := strings.ToLower(h.name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, h.name)
	}
	return out
}

func pickGenres(raw map[string]any, max int) []string {
	if raw == nil || max <= 0 {
		return nil
	}
	if official := namedCounts(asAnySlice(raw["genres"])); len(official) > 0 {
		if len(official) > max {
			official = official[:max]
		}
		return official
	}
	tags := namedCounts(asAnySlice(raw["tags"]))
	var kept []string
	for _, t := range tags {
		if len(kept) >= max {
			break
		}
		kept = append(kept, t)
	}
	return kept
}

func asAnySlice(v any) []any {
	s, _ := v.([]any)
	return s
}

// GenreList is the canonical genre set for a probe (MusicBrainz list, else genre_text).
func GenreList(p Probe) []string {
	if len(p.Genres) > 0 {
		return p.Genres
	}
	g := strings.TrimSpace(p.Genre)
	if g == "" {
		return nil
	}
	return []string{g}
}

func applyArtistCredits(p *Probe, rec map[string]any) {
	credits := parseArtistCredits(rec)
	if len(credits) == 0 {
		return
	}
	if p.ArtistMBID == "" {
		p.ArtistMBID = credits[0].MBID
	}
	if p.ArtistSortName == "" {
		p.ArtistSortName = credits[0].SortName
	}
	if p.Artist == "" {
		p.Artist = credits[0].Name
	}
	if len(p.Credits) == 0 {
		p.Credits = credits
	}
}

func applyGenres(p *Probe, src map[string]any) {
	if p.Genre != "" && len(p.Genres) > 0 {
		return
	}
	genres := pickGenres(src, 5)
	if len(genres) == 0 {
		if rel := firstRelease(src); rel != nil {
			genres = pickGenres(rel, 5)
		}
	}
	if len(genres) == 0 {
		return
	}
	if len(p.Genres) == 0 {
		p.Genres = genres
	}
	if p.Genre == "" {
		p.Genre = genres[0]
	}
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

func applyRecordingMatch(p *Probe, rec map[string]any, conf float64) {
	if id, _ := rec["id"].(string); id != "" && p.RecordingMBID == "" {
		p.RecordingMBID = id
	}
	if p.Title == "" {
		if title, _ := rec["title"].(string); title != "" {
			p.Title = title
		}
	}
	applyArtistCredits(p, rec)
	applyGenres(p, rec)
	rel := firstRelease(rec)
	if rel != nil {
		applyReleaseFields(p, rel, conf)
		applyGenres(p, rel)
		return
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
		applyRecordingMatch(p, rec, conf)
		return
	}
	if _, ok := raw["artist-credit"]; ok {
		if id, _ := raw["id"].(string); id != "" || raw["title"] != nil {
			title, _ := raw["title"].(string)
			conf := MatchConfidence(p.Title, p.Artist, p.DurationMS, title, recordingArtist(raw), jsonInt(raw["length"]))
			if conf < MinEnrichConfidence && p.RecordingMBID != "" && p.RecordingMBID == id {
				conf = p.Confidence
			}
			if conf < MinEnrichConfidence && p.Confidence >= MinEnrichConfidence {
				conf = p.Confidence
			}
			if conf < MinEnrichConfidence {
				return
			}
			applyRecordingMatch(p, raw, conf)
			return
		}
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
	applyArtistCredits(p, rel)
	applyGenres(p, rel)
	applyReleaseFields(p, rel, conf)
}

func enrichRecordingGenres(ctx context.Context, p *Probe) {
	if p == nil || p.RecordingMBID == "" || (p.Genre != "" && len(p.Genres) > 0) {
		return
	}
	raw, err := (MusicBrainz{}).LookupRecording(ctx, p.RecordingMBID)
	if err != nil || raw == nil {
		return
	}
	applyArtistCredits(p, raw)
	applyGenres(p, raw)
}

func enrichMusicBrainz(ctx context.Context, p *Probe) {
	if p == nil {
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
	enrichRecordingGenres(ctx, p)
}

// EnrichMusicBrainz fills MBID / missing tags only when metadata_external_enabled is true.
func EnrichMusicBrainz(ctx context.Context, pool *pgxpool.Pool, p *Probe) {
	if p == nil || pool == nil || !ExternalEnabled(ctx, pool) {
		return
	}
	enrichMusicBrainz(ctx, p)
}

// EnrichMusicBrainzForced looks up MusicBrainz even when the admin toggle is off.
func EnrichMusicBrainzForced(ctx context.Context, p *Probe) {
	enrichMusicBrainz(ctx, p)
}

func enrichCoverArt(ctx context.Context, p *Probe) {
	if p == nil || len(p.Picture) > 0 || strings.TrimSpace(p.MBID) == "" {
		return
	}
	img, err := (CoverArt{}).FetchFront(ctx, p.MBID)
	if err != nil || len(img) == 0 {
		return
	}
	p.Picture = img
}

// EnrichCoverArt fetches Cover Art Archive front art when the probe has an MBID and no picture.
func EnrichCoverArt(ctx context.Context, pool *pgxpool.Pool, p *Probe) {
	if p == nil || pool == nil || !ExternalEnabled(ctx, pool) {
		return
	}
	enrichCoverArt(ctx, p)
}

// EnrichCoverArtForced fetches Cover Art Archive art even when the admin toggle is off.
func EnrichCoverArtForced(ctx context.Context, p *Probe) {
	enrichCoverArt(ctx, p)
}
