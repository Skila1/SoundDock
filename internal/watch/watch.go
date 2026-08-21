package watch

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/scan"
	"github.com/sounddock/sounddock/internal/storage"
)

const (
	SettingWatch      = "watch_enabled"
	SettingAutoRescan = "auto_rescan_enabled"
	SettingInbox      = "inbox_watch_enabled"
)

type Watcher struct {
	pool    *pgxpool.Pool
	scanner *scan.Scanner
	getProv func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)
	log     *slog.Logger
	every   time.Duration
}

func New(pool *pgxpool.Pool, scanner *scan.Scanner, getProv func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error), log *slog.Logger) *Watcher {
	return &Watcher{pool: pool, scanner: scanner, getProv: getProv, log: log, every: 30 * time.Second}
}

func boolSetting(ctx context.Context, pool *pgxpool.Pool, key string, def bool) bool {
	if pool == nil {
		return def
	}
	var v bool
	err := pool.QueryRow(ctx, `SELECT (value)::boolean FROM server_settings WHERE key=$1`, key).Scan(&v)
	if err != nil {
		return def
	}
	return v
}

func WatchEnabled(ctx context.Context, pool *pgxpool.Pool) bool {
	return boolSetting(ctx, pool, SettingWatch, false)
}

func AutoRescanEnabled(ctx context.Context, pool *pgxpool.Pool) bool {
	return boolSetting(ctx, pool, SettingAutoRescan, false)
}

func InboxEnabled(ctx context.Context, pool *pgxpool.Pool) bool {
	return boolSetting(ctx, pool, SettingInbox, false)
}

func (w *Watcher) Run(ctx context.Context) {
	if w == nil || w.pool == nil {
		return
	}
	if w.every <= 0 {
		w.every = 30 * time.Second
	}
	t := time.NewTicker(w.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

func (w *Watcher) tick(ctx context.Context) {
	if !WatchEnabled(ctx, w.pool) && !AutoRescanEnabled(ctx, w.pool) && !InboxEnabled(ctx, w.pool) {
		return
	}
	rows, err := w.pool.Query(ctx, `SELECT id, read_only FROM libraries`)
	if err != nil {
		return
	}
	defer rows.Close()
	type lib struct {
		id uuid.UUID
		ro bool
	}
	var libs []lib
	for rows.Next() {
		var l lib
		if err := rows.Scan(&l.id, &l.ro); err == nil {
			libs = append(libs, l)
		}
	}
	for _, l := range libs {
		if l.ro {
			continue
		}
		if w.getProv == nil || w.scanner == nil {
			continue
		}
		prov, libID, prefix, err := w.getProv(ctx, l.id)
		if err != nil {
			continue
		}
		if AutoRescanEnabled(ctx, w.pool) {
			_ = w.scanner.ScanLibrary(ctx, libID, prov, prefix, "watch", uuid.Nil)
			continue
		}
		if InboxEnabled(ctx, w.pool) {
			inbox := strings.TrimSuffix(prefix, "/")
			if inbox != "" {
				inbox += "/"
			}
			inbox += "inbox"
			_ = w.scanner.ScanLibrary(ctx, libID, prov, inbox, "inbox", uuid.Nil)
		}
	}
}
