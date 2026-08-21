package jobs

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler func(ctx context.Context, job Job) error

type Job struct {
	ID       uuid.UUID
	Type     string
	Payload  json.RawMessage
	Progress int
	Attempts int
}

type Runner struct {
	pool     *pgxpool.Pool
	handlers map[string]Handler
	workerID string
	log      *slog.Logger
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	draining bool
	mu       sync.Mutex
}

func New(pool *pgxpool.Pool, log *slog.Logger) *Runner {
	return &Runner{pool: pool, handlers: map[string]Handler{}, workerID: uuid.NewString(), log: log}
}

func (r *Runner) Register(typ string, h Handler) { r.handlers[typ] = h }

func (r *Runner) Enqueue(ctx context.Context, typ string, payload any) (uuid.UUID, error) {
	b, _ := json.Marshal(payload)
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `INSERT INTO jobs (type, payload) VALUES ($1,$2) RETURNING id`, typ, b).Scan(&id)
	return id, err
}

func (r *Runner) SetProgress(ctx context.Context, id uuid.UUID, p int) {
	_, _ = r.pool.Exec(ctx, `UPDATE jobs SET progress=$2, updated_at=now() WHERE id=$1`, id, p)
}

func (r *Runner) RequestCancel(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE jobs SET cancel_requested=true WHERE id=$1 AND status IN ('queued','running','retry')`, id)
	return err
}

func (r *Runner) Cancelled(ctx context.Context, id uuid.UUID) bool {
	var c bool
	_ = r.pool.QueryRow(ctx, `SELECT cancel_requested FROM jobs WHERE id=$1`, id).Scan(&c)
	return c
}

func (r *Runner) Start(ctx context.Context, n int) {
	cctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	for i := 0; i < n; i++ {
		r.wg.Add(1)
		go r.loop(cctx)
	}
}

func (r *Runner) Drain() {
	r.mu.Lock()
	r.draining = true
	r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
}

func (r *Runner) loop(ctx context.Context) {
	defer r.wg.Done()
	t := time.NewTicker(800 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.mu.Lock()
			d := r.draining
			r.mu.Unlock()
			if d {
				return
			}
			r.claimOnce(ctx)
		}
	}
}

func (r *Runner) claimOnce(ctx context.Context) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)
	var job Job
	var payload []byte
	err = tx.QueryRow(ctx, `
		SELECT id, type, payload, attempts FROM jobs
		WHERE status IN ('queued','retry') AND run_after <= now()
		  AND (locked_until IS NULL OR locked_until < now())
		ORDER BY created_at
		FOR UPDATE SKIP LOCKED
		LIMIT 1`).Scan(&job.ID, &job.Type, &payload, &job.Attempts)
	if err != nil {
		return
	}
	job.Payload = payload
	_, err = tx.Exec(ctx, `UPDATE jobs SET status='running', locked_until=now()+interval '15 minutes', locked_by=$2, attempts=attempts+1, updated_at=now() WHERE id=$1`, job.ID, r.workerID)
	if err != nil {
		return
	}
	if err := tx.Commit(ctx); err != nil {
		return
	}
	h := r.handlers[job.Type]
	if h == nil {
		_, _ = r.pool.Exec(ctx, `UPDATE jobs SET status='failed', last_error='no handler', updated_at=now() WHERE id=$1`, job.ID)
		return
	}
	err = h(ctx, job)
	if err != nil {
		r.log.Error("job failed", "type", job.Type, "id", job.ID, "err", err)
		_, _ = r.pool.Exec(ctx, `
			UPDATE jobs SET status=CASE WHEN attempts>=max_attempts THEN 'failed' ELSE 'retry' END,
			last_error=$2, run_after=now()+interval '15 seconds', locked_until=NULL, updated_at=now()
			WHERE id=$1`, job.ID, err.Error())
		return
	}
	_, _ = r.pool.Exec(ctx, `UPDATE jobs SET status='completed', progress=100, locked_until=NULL, updated_at=now() WHERE id=$1`, job.ID)
}
