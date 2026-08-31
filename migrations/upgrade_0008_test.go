package migrations_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	if dsn := os.Getenv("SD_TEST_MIGRATE_URL"); dsn != "" {
		return dsn
	}
	return "postgres://sounddock:sounddock@127.0.0.1:55432/sounddock_mig?sslmode=disable"
}

func openPool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	required := os.Getenv("SD_TEST_DATABASE_URL") != ""
	wait := 2 * time.Second
	if required {
		wait = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
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
		time.Sleep(200 * time.Millisecond)
	}
	if !required {
		t.Skipf("postgres not running (set SD_TEST_DATABASE_URL to require it): %v", last)
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
	if v != 25 {
		t.Fatalf("version %d", v)
	}
}

func TestUpgrade0019DiscordAvatar(t *testing.T) {
	dsn := testDSN(t)
	pool := openPool(t, dsn)
	defer pool.Close()
	resetDB(t, pool)
	m := migrator(t, dsn)
	if err := m.Migrate(18); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate to 18: %v", err)
	}
	ctx := context.Background()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name='user_identities' AND column_name='avatar_hash'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("avatar_hash should be missing at 18, got %d", n)
	}
	if err := m.Migrate(19); err != nil {
		t.Fatalf("migrate to 19: %v", err)
	}
	var dataType, isNullable string
	if err := pool.QueryRow(ctx, `
		SELECT data_type, is_nullable FROM information_schema.columns
		WHERE table_name='user_identities' AND column_name='avatar_hash'`).Scan(&dataType, &isNullable); err != nil {
		t.Fatal(err)
	}
	if dataType != "text" || isNullable != "YES" {
		t.Fatalf("avatar_hash %s nullable=%s", dataType, isNullable)
	}
}

func TestUpgrade0020BackupsDestination(t *testing.T) {
	dsn := testDSN(t)
	pool := openPool(t, dsn)
	defer pool.Close()
	resetDB(t, pool)
	m := migrator(t, dsn)
	if err := m.Migrate(19); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate to 19: %v", err)
	}
	ctx := context.Background()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name='backups' AND column_name='destination'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("destination should be missing at 19, got %d", n)
	}
	if err := m.Migrate(20); err != nil {
		t.Fatalf("migrate to 20: %v", err)
	}
	var dest, kind, remote string
	if err := pool.QueryRow(ctx, `
		SELECT column_default FROM information_schema.columns
		WHERE table_name='backups' AND column_name='destination'`).Scan(&dest); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dest, "local") {
		t.Fatalf("destination default %s", dest)
	}
	if err := pool.QueryRow(ctx, `
		SELECT column_default FROM information_schema.columns
		WHERE table_name='backups' AND column_name='kind'`).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(kind, "sql") {
		t.Fatalf("kind default %s", kind)
	}
	if err := pool.QueryRow(ctx, `
		SELECT column_default FROM information_schema.columns
		WHERE table_name='backups' AND column_name='remote_key'`).Scan(&remote); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(remote, "''") && remote != "''::text" {
		t.Fatalf("remote_key default %s", remote)
	}
}

func TestUpgrade0021DedupeNullInstanceQualify(t *testing.T) {
	dsn := testDSN(t)
	pool := openPool(t, dsn)
	defer pool.Close()
	resetDB(t, pool)
	m := migrator(t, dsn)
	if err := m.Migrate(20); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate to 20: %v", err)
	}
	ctx := context.Background()
	uid := "00000000-0000-4000-8000-000000000001"
	tid := "00000000-0000-4000-8000-000000000050"
	inst := "00000000-0000-4000-8000-0000000000aa"
	lib := "00000000-0000-4000-8000-000000000010"
	sid := "00000000-0000-4000-8000-00000000000a"
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type) VALUES ($1,'w0','managed')`, sid); err != nil {
		t.Fatalf("storage: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, storage_provider_id) VALUES ($1,'w0',$2)`, lib, sid); err != nil {
		t.Fatalf("library: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,'w0','x','w0')`, uid); err != nil {
		t.Fatalf("user: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title) VALUES ($1,$2,'t')`, tid, lib); err != nil {
		t.Fatalf("track: %v", err)
	}
	when := "2024-01-01T00:00:00Z"
	_, err := pool.Exec(ctx, `
		INSERT INTO listen_events (user_id, track_id, kind, qualified_play, legacy_backfill, source, started_at)
		VALUES
			($1,$2,'qualify',true,true,'web',$3::timestamptz),
			($1,$2,'qualify',true,true,'web',$3::timestamptz),
			($1,$2,'qualify',true,false,'web',$3::timestamptz)`, uid, tid, when)
	if err != nil {
		t.Fatalf("seed dups: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO listen_events (playback_instance_id, user_id, track_id, kind, qualified_play, source, started_at)
		VALUES ($1,$2,$3,'qualify',true,'web',$4::timestamptz)`, inst, uid, tid, when)
	if err != nil {
		t.Fatalf("seed live: %v", err)
	}
	if err := m.Migrate(21); err != nil {
		t.Fatalf("migrate to 21: %v", err)
	}
	var nullN, liveN int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM listen_events
		WHERE kind='qualify' AND playback_instance_id IS NULL AND user_id=$1 AND track_id=$2`, uid, tid).Scan(&nullN); err != nil {
		t.Fatal(err)
	}
	if nullN != 1 {
		t.Fatalf("null-instance qualify count %d", nullN)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM listen_events
		WHERE kind='qualify' AND playback_instance_id=$1`, inst).Scan(&liveN); err != nil {
		t.Fatal(err)
	}
	if liveN != 1 {
		t.Fatalf("live qualify count %d", liveN)
	}
}

func TestUpgrade0022MediaHoldsThen0023Logs(t *testing.T) {
	dsn := testDSN(t)
	pool := openPool(t, dsn)
	defer pool.Close()
	resetDB(t, pool)
	m := migrator(t, dsn)
	if err := m.Migrate(22); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate to 22: %v", err)
	}
	ctx := context.Background()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_name='media_holds'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("media_holds %v %d", err, n)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_name='managed_cleanup_items'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("managed_cleanup_items %v %d", err, n)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_name='operational_logs'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("operational_logs must wait for 0023: %v %d", err, n)
	}
	if err := m.Migrate(23); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate to 23: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_name='operational_logs'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("operational_logs %v %d", err, n)
	}
}
