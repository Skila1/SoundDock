package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/oplog"
)

var (
	ErrQueueFull    = errors.New("workload queue is full")
	ErrPoolDisabled = errors.New("workload pool is disabled")
	ErrJobFailed    = errors.New("job failed")
	ErrNotRetryable = errors.New("job cannot be retried")
	ErrTerminal     = errors.New("terminal job failure")
	ErrCancelled    = errors.New("job cancelled")
)

type Handler func(ctx context.Context, job Job) error

type Job struct {
	ID       uuid.UUID
	Type     string
	Payload  json.RawMessage
	Progress int
	Attempts int
	Pool     ID
}

type workItem struct {
	fn       func(context.Context) error
	ctx      context.Context
	done     chan error
	queuedAt time.Time
	label    string
}

type poolRuntime struct {
	id     ID
	r      *Runner
	mu     sync.RWMutex
	cfg    PoolConfig
	inbox  chan workItem
	live   atomic.Int32
	busy   atomic.Int32
	target atomic.Int32
	wg     sync.WaitGroup
}

type Runner struct {
	db       *pgxpool.Pool
	handlers map[string]Handler
	workerID string
	log      *slog.Logger
	cancel   context.CancelFunc
	supWG    sync.WaitGroup
	draining atomic.Bool
	started  atomic.Bool
	mu       sync.RWMutex
	runtimes map[ID]*poolRuntime
}

func New(pool *pgxpool.Pool, log *slog.Logger) *Runner {
	if log == nil {
		log = slog.Default()
	}
	r := &Runner{
		db:       pool,
		handlers: map[string]Handler{},
		workerID: uuid.NewString(),
		log:      log,
		runtimes: map[ID]*poolRuntime{},
	}
	for _, id := range All() {
		cfg := DefaultConfigs()[id]
		rt := &poolRuntime{
			id:    id,
			r:     r,
			cfg:   cfg,
			inbox: make(chan workItem, 256),
		}
		rt.target.Store(int32(cfg.MinWorkers))
		r.runtimes[id] = rt
	}
	return r
}

func (r *Runner) Register(typ string, h Handler) { r.handlers[typ] = h }

func (r *Runner) Configs() Configs {
	out := Configs{}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for id, rt := range r.runtimes {
		rt.mu.RLock()
		out[id] = rt.cfg
		rt.mu.RUnlock()
	}
	return out
}

func (r *Runner) config(id ID) PoolConfig {
	if rt := r.runtime(id); rt != nil {
		rt.mu.RLock()
		defer rt.mu.RUnlock()
		return rt.cfg
	}
	return DefaultConfigs()[id]
}

func (r *Runner) runtime(id ID) *poolRuntime {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.runtimes[id]
}

func (r *Runner) setConfigs(cfg Configs) {
	for id, c := range cfg {
		rt := r.runtime(id)
		if rt == nil {
			continue
		}
		rt.mu.Lock()
		rt.cfg = c
		rt.mu.Unlock()
		want := int32(c.MinWorkers)
		if !c.Enabled {
			want = 0
		}
		rt.target.Store(want)
	}
}

func (r *Runner) Load(ctx context.Context) {
	cfg := DefaultConfigs()
	if r.db != nil {
		var raw []byte
		if err := r.db.QueryRow(ctx, `SELECT value FROM server_settings WHERE key=$1`, SettingKey).Scan(&raw); err == nil && len(raw) > 0 {
			overlay := Configs{}
			if json.Unmarshal(raw, &overlay) == nil {
				bumped := false
				if got, ok := overlay[PoolSync]; ok && got.QueueLimit > 0 && got.QueueLimit <= 16 {
					got.QueueLimit = 256
					overlay[PoolSync] = got
					bumped = true
				}
				cfg = Sanitize(Merge(cfg, overlay))
				if bumped {
					if b, err := json.Marshal(cfg); err == nil {
						_, _ = r.db.Exec(ctx, `
							INSERT INTO server_settings (key, value) VALUES ($1, $2::jsonb)
							ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, SettingKey, b)
					}
				}
			}
		}
	}
	r.setConfigs(cfg)
}

func (r *Runner) Apply(ctx context.Context, in Configs) (Configs, error) {
	cfg, err := Enforce(Merge(r.Configs(), in))
	if err != nil {
		return nil, err
	}
	if r.db != nil {
		b, _ := json.Marshal(cfg)
		if _, err := r.db.Exec(ctx, `
			INSERT INTO server_settings (key, value) VALUES ($1, $2::jsonb)
			ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, SettingKey, b); err != nil {
			return nil, err
		}
	}
	r.setConfigs(cfg)
	return cfg, nil
}

func (r *Runner) Enqueue(ctx context.Context, typ string, payload any) (uuid.UUID, error) {
	return r.enqueue(ctx, typ, "", payload)
}

// EnqueueCoalesced returns an existing queued|running|retry job with the same
// coalesce_key instead of inserting a duplicate. The key is stored on the
// payload as "coalesce_key". Empty key falls through to Enqueue.
func (r *Runner) EnqueueCoalesced(ctx context.Context, typ, coalesceKey string, payload any) (uuid.UUID, error) {
	return r.enqueue(ctx, typ, strings.TrimSpace(coalesceKey), payload)
}

func (r *Runner) enqueue(ctx context.Context, typ, coalesceKey string, payload any) (uuid.UUID, error) {
	pool := PoolForType(typ)
	cfg := r.config(pool)
	if !cfg.Enabled {
		return uuid.Nil, fmt.Errorf("%w: %s", ErrPoolDisabled, Name(pool))
	}
	if r.db == nil {
		return uuid.Nil, errors.New("jobs: no database")
	}
	b, err := payloadWithCoalesceKey(payload, coalesceKey)
	if err != nil {
		return uuid.Nil, err
	}
	if coalesceKey != "" {
		if id, ok, err := r.findCoalesced(ctx, typ, coalesceKey); err != nil {
			return uuid.Nil, err
		} else if ok {
			return id, nil
		}
	}
	var queued int
	if err := r.db.QueryRow(ctx, `
		SELECT count(*) FROM jobs WHERE pool=$1 AND status IN ('queued','retry')`, pool).Scan(&queued); err != nil {
		return uuid.Nil, err
	}
	if queued >= cfg.QueueLimit {
		return uuid.Nil, fmt.Errorf("%w: %s", ErrQueueFull, Name(pool))
	}
	var id uuid.UUID
	err = r.db.QueryRow(ctx, `
		INSERT INTO jobs (type, payload, pool, priority) VALUES ($1,$2,$3,$4) RETURNING id`,
		typ, b, pool, cfg.Priority).Scan(&id)
	if err != nil && coalesceKey != "" {
		if id, ok, findErr := r.findCoalesced(ctx, typ, coalesceKey); findErr == nil && ok {
			return id, nil
		}
	}
	return id, err
}

func payloadWithCoalesceKey(payload any, key string) ([]byte, error) {
	var m map[string]any
	switch p := payload.(type) {
	case nil:
		m = map[string]any{}
	case map[string]any:
		m = make(map[string]any, len(p)+1)
		for k, v := range p {
			m[k] = v
		}
	default:
		raw, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &m); err != nil || m == nil {
			m = map[string]any{}
		}
	}
	if key != "" {
		m["coalesce_key"] = key
	}
	return json.Marshal(m)
}

func (r *Runner) findCoalesced(ctx context.Context, typ, key string) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := r.db.QueryRow(ctx, `
		SELECT id FROM jobs
		WHERE type=$1 AND status IN ('queued','running','retry')
		  AND payload->>'coalesce_key'=$2
		ORDER BY created_at
		LIMIT 1`, typ, key).Scan(&id)
	if err != nil {
		return uuid.Nil, false, nil
	}
	return id, true, nil
}

func (r *Runner) SetProgress(ctx context.Context, id uuid.UUID, p int) {
	if r.db == nil {
		return
	}
	_, _ = r.db.Exec(ctx, `UPDATE jobs SET progress=$2, updated_at=now() WHERE id=$1`, id, p)
}

func (r *Runner) SetResult(ctx context.Context, id uuid.UUID, v any) {
	if r.db == nil {
		return
	}
	b, _ := json.Marshal(v)
	_, _ = r.db.Exec(ctx, `UPDATE jobs SET result=$2::jsonb, updated_at=now() WHERE id=$1`, id, b)
}

func (r *Runner) RequestCancel(ctx context.Context, id uuid.UUID) error {
	if r.db == nil {
		return errors.New("jobs: no database")
	}
	var typ, status string
	var progress int
	err := r.db.QueryRow(ctx, `SELECT type, status, progress FROM jobs WHERE id=$1`, id).Scan(&typ, &status, &progress)
	if err != nil {
		return err
	}
	extra := r.cancelExtra(ctx, typ, id)
	if typ == "stats.rebuild" && strings.EqualFold(status, "running") {
		extra.StatsSwapStarted = true
	}
	if !AllowCancel(typ, status, progress, extra) {
		return ErrNotCancellable
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE jobs SET status='cancelled', cancel_requested=true, finished_at=now(), updated_at=now()
		WHERE id=$1 AND status IN ('queued','retry')`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	_, err = r.db.Exec(ctx, `UPDATE jobs SET cancel_requested=true WHERE id=$1 AND status='running'`, id)
	return err
}

func (r *Runner) Retry(ctx context.Context, id uuid.UUID) error {
	if r.db == nil {
		return errors.New("jobs: no database")
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE jobs SET status='queued', attempts=0, last_error=NULL, cancel_requested=false,
			run_after=now(), locked_until=NULL, finished_at=NULL, updated_at=now()
		WHERE id=$1 AND status IN ('failed','cancelled')`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotRetryable
	}
	return nil
}

func (r *Runner) Cancelled(ctx context.Context, id uuid.UUID) bool {
	if r.db == nil {
		return false
	}
	var c bool
	_ = r.db.QueryRow(ctx, `SELECT cancel_requested FROM jobs WHERE id=$1`, id).Scan(&c)
	return c
}

func (r *Runner) EnqueueWait(ctx context.Context, typ string, payload, dest any) error {
	id, err := r.Enqueue(ctx, typ, payload)
	if err != nil {
		return err
	}
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			var status, lastErr string
			var result []byte
			err := r.db.QueryRow(ctx, `
				SELECT status, coalesce(last_error,''), result FROM jobs WHERE id=$1`, id).
				Scan(&status, &lastErr, &result)
			if err != nil {
				return err
			}
			switch status {
			case "completed":
				if dest != nil && len(result) > 0 && string(result) != "{}" {
					return json.Unmarshal(result, dest)
				}
				return nil
			case "failed", "cancelled":
				if lastErr == "" {
					lastErr = status
				}
				return fmt.Errorf("%w: %s", ErrJobFailed, lastErr)
			}
		}
	}
}

func (r *Runner) Do(ctx context.Context, pool ID, fn func(context.Context) error) error {
	cfg := r.config(pool)
	if !cfg.Enabled {
		return fmt.Errorf("%w: %s", ErrPoolDisabled, Name(pool))
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if !r.started.Load() {
		return fn(ctx)
	}
	rt := r.runtime(pool)
	if rt == nil {
		return fn(ctx)
	}
	item := workItem{fn: fn, ctx: ctx, done: make(chan error, 1), queuedAt: time.Now(), label: string(pool) + ".live"}
	select {
	case rt.inbox <- item:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-item.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runner) Start(ctx context.Context) {
	r.Load(ctx)
	cctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.started.Store(true)
	r.supWG.Add(1)
	go r.supervise(cctx)
	for _, id := range All() {
		r.ensureWorkers(cctx, id)
	}
}

func (r *Runner) Started() bool { return r.started.Load() }

func (r *Runner) Drain() {
	r.draining.Store(true)
	if r.cancel != nil {
		r.cancel()
	}
	r.supWG.Wait()
	for _, id := range All() {
		if rt := r.runtime(id); rt != nil {
			rt.wg.Wait()
		}
	}
	r.started.Store(false)
}

func (r *Runner) supervise(ctx context.Context) {
	defer r.supWG.Done()
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if r.draining.Load() {
				return
			}
			r.reapStale(ctx)
			for _, id := range All() {
				r.ensureWorkers(ctx, id)
			}
		}
	}
}

func (r *Runner) ensureWorkers(ctx context.Context, id ID) {
	rt := r.runtime(id)
	if rt == nil {
		return
	}
	cfg := r.config(id)
	want := cfg.MinWorkers
	if !cfg.Enabled {
		want = 0
	} else {
		q := r.queueDepth(ctx, id) + len(rt.inbox)
		need := cfg.MinWorkers + q
		if need > cfg.MaxWorkers {
			need = cfg.MaxWorkers
		}
		if need > want {
			want = need
		}
	}
	rt.target.Store(int32(want))
	live := int(rt.live.Load())
	for live < want {
		r.spawn(ctx, rt)
		live++
	}
}

func (r *Runner) spawn(ctx context.Context, rt *poolRuntime) {
	rt.wg.Add(1)
	rt.live.Add(1)
	go func() {
		defer rt.wg.Done()
		defer rt.live.Add(-1)
		defer func() {
			if rec := recover(); rec != nil {
				r.log.Error("worker recovered", "pool", rt.id, "panic", rec)
			}
		}()
		rt.serve(ctx)
	}()
}

func (rt *poolRuntime) serve(ctx context.Context) {
	for {
		if ctx.Err() != nil || rt.r.draining.Load() {
			return
		}
		select {
		case <-ctx.Done():
			return
		case item := <-rt.inbox:
			rt.runEphemeral(item)
		default:
			if rt.claimOnce(ctx) {
				continue
			}
			if rt.live.Load() > rt.target.Load() {
				return
			}
			select {
			case <-ctx.Done():
				return
			case item := <-rt.inbox:
				rt.runEphemeral(item)
			case <-time.After(400 * time.Millisecond):
			}
		}
	}
}

func (rt *poolRuntime) runEphemeral(item workItem) {
	rt.busy.Add(1)
	defer rt.busy.Add(-1)
	defer func() {
		if rec := recover(); rec != nil {
			rt.r.log.Error("live work panic", "pool", rt.id, "panic", rec)
			select {
			case item.done <- fmt.Errorf("panic: %v", rec):
			default:
			}
		}
	}()
	if item.ctx.Err() != nil {
		item.done <- item.ctx.Err()
		return
	}
	item.done <- item.fn(item.ctx)
}

func (rt *poolRuntime) claimOnce(ctx context.Context) bool {
	r := rt.r
	if r.db == nil {
		return false
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false
	}
	defer tx.Rollback(ctx)
	var job Job
	var payload []byte
	var pool string
	err = tx.QueryRow(ctx, `
		SELECT id, type, payload, attempts, pool FROM jobs
		WHERE pool=$1 AND status IN ('queued','retry') AND run_after <= now()
		  AND (locked_until IS NULL OR locked_until < now())
		ORDER BY priority DESC, created_at
		FOR UPDATE SKIP LOCKED
		LIMIT 1`, rt.id).Scan(&job.ID, &job.Type, &payload, &job.Attempts, &pool)
	if err != nil {
		return false
	}
	job.Payload = payload
	job.Pool = ID(pool)
	cfg := r.config(rt.id)
	lockSecs := cfg.TimeoutSeconds + 45
	if lockSecs < 60 {
		lockSecs = 60
	}
	wid := r.workerID + ":" + string(rt.id)
	_, err = tx.Exec(ctx, `
		UPDATE jobs SET status='running', locked_until=now() + make_interval(secs => $3), locked_by=$2,
			attempts=attempts+1, started_at=now(), updated_at=now()
		WHERE id=$1`, job.ID, wid, lockSecs)
	if err != nil {
		return false
	}
	if err := tx.Commit(ctx); err != nil {
		return false
	}
	rt.runJob(ctx, job, cfg)
	return true
}

func (rt *poolRuntime) runJob(parent context.Context, job Job, cfg PoolConfig) {
	r := rt.r
	rt.busy.Add(1)
	defer rt.busy.Add(-1)
	defer func() {
		if rec := recover(); rec != nil {
			r.log.Error("job panic", "type", job.Type, "id", job.ID, "pool", rt.id, "panic", rec)
			r.fail(parent, job, fmt.Errorf("panic: %v", rec), cfg)
		}
	}()
	h := r.handlers[job.Type]
	if h == nil {
		_, _ = r.db.Exec(parent, `UPDATE jobs SET status='failed', last_error='no handler', finished_at=now(), locked_until=NULL, updated_at=now() WHERE id=$1`, job.ID)
		jid := job.ID
		_ = oplog.Write(parent, r.db, oplog.Entry{Level: "error", Category: "job", Message: "no handler", JobID: &jid, Details: map[string]any{"type": job.Type}})
		return
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	err := h(ctx, job)
	if err == nil {
		_, _ = r.db.Exec(parent, `
			UPDATE jobs SET status='completed', progress=100, finished_at=now(), locked_until=NULL, updated_at=now()
			WHERE id=$1 AND status='running'`, job.ID)
		return
	}
	if errors.Is(err, ErrCancelled) || errors.Is(err, context.Canceled) {
		_, _ = r.db.Exec(parent, `
			UPDATE jobs SET status='cancelled', last_error=$2, finished_at=now(), locked_until=NULL, updated_at=now()
			WHERE id=$1 AND status='running' AND locked_by LIKE $3`, job.ID, errString(err), r.workerID+"%")
		return
	}
	r.log.Error("job failed", "type", job.Type, "id", job.ID, "pool", rt.id, "err", err)
	r.fail(parent, job, err, cfg)
}

func (r *Runner) fail(ctx context.Context, job Job, err error, cfg PoolConfig) {
	msg := err.Error()
	if errors.Is(err, context.DeadlineExceeded) {
		msg = fmt.Sprintf("timed out after %ds", cfg.TimeoutSeconds)
	}
	if errors.Is(err, ErrTerminal) {
		_, _ = r.db.Exec(ctx, `
			UPDATE jobs SET status='failed', last_error=$2, finished_at=now(), locked_until=NULL, updated_at=now()
			WHERE id=$1 AND status='running'`, job.ID, msg)
		writeJobLog(ctx, r.db, job, msg)
		return
	}
	_, _ = r.db.Exec(ctx, `
		UPDATE jobs SET status=CASE WHEN attempts>=max_attempts THEN 'failed' ELSE 'retry' END,
			last_error=$2, run_after=now()+interval '15 seconds', locked_until=NULL,
			finished_at=CASE WHEN attempts>=max_attempts THEN now() ELSE NULL END, updated_at=now()
		WHERE id=$1 AND status='running'`, job.ID, msg)
	var status string
	_ = r.db.QueryRow(ctx, `SELECT status FROM jobs WHERE id=$1`, job.ID).Scan(&status)
	if status == "failed" {
		writeJobLog(ctx, r.db, job, msg)
	}
}

func writeJobLog(ctx context.Context, pool *pgxpool.Pool, job Job, msg string) {
	jid := job.ID
	_ = oplog.Write(ctx, pool, oplog.Entry{
		Level:    "error",
		Category: "job",
		Message:  msg,
		JobID:    &jid,
		Details:  map[string]any{"type": job.Type},
	})
}

func errString(err error) string {
	if err == nil {
		return "cancelled"
	}
	return err.Error()
}

func (r *Runner) reapStale(ctx context.Context) {
	if r.db == nil {
		return
	}
	_, _ = r.db.Exec(ctx, `
		UPDATE jobs SET status=CASE WHEN attempts>=max_attempts THEN 'failed' ELSE 'retry' END,
			last_error=coalesce(nullif(last_error,''), 'timed out or worker lost'),
			run_after=now()+interval '15 seconds', locked_until=NULL,
			finished_at=CASE WHEN attempts>=max_attempts THEN now() ELSE finished_at END,
			updated_at=now()
		WHERE status='running' AND locked_until < now()`)
}

func (r *Runner) queueDepth(ctx context.Context, id ID) int {
	if r.db == nil {
		return 0
	}
	var n int
	_ = r.db.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE pool=$1 AND status IN ('queued','retry')`, id).Scan(&n)
	return n
}

type LiveStats struct {
	ActiveWorkers int        `json:"active_workers"`
	Busy          int        `json:"busy"`
	Idle          int        `json:"idle"`
	QueueDepth    int        `json:"queue_depth"`
	Running       int        `json:"running"`
	Failed        int        `json:"failed"`
	AvgDurationMS int        `json:"avg_duration_ms"`
	OldestQueued  *time.Time `json:"oldest_queued_at"`
	Ephemeral     int        `json:"ephemeral"`
}

type PoolStatus struct {
	PoolInfo
	PoolConfig
	Live LiveStats `json:"live"`
}

type JobRow struct {
	ID          uuid.UUID  `json:"id"`
	Type        string     `json:"type"`
	Pool        string     `json:"pool"`
	Status      string     `json:"status"`
	Progress    int        `json:"progress"`
	Attempts    int        `json:"attempts"`
	LastError   *string    `json:"last_error"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Cancellable bool       `json:"cancellable"`
}

func (r *Runner) Status(ctx context.Context) []PoolStatus {
	infos := Infos()
	out := make([]PoolStatus, 0, len(infos))
	for _, info := range infos {
		cfg := r.config(info.ID)
		rt := r.runtime(info.ID)
		liveN, busyN := 0, 0
		ephem := 0
		if rt != nil {
			liveN = int(rt.live.Load())
			busyN = int(rt.busy.Load())
			ephem = len(rt.inbox)
		}
		idle := liveN - busyN
		if idle < 0 {
			idle = 0
		}
		st := PoolStatus{PoolInfo: info, PoolConfig: cfg, Live: LiveStats{
			ActiveWorkers: liveN,
			Busy:          busyN,
			Idle:          idle,
			Ephemeral:     ephem,
		}}
		if r.db != nil {
			_ = r.db.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE pool=$1 AND status IN ('queued','retry')`, info.ID).Scan(&st.Live.QueueDepth)
			st.Live.QueueDepth += ephem
			_ = r.db.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE pool=$1 AND status='running'`, info.ID).Scan(&st.Live.Running)
			_ = r.db.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE pool=$1 AND status='failed' AND updated_at > now() - interval '24 hours'`, info.ID).Scan(&st.Live.Failed)
			var avg *float64
			_ = r.db.QueryRow(ctx, `
				SELECT avg(extract(epoch from (finished_at - started_at)) * 1000)
				FROM jobs WHERE pool=$1 AND status='completed' AND started_at IS NOT NULL AND finished_at IS NOT NULL
				  AND finished_at > now() - interval '24 hours'`, info.ID).Scan(&avg)
			if avg != nil {
				st.Live.AvgDurationMS = int(*avg)
			}
			var oldest *time.Time
			_ = r.db.QueryRow(ctx, `
				SELECT min(created_at) FROM jobs WHERE pool=$1 AND status IN ('queued','retry')`, info.ID).Scan(&oldest)
			st.Live.OldestQueued = oldest
		} else {
			st.Live.QueueDepth = ephem
		}
		out = append(out, st)
	}
	return out
}

func (r *Runner) RecentJobs(ctx context.Context, limit int) []JobRow {
	if r.db == nil || limit <= 0 {
		return []JobRow{}
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, type, pool, status, progress, attempts, last_error, created_at, started_at, updated_at
		FROM jobs ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return []JobRow{}
	}
	defer rows.Close()
	out := []JobRow{}
	for rows.Next() {
		var j JobRow
		if err := rows.Scan(&j.ID, &j.Type, &j.Pool, &j.Status, &j.Progress, &j.Attempts, &j.LastError, &j.CreatedAt, &j.StartedAt, &j.UpdatedAt); err != nil {
			continue
		}
		out = append(out, r.decorateJob(ctx, j))
	}
	return out
}

func (r *Runner) decorateJob(ctx context.Context, j JobRow) JobRow {
	extra := r.cancelExtra(ctx, j.Type, j.ID)
	if j.Type == "stats.rebuild" && strings.EqualFold(j.Status, "running") {
		extra.StatsSwapStarted = true
	}
	j.Cancellable = AllowCancel(j.Type, j.Status, j.Progress, extra)
	return j
}

func (r *Runner) RunningJobs(ctx context.Context) []JobRow {
	if r.db == nil {
		return []JobRow{}
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, type, pool, status, progress, attempts, last_error, created_at, started_at, updated_at
		FROM jobs WHERE status='running' ORDER BY started_at NULLS LAST, created_at`)
	if err != nil {
		return []JobRow{}
	}
	defer rows.Close()
	out := []JobRow{}
	for rows.Next() {
		var j JobRow
		if err := rows.Scan(&j.ID, &j.Type, &j.Pool, &j.Status, &j.Progress, &j.Attempts, &j.LastError, &j.CreatedAt, &j.StartedAt, &j.UpdatedAt); err != nil {
			continue
		}
		out = append(out, r.decorateJob(ctx, j))
	}
	return out
}
