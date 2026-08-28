package testdb

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/migrations"
)

var (
	migrateOnce sync.Once
	migrateErr  error
)

// Open connects to SD_TEST_DATABASE_URL and applies shipped migrations once.
func Open(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("SD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SD_TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skip(err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skip(err)
	}
	migrateOnce.Do(func() {
		migrateErr = apply(dsn)
	})
	if migrateErr != nil {
		pool.Close()
		t.Fatalf("test schema: %v", migrateErr)
	}
	t.Cleanup(pool.Close)
	return pool
}

func apply(dsn string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `SELECT pg_advisory_lock(829145)`); err != nil {
		return err
	}
	defer func() { _, _ = pool.Exec(context.Background(), `SELECT pg_advisory_unlock(829145)`) }()

	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return err
	}
	var last error
	for i := 0; i < 5; i++ {
		m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
		if err != nil {
			return err
		}
		err = m.Up()
		_, _ = m.Close()
		if err == nil || err == migrate.ErrNoChange {
			return nil
		}
		last = err
		time.Sleep(time.Duration(i+1) * 200 * time.Millisecond)
	}
	return last
}
