package update

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/db"
	"github.com/sounddock/sounddock/internal/oplog"
	"github.com/sounddock/sounddock/internal/version"
)

const settingsKey = "app_update"

// CanonicalImage is the only registry repository SoundDock will pull.
const CanonicalImage = "ghcr.io/skila1/sounddock"

type Status struct {
	AutoEnabled       bool             `json:"auto_enabled"`
	HelperOK          bool             `json:"helper_ok"`
	SocketOK          bool             `json:"socket_ok"`
	CanApply          bool             `json:"can_apply"`
	Available         bool             `json:"available"`
	Version           string           `json:"version"`
	LatestVersion     string           `json:"latest_version"`
	Image             string           `json:"image"`
	CurrentDigest     string           `json:"current_digest"`
	LatestDigest      string           `json:"latest_digest"`
	ExpectedDigest    string           `json:"expected_digest,omitempty"`
	Changelog         []ChangelogEntry `json:"changelog"`
	Progress          *Progress        `json:"progress,omitempty"`
	LastCheckAt       *time.Time       `json:"last_check_at"`
	LastAppliedAt     *time.Time       `json:"last_applied_at"`
	LastStatus        string           `json:"last_status"`
	LastError         string           `json:"last_error"`
	LastAppliedBy     string           `json:"last_applied_by"`
	Checking          bool             `json:"checking"`
	Updating          bool             `json:"updating"`
	ApplyReason       string           `json:"apply_reason"`
	ApplyKind         string           `json:"apply_kind,omitempty"`
	Reversible        bool             `json:"reversible"`
	SchemaForwardOnly bool             `json:"schema_forward_only"`
	NeedsRecovery     bool             `json:"needs_recovery"`
	SchemaVersion     int64            `json:"schema_version"`
	TargetSchema      int64            `json:"target_schema"`
	BackupPath        string           `json:"backup_path,omitempty"`
}

type stored struct {
	AutoEnabled    bool             `json:"auto_enabled"`
	Available      bool             `json:"available"`
	CurrentDigest  string           `json:"current_digest"`
	LatestDigest   string           `json:"latest_digest"`
	ExpectedDigest string           `json:"expected_digest,omitempty"`
	LatestVersion  string           `json:"latest_version"`
	Changelog      []ChangelogEntry `json:"changelog"`
	LastCheckAt    *time.Time       `json:"last_check_at"`
	LastAppliedAt  *time.Time       `json:"last_applied_at"`
	LastStatus     string           `json:"last_status"`
	LastError      string           `json:"last_error"`
	LastAppliedBy  string           `json:"last_applied_by"`
	ApplyKind      string           `json:"apply_kind,omitempty"`
	NeedsRecovery  bool             `json:"needs_recovery"`
	SchemaBefore   int64            `json:"schema_before,omitempty"`
	TargetSchema   int64            `json:"target_schema,omitempty"`
	BackupPath     string           `json:"backup_path,omitempty"`
	OldImageHead   int64            `json:"old_image_head,omitempty"`
}

var (
	mu       sync.Mutex
	checking bool
	applying bool
)

func ImageRef() string {
	if v := strings.TrimSpace(os.Getenv("SD_IMAGE")); v != "" && imageIsCanonical(v) {
		return v
	}
	return CanonicalImage + ":latest"
}

func imageIsCanonical(ref string) bool {
	s := strings.TrimSpace(strings.ToLower(ref))
	base := strings.ToLower(CanonicalImage)
	if s == base {
		return true
	}
	return strings.HasPrefix(s, base+":") || strings.HasPrefix(s, base+"@")
}

func ProjectName() string {
	if v := strings.TrimSpace(os.Getenv("SD_COMPOSE_PROJECT")); v != "" {
		return v
	}
	return "sounddock"
}

func Load(ctx context.Context, pool *pgxpool.Pool) Status {
	reconcile(ctx, pool)
	st := loadStored(ctx, pool)
	if rec := ReadRecovery(); rec.Status == "needs_recovery" {
		st.LastStatus = "needs_recovery"
		st.NeedsRecovery = true
		if rec.Detail != "" && st.LastError == "" {
			st.LastError = rec.Detail
		}
		if rec.Backup != "" && st.BackupPath == "" {
			st.BackupPath = rec.Backup
		}
	}
	mu.Lock()
	ch, ap := checking, applying
	mu.Unlock()
	helper := HelperOK()
	sock := SocketOK()
	updating := ap || st.LastStatus == "updating" || RequestPending() || HelperActive()
	prog := ReadHostProgress(updating || st.NeedsRecovery)
	var progress *Progress
	if updating || prog.Stage == "error" || prog.Stage == "needs_recovery" {
		cp := prog
		progress = &cp
	}
	latestVer := st.LatestVersion
	if latestVer == "" {
		latestVer = version.Version
	}
	schemaNow := st.SchemaBefore
	if v, _, err := db.Version(ctx, pool); err == nil {
		schemaNow = v
	}
	kind := Kind(st.ApplyKind)
	if kind == "" {
		kind = Classify(schemaNow, st.TargetSchema)
	}
	reason := ""
	switch {
	case st.NeedsRecovery || st.LastStatus == "needs_recovery":
		reason = "A schema-forward update failed after migrate. The previous image was not started. Restore from the pre-update SQL backup."
	case helper:
		reason = "The host helper (sounddock-update) is available. Update now will pull the image on the host and recreate this container."
	case sock:
		reason = "No host helper. SD_ALLOW_DOCKER_SOCK is on, so Update now will pull via Docker and recreate the app container."
	default:
		reason = "Neither the host helper nor an opted-in Docker socket is available. Check now still works. Update now cannot run until you re-run the installer or set SD_ALLOW_DOCKER_SOCK."
	}
	return Status{
		AutoEnabled:       st.AutoEnabled,
		HelperOK:          helper,
		SocketOK:          sock,
		CanApply:          helper || sock,
		Available:         st.Available,
		Version:           version.Version,
		LatestVersion:     latestVer,
		Image:             ImageRef(),
		CurrentDigest:     st.CurrentDigest,
		LatestDigest:      st.LatestDigest,
		ExpectedDigest:    st.ExpectedDigest,
		Changelog:         st.Changelog,
		Progress:          progress,
		LastCheckAt:       st.LastCheckAt,
		LastAppliedAt:     st.LastAppliedAt,
		LastStatus:        st.LastStatus,
		LastError:         st.LastError,
		LastAppliedBy:     st.LastAppliedBy,
		Checking:          ch,
		Updating:          updating,
		ApplyReason:       reason,
		ApplyKind:         string(kind),
		Reversible:        kind == KindImageOnly,
		SchemaForwardOnly: kind == KindSchemaForward,
		NeedsRecovery:     st.NeedsRecovery || st.LastStatus == "needs_recovery",
		SchemaVersion:     schemaNow,
		TargetSchema:      st.TargetSchema,
		BackupPath:        st.BackupPath,
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
	if st.LastStatus != "needs_recovery" {
		st.LastStatus = "checking"
		st.LastError = ""
	}
	_ = save(ctx, pool, st)

	img := ImageRef()
	current := AppliedDigest()
	if current == "" && SocketOK() {
		if d, err := RunningDigest(ctx, img, ProjectName()); err == nil {
			current = d
		}
	}
	if current == "" {
		current = st.CurrentDigest
	}
	latest, err := RegistryDigest(ctx, img)
	if err != nil {
		if st.LastStatus != "needs_recovery" {
			st.LastStatus = "error"
		}
		st.LastError = err.Error()
		_ = save(ctx, pool, st)
		return Load(ctx, pool), err
	}
	st.CurrentDigest = current
	st.LatestDigest = latest
	st.Available = latest != "" && (current == "" || !digestEqual(current, latest))
	if lv, notes := FetchReleaseNotes(ctx, version.Version); lv != "" || len(notes) > 0 {
		if lv != "" {
			st.LatestVersion = lv
			if compareVersions(lv, version.Version) > 0 {
				st.Available = true
			}
		}
		st.Changelog = notes
	}
	if st.LastStatus != "needs_recovery" && st.LastStatus != "updating" {
		st.LastStatus = "ok"
	}
	_ = save(ctx, pool, st)
	return Load(ctx, pool), nil
}

func Apply(ctx context.Context, pool *pgxpool.Pool, by string) error {
	started, err := BeginApply(ctx, pool, by)
	if err != nil {
		return err
	}
	if !started {
		return nil
	}
	return RunApply(ctx, pool, by)
}

// BeginApply records the update and drops update/request for the host helper.
func BeginApply(ctx context.Context, pool *pgxpool.Pool, by string) (bool, error) {
	mu.Lock()
	if applying {
		mu.Unlock()
		return false, nil
	}
	applying = true
	mu.Unlock()

	st := loadStored(ctx, pool)
	if st.NeedsRecovery || st.LastStatus == "needs_recovery" {
		mu.Lock()
		applying = false
		mu.Unlock()
		return false, fmt.Errorf("instance needs recovery from the pre-update SQL backup before another apply")
	}
	now := time.Now().UTC()
	schemaNow, _, _ := db.Version(ctx, pool)
	st.LastStatus = "updating"
	st.LastError = ""
	st.LastAppliedBy = by
	st.LastAppliedAt = &now
	st.Available = false
	st.ExpectedDigest = st.LatestDigest
	st.ApplyKind = string(KindImageOnly)
	st.SchemaBefore = schemaNow
	st.TargetSchema = schemaNow
	st.OldImageHead = schemaNow
	st.NeedsRecovery = false
	if err := save(ctx, pool, st); err != nil {
		mu.Lock()
		applying = false
		mu.Unlock()
		return false, err
	}

	helper, sock := HelperOK(), SocketOK()
	if !helper && !sock {
		st.LastStatus = "error"
		st.LastError = "host update helper is not available. Re-run the installer so sounddock-update can pull images on the host"
		_ = save(ctx, pool, st)
		mu.Lock()
		applying = false
		mu.Unlock()
		return false, fmt.Errorf("%s", st.LastError)
	}
	if helper {
		if err := RequestUpdate(by); err != nil && !sock {
			st.LastStatus = "error"
			st.LastError = err.Error()
			_ = save(ctx, pool, st)
			mu.Lock()
			applying = false
			mu.Unlock()
			return false, err
		}
	} else {
		writeProgress(8, "queued", "Pulling via the Docker socket")
	}
	_ = oplog.Write(ctx, pool, oplog.Entry{Level: "info", Category: "update", Message: "update started", Details: map[string]any{"type": "app.update.apply", "kind": st.ApplyKind, "by": by}})
	return true, nil
}

func RunApply(ctx context.Context, pool *pgxpool.Pool, by string) error {
	defer func() {
		mu.Lock()
		applying = false
		mu.Unlock()
	}()

	st := loadStored(ctx, pool)
	helper, sock := HelperOK(), SocketOK()
	if helper && waitHelper(12*time.Second) {
		return nil
	}

	if sock {
		writeProgress(10, "pulling", "Pulling "+ImageRef()+" via Docker socket")
		ClearRequest()
		policy := RollbackPolicy{
			SchemaBefore:   st.SchemaBefore,
			OldImageHead:   st.OldImageHead,
			PreviousDigest: st.CurrentDigest,
			ExpectedDigest: st.ExpectedDigest,
		}
		if err := PullAndSwap(ctx, ImageRef(), ProjectName(), policy); err != nil {
			if helper && HelperActive() {
				return nil
			}
			st.LastStatus = "error"
			st.LastError = err.Error()
			_ = save(ctx, pool, st)
			writeProgress(0, "error", err.Error())
			return err
		}
		writeProgress(80, "restarting", "Starting updated containers")
		return nil
	}

	if helper {
		return nil
	}
	st.LastStatus = "error"
	st.LastError = "update did not start"
	_ = save(ctx, pool, st)
	return fmt.Errorf("%s", st.LastError)
}

func waitHelper(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if helperTookOver() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func helperTookOver() bool {
	if RequestPending() {
		return false
	}
	prog := ReadHostProgress(true)
	switch prog.Stage {
	case "queued", "pulling", "restarting", "done", "backing_up", "needs_recovery":
		return true
	default:
		return false
	}
}

func reconcile(ctx context.Context, pool *pgxpool.Pool) {
	st := loadStored(ctx, pool)
	if rec := ReadRecovery(); rec.Status == "needs_recovery" {
		if !st.NeedsRecovery || st.LastStatus != "needs_recovery" {
			st.LastStatus = "needs_recovery"
			st.NeedsRecovery = true
			st.LastError = rec.Detail
			if rec.Backup != "" {
				st.BackupPath = rec.Backup
			}
			_ = save(ctx, pool, st)
		}
		return
	}
	if RequestPending() || HelperActive() {
		return
	}
	if st.LastStatus != "updating" {
		return
	}
	d := AppliedDigest()
	if d == "" && SocketOK() {
		if got, err := RunningDigest(ctx, ImageRef(), ProjectName()); err == nil {
			d = got
		}
	}
	healthy := AppliedHealthy() || (d != "" && digestEqual(d, firstNonEmpty(st.ExpectedDigest, st.LatestDigest)))
	next, ok := confirmApply(st, d, healthy)
	if ok {
		now := time.Now().UTC()
		next.LastAppliedAt = &now
		_ = save(ctx, pool, next)
		_ = oplog.Write(ctx, pool, oplog.Entry{Level: "info", Category: "update", Message: "update applied", Details: map[string]any{"digest": d}})
		return
	}
	if d == "" {
		started := st.LastAppliedAt
		if started == nil {
			started = st.LastCheckAt
		}
		if started != nil && time.Since(*started) > 30*time.Minute {
			st.LastStatus = "error"
			st.LastError = "update did not finish. Use docker compose pull && docker compose up -d on the host."
			_ = save(ctx, pool, st)
		}
	}
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func Tick(ctx context.Context, pool *pgxpool.Pool) {
	st := loadStored(ctx, pool)
	if !st.AutoEnabled || st.NeedsRecovery || st.LastStatus == "needs_recovery" {
		return
	}
	if st.LastCheckAt != nil && time.Since(*st.LastCheckAt) < time.Hour {
		if st.Available && CanApply() {
			_ = Apply(ctx, pool, "auto")
		}
		return
	}
	s, err := Check(ctx, pool)
	if err != nil {
		return
	}
	if s.AutoEnabled && s.Available && s.CanApply && !s.NeedsRecovery {
		_ = Apply(ctx, pool, "auto")
	}
}
