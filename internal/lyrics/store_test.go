package lyrics

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
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
	t.Cleanup(pool.Close)
	return pool
}

func seedTrack(t *testing.T, pool *pgxpool.Pool) (trackID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	sid, libID, trackID := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc)
		VALUES ($1, $2, 'local', $3)`, sid, "ly-"+sid.String()[:8], []byte(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id, root_prefix, read_only)
		VALUES ($1, $2, 'music', $3, '', false)`, libID, "ly-"+libID.String()[:8], sid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title, duration_ms)
		VALUES ($1, $2, 'Lyric Song', 120000)`, trackID, libID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM lyrics WHERE track_id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, libID)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, sid)
	})
	return trackID
}

func TestGetLyricsEmbeddedFromDBNoNetwork(t *testing.T) {
	pool := testPool(t)
	id := seedTrack(t, pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO lyrics (track_id, source, timed, body)
		VALUES ($1, 'embedded', false, 'from file tags')`, id); err != nil {
		t.Fatal(err)
	}
	s := New(pool, nil)
	s.fetchFn = func(context.Context, string, Meta) (string, bool, error) {
		t.Fatal("must not fetch when embedded lyrics exist")
		return "", false, nil
	}
	s.urlFn = func(context.Context) string { return "https://lrclib.net" }
	got := s.GetLyrics(ctx, Meta{Title: "Lyric Song", Artist: "A", TrackID: id, DurationMS: 120000})
	if got.Source != SourceEmbedded || got.Body != "from file tags" {
		t.Fatalf("got %+v", got)
	}
}

func TestSaveProviderDoesNotOverwriteManual(t *testing.T) {
	pool := testPool(t)
	id := seedTrack(t, pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO lyrics (track_id, source, timed, body)
		VALUES ($1, 'manual', false, 'keep me')`, id); err != nil {
		t.Fatal(err)
	}
	s := New(pool, nil)
	if err := s.saveProvider(ctx, id, SourceLRCLIB, "from provider", false); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM lyrics WHERE track_id=$1 AND source='lrclib'`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("lrclib rows=%d want 0", n)
	}
	var body string
	if err := pool.QueryRow(ctx, `SELECT body FROM lyrics WHERE track_id=$1 AND source='manual'`, id).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if body != "keep me" {
		t.Fatalf("manual body %q", body)
	}
}

func TestStoreConfigRoundTrip(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	var prev []byte
	had := pool.QueryRow(ctx, `SELECT value FROM server_settings WHERE key=$1`, SettingKey).Scan(&prev) == nil
	t.Cleanup(func() {
		c := context.Background()
		if had {
			_, _ = pool.Exec(c, `
				INSERT INTO server_settings (key, value) VALUES ($1, $2::jsonb)
				ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, SettingKey, prev)
			return
		}
		_, _ = pool.Exec(c, `DELETE FROM server_settings WHERE key=$1`, SettingKey)
	})
	if err := StoreConfig(ctx, pool, Config{Enabled: true, ProviderURL: "https://lrclib.net"}); err != nil {
		t.Fatal(err)
	}
	got := LoadConfig(ctx, pool)
	if !got.Enabled || got.ProviderURL != "https://lrclib.net" {
		t.Fatalf("got %+v", got)
	}
	if err := StoreConfig(ctx, pool, Config{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	got = LoadConfig(ctx, pool)
	if got.Enabled || got.ProviderURL != "" {
		t.Fatalf("disabled %+v", got)
	}
}
