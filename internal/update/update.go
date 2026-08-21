package update

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/version"
)

const settingsKey = "app_update"

type Status struct {
	AutoEnabled   bool       `json:"auto_enabled"`
	SocketOK      bool       `json:"socket_ok"`
	CanApply      bool       `json:"can_apply"`
	Available     bool       `json:"available"`
	Version       string     `json:"version"`
	Image         string     `json:"image"`
	CurrentDigest string     `json:"current_digest"`
	LatestDigest  string     `json:"latest_digest"`
	LastCheckAt   *time.Time `json:"last_check_at"`
	LastAppliedAt *time.Time `json:"last_applied_at"`
	LastStatus    string     `json:"last_status"`
	LastError     string     `json:"last_error"`
	LastAppliedBy string     `json:"last_applied_by"`
	Checking      bool       `json:"checking"`
	Updating      bool       `json:"updating"`
}

type stored struct {
	AutoEnabled   bool       `json:"auto_enabled"`
	Available     bool       `json:"available"`
	CurrentDigest string     `json:"current_digest"`
	LatestDigest  string     `json:"latest_digest"`
	LastCheckAt   *time.Time `json:"last_check_at"`
	LastAppliedAt *time.Time `json:"last_applied_at"`
	LastStatus    string     `json:"last_status"`
	LastError     string     `json:"last_error"`
	LastAppliedBy string     `json:"last_applied_by"`
}

var (
	mu       sync.Mutex
	checking bool
	applying bool
)

func ImageRef() string {
	if v := strings.TrimSpace(os.Getenv("SD_IMAGE")); v != "" {
		return v
	}
	return "ghcr.io/skila1/sounddock:latest"
}

func ProjectName() string {
	if v := strings.TrimSpace(os.Getenv("SD_COMPOSE_PROJECT")); v != "" {
		return v
	}
	return "sounddock"
}

func Load(ctx context.Context, pool *pgxpool.Pool) Status {
	st := stored{LastStatus: "idle"}
	var raw []byte
	_ = pool.QueryRow(ctx, `SELECT value FROM server_settings WHERE key=$1`, settingsKey).Scan(&raw)
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &st)
	}
	mu.Lock()
	ch, ap := checking, applying
	mu.Unlock()
	sock := SocketOK()
	return Status{
		AutoEnabled:   st.AutoEnabled,
		SocketOK:      sock,
		CanApply:      sock,
		Available:     st.Available,
		Version:       version.Version,
		Image:         ImageRef(),
		CurrentDigest: st.CurrentDigest,
		LatestDigest:  st.LatestDigest,
		LastCheckAt:   st.LastCheckAt,
		LastAppliedAt: st.LastAppliedAt,
		LastStatus:    st.LastStatus,
		LastError:     st.LastError,
		LastAppliedBy: st.LastAppliedBy,
		Checking:      ch,
		Updating:      ap,
	}
}

func save(ctx context.Context, pool *pgxpool.Pool, st stored) error {
	b, _ := json.Marshal(st)
	_, err := pool.Exec(ctx, `
		INSERT INTO server_settings (key, value) VALUES ($1, $2::jsonb)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, settingsKey, b)
	return err
}

func loadStored(ctx context.Context, pool *pgxpool.Pool) stored {
	st := stored{LastStatus: "idle"}
	var raw []byte
	_ = pool.QueryRow(ctx, `SELECT value FROM server_settings WHERE key=$1`, settingsKey).Scan(&raw)
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &st)
	}
	return st
}

func SetAuto(ctx context.Context, pool *pgxpool.Pool, on bool) error {
	st := loadStored(ctx, pool)
	st.AutoEnabled = on
	return save(ctx, pool, st)
}

func Check(ctx context.Context, pool *pgxpool.Pool) (Status, error) {
	mu.Lock()
	if checking || applying {
		mu.Unlock()
		return Load(ctx, pool), nil
	}
	checking = true
	mu.Unlock()
	defer func() {
		mu.Lock()
		checking = false
		mu.Unlock()
	}()

	st := loadStored(ctx, pool)
	now := time.Now().UTC()
	st.LastCheckAt = &now
	st.LastStatus = "checking"
	st.LastError = ""
	_ = save(ctx, pool, st)

	img := ImageRef()
	current := ""
	if SocketOK() {
		if d, err := RunningDigest(ctx, img, ProjectName()); err == nil {
			current = d
		}
	}
	latest, err := RegistryDigest(ctx, img)
	if err != nil {
		st.LastStatus = "error"
		st.LastError = err.Error()
		_ = save(ctx, pool, st)
		return Load(ctx, pool), err
	}
	st.CurrentDigest = current
	st.LatestDigest = latest
	st.Available = latest != "" && (current == "" || !digestEqual(current, latest))
	st.LastStatus = "ok"
	_ = save(ctx, pool, st)
	return Load(ctx, pool), nil
}

func Apply(ctx context.Context, pool *pgxpool.Pool, by string) error {
	mu.Lock()
	if applying {
		mu.Unlock()
		return nil
	}
	applying = true
	mu.Unlock()
	defer func() {
		mu.Lock()
		applying = false
		mu.Unlock()
	}()

	st := loadStored(ctx, pool)
	now := time.Now().UTC()
	st.LastStatus = "updating"
	st.LastError = ""
	st.LastAppliedBy = by
	_ = save(ctx, pool, st)

	if err := PullAndRecreate(ctx, ImageRef(), ProjectName()); err != nil {
		st.LastStatus = "error"
		st.LastError = err.Error()
		_ = save(ctx, pool, st)
		return err
	}
	st.LastAppliedAt = &now
	st.Available = false
	st.LastStatus = "ok"
	st.LastError = ""
	_ = save(ctx, pool, st)
	return nil
}

func Tick(ctx context.Context, pool *pgxpool.Pool) {
	st := loadStored(ctx, pool)
	if !st.AutoEnabled {
		return
	}
	if st.LastCheckAt != nil && time.Since(*st.LastCheckAt) < time.Hour {
		if st.Available && SocketOK() {
			_ = Apply(ctx, pool, "auto")
		}
		return
	}
	s, err := Check(ctx, pool)
	if err != nil {
		return
	}
	if s.AutoEnabled && s.Available && s.CanApply {
		_ = Apply(ctx, pool, "auto")
	}
}
