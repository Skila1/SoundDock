package listen

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Event struct {
	UserID     uuid.UUID
	TrackID    uuid.UUID
	PositionMs int
	DurationMs int
	Source     string
	Kind       string
	StopAfter  bool
}

type playState struct {
	trackID uuid.UUID
	counted bool
	lastPos int
}

type Writer struct {
	pool *pgxpool.Pool
	mu   sync.Mutex
	cur  map[string]playState
}

func New(pool *pgxpool.Pool) *Writer {
	return &Writer{pool: pool, cur: map[string]playState{}}
}

var writers sync.Map

func For(pool *pgxpool.Pool) *Writer {
	if pool == nil {
		return New(nil)
	}
	if v, ok := writers.Load(pool); ok {
		return v.(*Writer)
	}
	w := New(pool)
	actual, _ := writers.LoadOrStore(pool, w)
	return actual.(*Writer)
}

func (w *Writer) Record(ctx context.Context, ev Event) error {
	src, ok := NormalizeSource(ev.Source)
	if !ok {
		return ErrSource
	}
	ev.Source = src
	key := ev.UserID.String() + "|" + ev.Source
	w.mu.Lock()
	st := w.cur[key]
	st, act := decide(st, ev)
	w.cur[key] = st
	w.mu.Unlock()
	switch act {
	case actionPlay:
		return w.insertPlay(ctx, ev)
	case actionSkip:
		return w.insertSkip(ctx, ev)
	default:
		return nil
	}
}

func (w *Writer) insertPlay(ctx context.Context, ev Event) error {
	if w.pool == nil {
		return nil
	}
	if _, err := w.pool.Exec(ctx, `
		INSERT INTO listen_history (user_id, track_id, duration_ms, source)
		VALUES ($1,$2,$3,$4)`, ev.UserID, ev.TrackID, ev.DurationMs, ev.Source); err != nil {
		return err
	}
	_, err := w.pool.Exec(ctx, `
		INSERT INTO play_counts (user_id, track_id, count, last_played_at)
		VALUES ($1,$2,1,now())
		ON CONFLICT (user_id, track_id) DO UPDATE
		SET count = play_counts.count + 1, last_played_at = now()`, ev.UserID, ev.TrackID)
	return err
}

func (w *Writer) insertSkip(ctx context.Context, ev Event) error {
	if w.pool == nil {
		return nil
	}
	_, err := w.pool.Exec(ctx, `
		INSERT INTO play_counts (user_id, track_id, skip_count)
		VALUES ($1,$2,1)
		ON CONFLICT (user_id, track_id) DO UPDATE
		SET skip_count = play_counts.skip_count + 1`, ev.UserID, ev.TrackID)
	return err
}
