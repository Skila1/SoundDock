package jobs

import (
	"fmt"
	"sort"
	"strings"
)

const SettingKey = "worker_pools"

type ID string

const (
	PoolPlayback    ID = "playback"
	PoolSearch      ID = "search"
	PoolAcquisition ID = "acquisition"
	PoolSync        ID = "sync"
	PoolMaintenance ID = "maintenance"
)

type PoolConfig struct {
	Enabled        bool `json:"enabled"`
	MinWorkers     int  `json:"min_workers"`
	MaxWorkers     int  `json:"max_workers"`
	QueueLimit     int  `json:"queue_limit"`
	TimeoutSeconds int  `json:"timeout_seconds"`
	Priority       int  `json:"priority"`
	MaxRSSMB       int  `json:"max_rss_mb"`
}

type Configs map[ID]PoolConfig

type PoolInfo struct {
	ID          ID       `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Reserved    bool     `json:"reserved"`
	JobTypes    []string `json:"job_types"`
}

func All() []ID {
	return []ID{PoolPlayback, PoolSearch, PoolAcquisition, PoolSync, PoolMaintenance}
}

func Reserved(id ID) bool {
	return id == PoolPlayback || id == PoolSearch
}

func Name(id ID) string {
	switch id {
	case PoolPlayback:
		return "Playback"
	case PoolSearch:
		return "Search"
	case PoolAcquisition:
		return "Acquisition"
	case PoolSync:
		return "Sync"
	case PoolMaintenance:
		return "Maintenance"
	default:
		return string(id)
	}
}

func Description(id ID) string {
	switch id {
	case PoolPlayback:
		return "Reserved for playback-adjacent work such as party expiry and radio refresh. Stream requests stay on the HTTP API and are never queued behind downloads."
	case PoolSearch:
		return "Reserved for YouTube and other search work that can hang in yt-dlp. Library search stays on the API and is not starved by background jobs."
	case PoolAcquisition:
		return "ScapeX / yt-dlp downloads and URL ingest. A hung download cannot take playback or search workers."
	case PoolSync:
		return "Spotify and other external playlist import and periodic sync."
	case PoolMaintenance:
		return "Library scans, merges, bulk deletes, metadata, fingerprints, backups, media retention, and other background upkeep."
	default:
		return ""
	}
}

func DefaultConfigs() Configs {
	return Configs{
		PoolPlayback: {
			Enabled: true, MinWorkers: 1, MaxWorkers: 2,
			QueueLimit: 64, TimeoutSeconds: 30, Priority: 100,
		},
		PoolSearch: {
			Enabled: true, MinWorkers: 1, MaxWorkers: 2,
			QueueLimit: 64, TimeoutSeconds: 20, Priority: 90,
		},
		PoolAcquisition: {
			Enabled: true, MinWorkers: 0, MaxWorkers: 2,
			QueueLimit: 32, TimeoutSeconds: 300, Priority: 40, MaxRSSMB: 512,
		},
		PoolSync: {
			Enabled: true, MinWorkers: 0, MaxWorkers: 2,
			QueueLimit: 256, TimeoutSeconds: 600, Priority: 50,
		},
		PoolMaintenance: {
			Enabled: true, MinWorkers: 0, MaxWorkers: 2,
			QueueLimit: 64, TimeoutSeconds: 1800, Priority: 20, MaxRSSMB: 256,
		},
	}
}

func Infos() []PoolInfo {
	out := make([]PoolInfo, 0, len(All()))
	for _, id := range All() {
		out = append(out, PoolInfo{
			ID: id, Name: Name(id), Description: Description(id),
			Reserved: Reserved(id), JobTypes: TypesFor(id),
		})
	}
	return out
}

var typePools = map[string]ID{
	"party.expire":             PoolPlayback,
	"radio.refresh":            PoolPlayback,
	"search.youtube":           PoolSearch,
	"scapex.fetch":             PoolAcquisition,
	"ingest.url":               PoolAcquisition,
	"external.playlist.import": PoolSync,
	"external.playlist.tick":   PoolSync,
	"library.scan":             PoolMaintenance,
	"library.migrate":          PoolMaintenance,
	"library.merge":            PoolMaintenance,
	"library.delete":           PoolMaintenance,
	"ingest.zip":               PoolMaintenance,
	"tracks.bulk_delete":       PoolMaintenance,
	"tracks.metadata":          PoolMaintenance,
	"lyrics.fetch":             PoolMaintenance,
	"scan.duplicates":          PoolMaintenance,
	"maintenance.retention":    PoolMaintenance,
	"maintenance.gc-cache":     PoolMaintenance,
	"backup.run":               PoolMaintenance,
	"app.update.apply":         PoolMaintenance,
	"app.update.check":         PoolMaintenance,
	"waveform.generate":        PoolMaintenance,
	"fingerprint.generate":     PoolMaintenance,
	"integrity.scan":           PoolMaintenance,
	"smart_playlist.refresh":   PoolMaintenance,
}

func PoolForType(typ string) ID {
	if id, ok := typePools[typ]; ok {
		return id
	}
	if strings.HasPrefix(typ, "search.") {
		return PoolSearch
	}
	if strings.HasPrefix(typ, "scapex.") || strings.HasPrefix(typ, "ingest.") {
		return PoolAcquisition
	}
	if strings.HasPrefix(typ, "external.") {
		return PoolSync
	}
	return PoolMaintenance
}

func TypesFor(id ID) []string {
	var out []string
	for typ, p := range typePools {
		if p == id {
			out = append(out, typ)
		}
	}
	sort.Strings(out)
	return out
}

func Merge(base, overlay Configs) Configs {
	out := DefaultConfigs()
	for id, cfg := range base {
		out[id] = cfg
	}
	for id, cfg := range overlay {
		out[id] = cfg
	}
	return out
}

// Enforce rejects configuration that would starve playback or search,
// then clamps remaining values to safe ranges.
func Enforce(in Configs) (Configs, error) {
	out := DefaultConfigs()
	var errs []string
	for _, id := range All() {
		cfg := out[id]
		if got, ok := in[id]; ok {
			if Reserved(id) {
				if !got.Enabled {
					errs = append(errs, Name(id)+" cannot be disabled")
				}
				if got.MinWorkers < 1 {
					errs = append(errs, Name(id)+" must keep at least one worker")
				}
				if got.MaxWorkers < 1 {
					errs = append(errs, Name(id)+" maximum workers cannot be zero")
				}
			}
			cfg = got
		}
		out[id] = clamp(id, cfg)
	}
	if len(errs) > 0 {
		return DefaultConfigs(), fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return out, nil
}

func Sanitize(in Configs) Configs {
	out := DefaultConfigs()
	for _, id := range All() {
		cfg := out[id]
		if got, ok := in[id]; ok {
			cfg = got
		}
		out[id] = clamp(id, cfg)
	}
	return out
}

func clamp(id ID, c PoolConfig) PoolConfig {
	if Reserved(id) {
		c.Enabled = true
		if c.MinWorkers < 1 {
			c.MinWorkers = 1
		}
	} else if c.MinWorkers < 0 {
		c.MinWorkers = 0
	}
	if c.MinWorkers > 32 {
		c.MinWorkers = 32
	}
	if !c.Enabled && !Reserved(id) {
		c.MinWorkers = 0
		if c.MaxWorkers < 0 {
			c.MaxWorkers = 0
		}
	}
	if c.Enabled && c.MaxWorkers < 1 {
		c.MaxWorkers = 1
	}
	if c.MaxWorkers < c.MinWorkers {
		c.MaxWorkers = c.MinWorkers
	}
	if c.MaxWorkers > 32 {
		c.MaxWorkers = 32
	}
	if c.QueueLimit < 8 {
		c.QueueLimit = 8
	}
	if c.QueueLimit > 10000 {
		c.QueueLimit = 10000
	}
	minT, maxT := 10, 120
	switch id {
	case PoolSearch:
		minT, maxT = 5, 120
	case PoolAcquisition:
		minT, maxT = 30, 3600
	case PoolSync:
		minT, maxT = 30, 3600
	case PoolMaintenance:
		minT, maxT = 30, 7200
	}
	if c.TimeoutSeconds < minT {
		c.TimeoutSeconds = minT
	}
	if c.TimeoutSeconds > maxT {
		c.TimeoutSeconds = maxT
	}
	if c.Priority < 1 {
		c.Priority = 1
	}
	if c.Priority > 100 {
		c.Priority = 100
	}
	if c.MaxRSSMB < 0 {
		c.MaxRSSMB = 0
	}
	if c.MaxRSSMB > 65536 {
		c.MaxRSSMB = 65536
	}
	return c
}
