package oplog

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Writer stores activity entries asynchronously in batches so request
// handlers never wait on the log table. When the buffer is full new entries
// are dropped and counted rather than blocking.
type Writer struct {
	pool    *pgxpool.Pool
	log     *slog.Logger
	ch      chan Entry
	dropped atomic.Int64
	written atomic.Int64

	flushReq chan chan struct{}
	stop     chan struct{}
	done     chan struct{}
	once     sync.Once
	started  atomic.Bool
}

const (
	writerBuffer   = 8192
	writerBatch    = 200
	writerInterval = 750 * time.Millisecond
)

// NewWriter returns a writer for pool. log must not be a logger that tees
// back into this writer; it reports storage failures.
func NewWriter(pool *pgxpool.Pool, log *slog.Logger) *Writer {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Writer{
		pool:     pool,
		log:      log,
		ch:       make(chan Entry, writerBuffer),
		flushReq: make(chan chan struct{}),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start runs the background flusher until ctx ends or Close is called.
func (w *Writer) Start(ctx context.Context) {
	if w == nil || !w.started.CompareAndSwap(false, true) {
		return
	}
	go w.loop(ctx)
}

// Enqueue queues e without blocking. The entry is stamped now so batching
// does not skew timestamps.
func (w *Writer) Enqueue(e Entry) {
	if w == nil {
		return
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	select {
	case w.ch <- e:
	default:
		w.dropped.Add(1)
	}
}

// Dropped returns how many entries were discarded because the buffer was full.
func (w *Writer) Dropped() int64 {
	if w == nil {
		return 0
	}
	return w.dropped.Load()
}

// Written returns how many entries were stored.
func (w *Writer) Written() int64 {
	if w == nil {
		return 0
	}
	return w.written.Load()
}

// Flush blocks until everything queued so far is stored (or ctx ends).
func (w *Writer) Flush(ctx context.Context) {
	if w == nil {
		return
	}
	if !w.started.Load() {
		w.drain(ctx)
		return
	}
	ack := make(chan struct{})
	select {
	case w.flushReq <- ack:
	case <-ctx.Done():
		return
	case <-w.done:
		return
	}
	select {
	case <-ack:
	case <-ctx.Done():
	}
}

// Close flushes and stops the writer.
func (w *Writer) Close() {
	if w == nil {
		return
	}
	w.once.Do(func() {
		if !w.started.Load() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			w.drain(ctx)
			cancel()
			return
		}
		close(w.stop)
		<-w.done
	})
}

func (w *Writer) loop(ctx context.Context) {
	defer close(w.done)
	tick := time.NewTicker(writerInterval)
	defer tick.Stop()
	batch := make([]Entry, 0, writerBatch)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		fctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		w.store(fctx, batch)
		cancel()
		batch = batch[:0]
	}
	collect := func() {
		for {
			select {
			case e := <-w.ch:
				batch = append(batch, e)
				if len(batch) >= writerBatch {
					flush()
				}
			default:
				return
			}
		}
	}
	for {
		select {
		case e := <-w.ch:
			batch = append(batch, e)
			if len(batch) >= writerBatch {
				flush()
			}
		case <-tick.C:
			flush()
		case ack := <-w.flushReq:
			collect()
			flush()
			close(ack)
		case <-w.stop:
			collect()
			flush()
			return
		case <-ctx.Done():
			collect()
			flush()
			return
		}
	}
}

func (w *Writer) drain(ctx context.Context) {
	batch := []Entry{}
	for {
		select {
		case e := <-w.ch:
			batch = append(batch, e)
		default:
			w.store(ctx, batch)
			return
		}
	}
}

func (w *Writer) store(ctx context.Context, entries []Entry) {
	if len(entries) == 0 || w.pool == nil {
		return
	}
	b := &pgx.Batch{}
	for _, e := range entries {
		p, raw := prepare(e)
		b.Queue(insertSQL, insertArgs(p, raw)...)
	}
	res := w.pool.SendBatch(ctx, b)
	failed := 0
	var firstErr error
	for range entries {
		if _, err := res.Exec(); err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	_ = res.Close()
	w.written.Add(int64(len(entries) - failed))
	if firstErr != nil && !isUndefinedTable(firstErr) {
		w.log.Warn("activity log write failed", "failed", failed, "err", Redact(firstErr.Error()))
	}
}

var defaultWriter atomic.Pointer[Writer]

// SetDefault installs the process-wide writer used by Emit.
func SetDefault(w *Writer) { defaultWriter.Store(w) }

// Default returns the process-wide writer, or nil.
func Default() *Writer { return defaultWriter.Load() }

// Emit records a domain event asynchronously through the default writer.
// Request id, IP and actor are taken from ctx when not set. It is a no-op
// when no writer is installed (tests, tools).
func Emit(ctx context.Context, e Entry) {
	w := Default()
	if w == nil {
		return
	}
	w.Enqueue(fillFromContext(ctx, e))
}

// TryNext pops one queued entry without storing it. It exists for tests and
// tools that inspect a writer that has not been started.
func TryNext(w *Writer) (Entry, bool) {
	if w == nil {
		return Entry{}, false
	}
	select {
	case e := <-w.ch:
		return e, true
	default:
		return Entry{}, false
	}
}
