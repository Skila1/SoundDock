package migrations_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/migrations"
)

func testDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("SD_TEST_DATABASE_URL"); dsn != "" {
		return dsn
	}
	return "postgres://sounddock:sounddock@127.0.0.1:55432/sounddock_w0?sslmode=disable"
}

func openPool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var last error
	for ctx.Err() == nil {
		pool, err := pgxpool.New(ctx, dsn)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				return pool
			}
			pool.Close()
			last = err
		} else {
			last = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("postgres not ready: %v", last)
	return nil
}

func migrator(t *testing.T, dsn string) *migrate.Migrate {
	t.Helper()
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = m.Close() })
	return m
}

func resetDB(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`)
	if err != nil {
		t.Fatal(err)
	}
}

func seedPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "testdata", "fixture-0.0.10", "seed.sql")
}

func TestUpgrade0008From010Fixture(t *testing.T) {
	dsn := testDSN(t)
	pool := openPool(t, dsn)
	defer pool.Close()
	resetDB(t, pool)

	m := migrator(t, dsn)
	if err := m.Migrate(7); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate to 7: %v", err)
	}

	sql, err := os.ReadFile(seedPath(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), string(sql)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := m.Migrate(8); err != nil {
		t.Fatalf("migrate to 8: %v", err)
	}

	ctx := context.Background()
	var key, quality, title, kind, owner string
	var reorg bool
	err = pool.QueryRow(ctx, `
		SELECT tf.storage_key, tf.quality, t.title, l.allow_physical_reorganise
		FROM track_files tf JOIN tracks t ON t.id=tf.track_id JOIN libraries l ON l.id=tf.library_id
		WHERE t.id='00000000-0000-4000-8000-000000000050'`).Scan(&key, &quality, &title, &reorg)
	if err != nil {
		t.Fatal(err)
	}
	if key != "uploads/aa/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.flac" {
		t.Fatalf("storage_key %q", key)
	}
	if quality != "original" {
		t.Fatalf("quality %q", quality)
	}
	if title != "Numb" {
		t.Fatalf("title %q", title)
	}
	if err := pool.QueryRow(ctx, `SELECT title FROM tracks WHERE id='00000000-0000-4000-8000-000000000051'`).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "In The End" {
		t.Fatalf("filename-parsed title %q", title)
	}
	if reorg {
		t.Fatal("virtual library must not allow physical reorganise")
	}

	err = pool.QueryRow(ctx, `SELECT kind, owner_key FROM playback_sessions WHERE id='00000000-0000-4000-8000-000000000070'`).Scan(&kind, &owner)
	if err != nil {
		t.Fatal(err)
	}
	if kind != "web_device" || owner != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("web session %s %s", kind, owner)
	}
	err = pool.QueryRow(ctx, `SELECT kind, owner_key FROM playback_sessions WHERE id='00000000-0000-4000-8000-000000000071'`).Scan(&kind, &owner)
	if err != nil {
		t.Fatal(err)
	}
	if kind != "discord_guild" || owner != "111111111111111111" {
		t.Fatalf("discord session %s %s", kind, owner)
	}

	var skip int
	if err := pool.QueryRow(ctx, `SELECT skip_count FROM play_counts LIMIT 1`).Scan(&skip); err == nil {
		t.Fatal("expected no play_counts rows")
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name='playback_sessions' AND column_name='device_id'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("device_id column missing: %v %d", err, n)
	}
}

func TestMigrateUpEmpty(t *testing.T) {
	dsn := testDSN(t)
	pool := openPool(t, dsn)
	defer pool.Close()
	resetDB(t, pool)
	m := migrator(t, dsn)
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("up: %v", err)
	}
	var v int
	if err := pool.QueryRow(context.Background(), `SELECT version FROM schema_migrations`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != 8 {
		t.Fatalf("version %d", v)
	}
}
