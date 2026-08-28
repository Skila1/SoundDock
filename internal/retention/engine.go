package retention

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/audit"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/mediabusy"
)

type PurgeFunc func(ctx context.Context, trackID uuid.UUID) (reclaimed int64, err error)

type Engine struct {
	pool       *pgxpool.Pool
	jobs       *jobs.Runner
	audit      *audit.Log
	managedDir string
	purge      PurgeFunc
	live       *mediabusy.Set
}

func New(pool *pgxpool.Pool, runner *jobs.Runner, log *audit.Log, managedDir string, purge PurgeFunc) *Engine {
	return &Engine{pool: pool, jobs: runner, audit: log, managedDir: managedDir, purge: purge, live: mediabusy.New()}
}

func (e *Engine) SetLive(s *mediabusy.Set) {
	if e == nil || s == nil {
		return
	}
	e.live = s
}

func (e *Engine) Live() *mediabusy.Set {
	if e == nil {
		return nil
	}
	return e.live
}

type Payload struct {
	Force     bool `json:"force"`
	Preview   bool `json:"preview"`
	Scheduled bool `json:"scheduled"`
}

type Candidate struct {
	ID             uuid.UUID  `json:"id"`
	Title          string     `json:"title"`
	Artist         string     `json:"artist"`
	SizeBytes      int64      `json:"size_bytes"`
	PlayCount      int        `json:"play_count"`
	LastPlayed     *time.Time `json:"last_played_at"`
	Acquisition    string     `json:"acquisition"`
	AcquisitionRef string     `json:"acquisition_ref"`
	CreatedAt      time.Time  `json:"created_at"`
	AgeEligible    bool       `json:"age_eligible"`
	Reason         string     `json:"reason"`
}

type RunResult struct {
	DryRun         bool        `json:"dry_run"`
	Mode           string      `json:"mode"`
	EligibleCount  int         `json:"eligible_count"`
	EligibleBytes  int64       `json:"eligible_bytes"`
	DeletedCount   int         `json:"deleted_count"`
	ReclaimedBytes int64       `json:"reclaimed_bytes"`
	Interrupted    bool        `json:"interrupted"`
	Preview        []Candidate `json:"preview,omitempty"`
}

type Status struct {
	Policy          Policy     `json:"policy"`
	ManagedBytes    int64      `json:"managed_bytes"`
	DiskPath        string     `json:"disk_path"`
	DiskTotal       int64      `json:"disk_total"`
	DiskFree        int64      `json:"disk_free"`
	DiskError       string     `json:"disk_error,omitempty"`
	EligibleCount   int        `json:"eligible_count"`
	EligibleBytes   int64      `json:"eligible_bytes"`
	LastPruneAt     *time.Time `json:"last_prune_at"`
	LastReclaimed   int64      `json:"last_reclaimed_bytes"`
	LastDeleted     int        `json:"last_deleted_count"`
	LastDryRun      bool       `json:"last_dry_run"`
	NextPruneAt     *time.Time `json:"next_prune_at"`
	Running         bool       `json:"running"`
	PressureStorage bool       `json:"pressure_storage"`
	PressureFree    bool       `json:"pressure_free"`
}

func Handler(e *Engine) jobs.Handler {
	return func(ctx context.Context, job jobs.Job) error {
		if e == nil || e.pool == nil {
			return nil
		}
		if err := applyLogPolicies(ctx, e.pool); err != nil {
			return err
		}
		var p Payload
		_ = json.Unmarshal(job.Payload, &p)
		_, err := e.Run(ctx, job, p)
		return err
	}
}

func (e *Engine) due(ctx context.Context, p Policy) bool {
	var finished *time.Time
	_ = e.pool.QueryRow(ctx, `SELECT finished_at FROM retention_runs WHERE finished_at IS NOT NULL ORDER BY finished_at DESC LIMIT 1`).Scan(&finished)
	if finished == nil {
		return true
	}
	return time.Since(*finished) >= p.Interval()
}

func BusyCount(ctx context.Context, pool *pgxpool.Pool) int {
	var n int
	if pool == nil {
		return 0
	}
	_ = pool.QueryRow(ctx, `
		SELECT count(*) FROM jobs
		WHERE type='maintenance.retention' AND status IN ('queued','running','retry')`).Scan(&n)
	return n
}

func EnqueueUnlessBusy(ctx context.Context, pool *pgxpool.Pool, enqueue func(context.Context, string, any) (uuid.UUID, error), payload any) (uuid.UUID, error) {
	if BusyCount(ctx, pool) > 0 {
		var id uuid.UUID
		_ = pool.QueryRow(ctx, `
			SELECT id FROM jobs
			WHERE type='maintenance.retention' AND status IN ('queued','running','retry')
			ORDER BY created_at LIMIT 1`).Scan(&id)
		return id, nil
	}
	return enqueue(ctx, "maintenance.retention", payload)
}

func (e *Engine) Run(ctx context.Context, job jobs.Job, p Payload) (RunResult, error) {
	policy := LoadPolicy(ctx, e.pool)
	out := RunResult{Mode: policy.Mode, DryRun: policy.DryRun || p.Preview}
	if p.Scheduled && !p.Force && !p.Preview {
		if !policy.Enabled || policy.Mode == ModeDisabled {
			return out, nil
		}
		if !e.due(ctx, policy) {
			return out, nil
		}
	}
	if policy.Mode == ModeDisabled && !p.Preview && !p.Force {
		return out, nil
	}
	if p.Force && policy.Mode == ModeDisabled {
		return out, nil
	}
	return e.prune(ctx, job, policy, out.DryRun)
}

func (e *Engine) Preview(ctx context.Context, limit int) (RunResult, error) {
	policy := LoadPolicy(ctx, e.pool)
	if limit <= 0 {
		limit = 100
	}
	cands, err := e.candidates(ctx, policy, fetchLimit(policy))
	if err != nil {
		return RunResult{}, err
	}
	for i := range cands {
		cands[i].Reason = candidateReason(cands[i])
	}
	planned := e.takeBatch(ctx, policy, cands)
	var bytes int64
	for _, c := range planned {
		bytes += c.SizeBytes
	}
	if len(planned) > limit {
		planned = planned[:limit]
	}
	return RunResult{
		DryRun: true, Mode: policy.Mode,
		EligibleCount: len(planned), EligibleBytes: bytes, Preview: planned,
	}, nil
}

func fetchLimit(policy Policy) int {
	n := policy.BatchSize * 5
	if n < 50 {
		n = 50
	}
	if n > 500 {
		n = 500
	}
	return n
}

func (e *Engine) takeBatch(ctx context.Context, policy Policy, cands []Candidate) []Candidate {
	managed, _ := e.managedBytes(ctx)
	_, free, _ := e.disk()
	needStorage := policy.UsesStorage() && managed >= policy.HighWater()
	needFree := policy.UsesFreeSpace() && free > 0 && free < policy.MinFreeBytes
	ageOnly := policy.Mode == ModeAge || (policy.Mode == ModeHybrid && !needStorage && !needFree)
	low := policy.LowWater()
	freeTarget := policy.FreeTarget()
	var out []Candidate
	var reclaimed int64
	for _, c := range cands {
		if len(out) >= policy.BatchSize {
			break
		}
		if !ageOnly {
			underStorage := !needStorage || (low > 0 && managed-reclaimed <= low)
			underFree := !needFree || (freeTarget > 0 && free+reclaimed >= freeTarget)
			if underStorage && underFree {
				if policy.Mode == ModeHybrid && c.AgeEligible {
					// keep idle tracks after pressure is relieved
				} else {
					break
				}
			}
		}
		out = append(out, c)
		reclaimed += c.SizeBytes
	}
	return out
}

func (e *Engine) prune(ctx context.Context, job jobs.Job, policy Policy, dry bool) (RunResult, error) {
	out := RunResult{DryRun: dry, Mode: policy.Mode}
	fetchN := policy.BatchSize * 5
	if fetchN < 50 {
		fetchN = 50
	}
	if fetchN > 500 {
		fetchN = 500
	}
	cands, err := e.candidates(ctx, policy, fetchN)
	if err != nil {
		return out, err
	}
	for i := range cands {
		cands[i].Reason = candidateReason(cands[i])
		out.EligibleBytes += cands[i].SizeBytes
	}
	out.EligibleCount = len(cands)

	managed, _ := e.managedBytes(ctx)
	_, free, _ := e.disk()
	needStorage := policy.UsesStorage() && managed >= policy.HighWater()
	needFree := policy.UsesFreeSpace() && free > 0 && free < policy.MinFreeBytes
	ageOnly := policy.Mode == ModeAge || (policy.Mode == ModeHybrid && !needStorage && !needFree)

	var runID uuid.UUID
	var jobID any
	if job.ID != uuid.Nil {
		jobID = job.ID
	}
	if err := e.pool.QueryRow(ctx, `
		INSERT INTO retention_runs (job_id, dry_run, mode, eligible_count, eligible_bytes)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`, jobID, dry, policy.Mode, out.EligibleCount, out.EligibleBytes).Scan(&runID); err != nil {
		return out, err
	}

	low := policy.LowWater()
	freeTarget := policy.FreeTarget()
	reclaimed := int64(0)
	deleted := 0
	interrupted := false

	for _, c := range cands {
		if ctx.Err() != nil || (e.jobs != nil && job.ID != uuid.Nil && e.jobs.Cancelled(ctx, job.ID)) {
			interrupted = true
			break
		}
		if deleted >= policy.BatchSize {
			break
		}
		if !ageOnly {
			underStorage := !needStorage || (low > 0 && managed-reclaimed <= low)
			underFree := !needFree || (freeTarget > 0 && free+reclaimed >= freeTarget)
			if underStorage && underFree {
				if policy.Mode == ModeHybrid && c.AgeEligible {
					// keep pruning idle tracks after pressure is relieved
				} else {
					break
				}
			}
		}
		if dry {
			if err := e.insertEvent(ctx, runID, c, true); err != nil {
				return out, err
			}
			reclaimed += c.SizeBytes
			deleted++
			continue
		}
		n, err := e.purgeOne(ctx, c)
		if err != nil {
			_, _ = e.pool.Exec(ctx, `UPDATE retention_runs SET error=$2, interrupted=true, finished_at=now() WHERE id=$1`, runID, err.Error())
			return out, err
		}
		if err := e.insertEvent(ctx, runID, c, false); err != nil {
			return out, err
		}
		if e.audit != nil {
			e.audit.Event(ctx, nil, "retention.prune", c.ID.String(), "", map[string]any{
				"title":           c.Title,
				"size_bytes":      n,
				"reason":          c.Reason,
				"last_played_at":  c.LastPlayed,
				"play_count":      c.PlayCount,
				"acquisition":     c.Acquisition,
				"acquisition_ref": c.AcquisitionRef,
			})
		}
		reclaimed += n
		deleted++
		if e.jobs != nil && job.ID != uuid.Nil {
			e.jobs.SetProgress(ctx, job.ID, 10+90*deleted/policy.BatchSize)
		}
	}

	out.DeletedCount = deleted
	out.ReclaimedBytes = reclaimed
	out.Interrupted = interrupted
	if len(cands) > 40 {
		out.Preview = cands[:40]
	} else {
		out.Preview = cands
	}
	_, _ = e.pool.Exec(ctx, `
		UPDATE retention_runs SET
		  deleted_count=$2, reclaimed_bytes=$3, interrupted=$4, finished_at=now()
		WHERE id=$1`, runID, deleted, reclaimed, interrupted)
	if e.jobs != nil && job.ID != uuid.Nil {
		e.jobs.SetResult(ctx, job.ID, out)
		e.jobs.SetProgress(ctx, job.ID, 100)
	}
	return out, nil
}

func (e *Engine) purgeOne(ctx context.Context, c Candidate) (int64, error) {
	if e.purge == nil {
		return 0, fmt.Errorf("media purge is not configured")
	}
	return e.purge(ctx, c.ID)
}

func (e *Engine) insertEvent(ctx context.Context, runID uuid.UUID, c Candidate, dry bool) error {
	_, err := e.pool.Exec(ctx, `
		INSERT INTO retention_events (
		  run_id, track_id, title, artist, size_bytes, reason, last_played_at,
		  play_count, acquisition, acquisition_ref, dry_run
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		runID, c.ID, c.Title, c.Artist, c.SizeBytes, c.Reason, c.LastPlayed,
		c.PlayCount, c.Acquisition, c.AcquisitionRef, dry)
	return err
}

func candidateReason(c Candidate) string {
	if c.PlayCount == 0 && c.LastPlayed == nil {
		return "never played after acquisition"
	}
	if c.AgeEligible {
		return "idle beyond age threshold"
	}
	return "storage or free-space pressure"
}

func (e *Engine) disk() (total, free int64, err error) {
	if e.managedDir == "" {
		return 0, 0, fmt.Errorf("managed directory is not set")
	}
	return diskUsage(e.managedDir)
}

func (e *Engine) managedBytes(ctx context.Context) (int64, error) {
	var n int64
	err := e.pool.QueryRow(ctx, `
		SELECT coalesce(sum(tf.size_bytes),0)
		FROM track_files tf
		JOIN libraries l ON l.id = tf.library_id
		JOIN storage_providers sp ON sp.id = l.storage_provider_id
		WHERE sp.type='managed' AND tf.deleted_at IS NULL`).Scan(&n)
	return n, err
}

func (e *Engine) Status(ctx context.Context) (Status, error) {
	st := Status{Policy: LoadPolicy(ctx, e.pool), DiskPath: e.managedDir}
	st.ManagedBytes, _ = e.managedBytes(ctx)
	total, free, err := e.disk()
	st.DiskTotal, st.DiskFree = total, free
	if err != nil {
		st.DiskError = err.Error()
	}
	st.PressureStorage = st.Policy.UsesStorage() && st.ManagedBytes >= st.Policy.HighWater()
	st.PressureFree = st.Policy.UsesFreeSpace() && st.DiskFree > 0 && st.DiskFree < st.Policy.MinFreeBytes
	cands, err := e.candidates(ctx, st.Policy, fetchLimit(st.Policy))
	if err != nil {
		return st, err
	}
	for i := range cands {
		cands[i].Reason = candidateReason(cands[i])
	}
	planned := e.takeBatch(ctx, st.Policy, cands)
	st.EligibleCount = len(planned)
	for _, c := range planned {
		st.EligibleBytes += c.SizeBytes
	}
	var lastAt *time.Time
	var rec int64
	var del int
	var dry bool
	_ = e.pool.QueryRow(ctx, `
		SELECT finished_at, reclaimed_bytes, deleted_count, dry_run
		FROM retention_runs
		WHERE finished_at IS NOT NULL
		ORDER BY finished_at DESC LIMIT 1`).Scan(&lastAt, &rec, &del, &dry)
	st.LastPruneAt, st.LastReclaimed, st.LastDeleted, st.LastDryRun = lastAt, rec, del, dry
	if st.Policy.Enabled && st.Policy.Mode != ModeDisabled {
		next := time.Now().UTC()
		if lastAt != nil {
			next = lastAt.Add(st.Policy.Interval())
		}
		st.NextPruneAt = &next
	}
	st.Running = BusyCount(ctx, e.pool) > 0
	return st, nil
}
