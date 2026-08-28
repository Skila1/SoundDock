package retention

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const SettingKey = "media_retention"

const (
	ModeDisabled  = "disabled"
	ModeAge       = "age"
	ModeStorage   = "storage"
	ModeFreeSpace = "free_space"
	ModeHybrid    = "hybrid"
)

// Policy is the admin-controlled media pruning configuration stored in
// server_settings.media_retention. Log/history day counts stay in retention_policies.
type Policy struct {
	Enabled               bool   `json:"enabled"`
	Mode                  string `json:"mode"`
	IntervalMinutes       int    `json:"interval_minutes"`
	MaxManagedBytes       int64  `json:"max_managed_bytes"`
	PruneDownToBytes      int64  `json:"prune_down_to_bytes"`
	MinFreeBytes          int64  `json:"min_free_bytes"`
	FreeSpaceTargetBytes  int64  `json:"free_space_target_bytes"`
	AgeDays               int    `json:"age_days"`
	MinPlayCountProtect   int    `json:"min_play_count_protect"`
	RecentPlayDays        int    `json:"recent_play_days"`
	BatchSize             int    `json:"batch_size"`
	DryRun                bool   `json:"dry_run"`
}

func DefaultPolicy() Policy {
	return Policy{
		Enabled:         false,
		Mode:            ModeDisabled,
		IntervalMinutes: 60,
		AgeDays:         14,
		RecentPlayDays:  7,
		BatchSize:       50,
	}
}

func Normalize(p Policy) Policy {
	d := DefaultPolicy()
	p.Mode = strings.ToLower(strings.TrimSpace(p.Mode))
	switch p.Mode {
	case ModeAge, ModeStorage, ModeFreeSpace, ModeHybrid, ModeDisabled:
	default:
		if p.Enabled {
			p.Mode = ModeAge
		} else {
			p.Mode = ModeDisabled
		}
	}
	if p.Mode == ModeDisabled {
		p.Enabled = false
	}
	if p.IntervalMinutes <= 0 {
		p.IntervalMinutes = d.IntervalMinutes
	}
	if p.IntervalMinutes > 7*24*60 {
		p.IntervalMinutes = 7 * 24 * 60
	}
	if p.AgeDays < 0 {
		p.AgeDays = 0
	}
	if p.RecentPlayDays < 0 {
		p.RecentPlayDays = 0
	}
	if p.MinPlayCountProtect < 0 {
		p.MinPlayCountProtect = 0
	}
	if p.BatchSize <= 0 {
		p.BatchSize = d.BatchSize
	}
	if p.BatchSize > 500 {
		p.BatchSize = 500
	}
	if p.MaxManagedBytes < 0 {
		p.MaxManagedBytes = 0
	}
	if p.PruneDownToBytes < 0 {
		p.PruneDownToBytes = 0
	}
	if p.MinFreeBytes < 0 {
		p.MinFreeBytes = 0
	}
	if p.FreeSpaceTargetBytes < 0 {
		p.FreeSpaceTargetBytes = 0
	}
	if p.PruneDownToBytes > 0 && p.MaxManagedBytes > 0 && p.PruneDownToBytes >= p.MaxManagedBytes {
		p.PruneDownToBytes = p.MaxManagedBytes * 9 / 10
	}
	return p
}

func (p Policy) HighWater() int64 { return p.MaxManagedBytes }

func (p Policy) LowWater() int64 {
	if p.MaxManagedBytes <= 0 {
		return 0
	}
	if p.PruneDownToBytes > 0 && p.PruneDownToBytes < p.MaxManagedBytes {
		return p.PruneDownToBytes
	}
	return p.MaxManagedBytes * 9 / 10
}

func (p Policy) FreeTarget() int64 {
	if p.MinFreeBytes <= 0 {
		return 0
	}
	if p.FreeSpaceTargetBytes > p.MinFreeBytes {
		return p.FreeSpaceTargetBytes
	}
	extra := int64(5) << 30
	return p.MinFreeBytes + extra
}

func (p Policy) Interval() time.Duration {
	if p.IntervalMinutes <= 0 {
		return time.Hour
	}
	return time.Duration(p.IntervalMinutes) * time.Minute
}

func (p Policy) UsesAge() bool {
	return p.Enabled && (p.Mode == ModeAge || p.Mode == ModeHybrid)
}

func (p Policy) UsesStorage() bool {
	return p.Enabled && p.MaxManagedBytes > 0 && (p.Mode == ModeStorage || p.Mode == ModeHybrid)
}

func (p Policy) UsesFreeSpace() bool {
	return p.Enabled && p.MinFreeBytes > 0 && (p.Mode == ModeFreeSpace || p.Mode == ModeHybrid)
}

func LoadPolicy(ctx context.Context, pool *pgxpool.Pool) Policy {
	p := DefaultPolicy()
	if pool == nil {
		return p
	}
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT value FROM server_settings WHERE key=$1`, SettingKey).Scan(&raw); err != nil || len(raw) == 0 {
		return p
	}
	_ = json.Unmarshal(raw, &p)
	return Normalize(p)
}

func SavePolicy(ctx context.Context, pool *pgxpool.Pool, p Policy) error {
	p = Normalize(p)
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO server_settings (key, value) VALUES ($1, $2::jsonb)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, SettingKey, b)
	return err
}
