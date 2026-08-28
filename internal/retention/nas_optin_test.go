package retention

import (
	"context"
	"os"
	"path/filepath"
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

// TestRetentionOptInNASFileRemainsOnDisk is the Wave 0 contract: opting a NAS
// library into retention may drop catalogue media rows, but must not physically
// delete non-managed (local/NAS/S3) files.
func TestRetentionOptInNASFileRemainsOnDisk(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	nasDir := t.TempDir()
	key := "album/track.flac"
	abs := filepath.Join(nasDir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("nas-audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	sid, libID, trackID := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc)
		VALUES ($1, $2, 'local', $3)`, sid, "nas-"+sid.String()[:8], []byte(nasDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id, root_prefix, read_only, retention_opt_in)
		VALUES ($1, $2, 'music', $3, '', false, true)`, libID, "NAS "+libID.String()[:8], sid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title, duration_ms, acquisition, created_at)
		VALUES ($1, $2, 'NAS Song', 1000, 'youtube', now() - interval '30 days')`, trackID, libID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO track_files (track_id, library_id, storage_key, size_bytes, quality)
		VALUES ($1, $2, $3, 9, 'original')`, trackID, libID, key); err != nil {
		t.Fatal(err)
	}

	var old []byte
	_ = pool.QueryRow(ctx, `SELECT value FROM server_settings WHERE key=$1`, SettingKey).Scan(&old)
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM retention_events WHERE track_id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM track_files WHERE track_id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, libID)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, sid)
		if len(old) > 0 {
			_, _ = pool.Exec(c, `INSERT INTO server_settings (key, value) VALUES ($1, $2::jsonb)
				ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, SettingKey, old)
		} else {
			_, _ = pool.Exec(c, `DELETE FROM server_settings WHERE key=$1`, SettingKey)
		}
	})

	if err := SavePolicy(ctx, pool, Policy{
		Enabled: true, Mode: ModeAge, AgeDays: 1, BatchSize: 50, IntervalMinutes: 1, DryRun: false,
	}); err != nil {
		t.Fatal(err)
	}

	e := New(pool, nil, nil, t.TempDir(), func(ctx context.Context, id uuid.UUID) (int64, error) {
		var typ, storageKey string
		var root []byte
		err := pool.QueryRow(ctx, `
			SELECT sp.type, tf.storage_key, sp.config_enc
			FROM track_files tf
			JOIN libraries l ON l.id = tf.library_id
			JOIN storage_providers sp ON sp.id = l.storage_provider_id
			WHERE tf.track_id=$1 AND tf.deleted_at IS NULL
			LIMIT 1`, id).Scan(&typ, &storageKey, &root)
		if err != nil {
			return 0, err
		}
		if _, err := pool.Exec(ctx, `DELETE FROM track_files WHERE track_id=$1`, id); err != nil {
			return 0, err
		}
		if _, err := pool.Exec(ctx, `
			UPDATE tracks SET media_unavailable_at=COALESCE(media_unavailable_at, now()), updated_at=now()
			WHERE id=$1`, id); err != nil {
			return 0, err
		}
		if typ == "managed" {
			_ = os.Remove(filepath.Join(string(root), filepath.FromSlash(storageKey)))
			return 9, nil
		}
		return 0, nil
	})

	policy := LoadPolicy(ctx, pool)
	cands, err := e.candidates(ctx, policy, 500)
	if err != nil {
		t.Fatal(err)
	}
	var found *Candidate
	for i := range cands {
		if cands[i].ID == trackID {
			found = &cands[i]
			break
		}
	}
	if found == nil {
		t.Fatal("NAS library with retention_opt_in should be eligible")
	}
	if _, err := e.purgeOne(ctx, *found); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("NAS file must remain on disk after retention with opt_in true: %v", err)
	}
	var unavailable bool
	if err := pool.QueryRow(ctx, `SELECT media_unavailable_at IS NOT NULL FROM tracks WHERE id=$1`, trackID).Scan(&unavailable); err != nil {
		t.Fatal(err)
	}
	if !unavailable {
		t.Fatal("catalogue media should be marked unavailable")
	}
}
