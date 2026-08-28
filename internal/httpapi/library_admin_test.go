package httpapi

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/testdb"
)

func testPool(t *testing.T) *pgxpool.Pool {
	return testdb.Open(t)
}

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

	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM track_files WHERE track_id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, libID)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, sid)
	})

	s := &Server{Pool: pool}
	if _, err := s.PurgeTrackMedia(ctx, trackID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("NAS file must remain on disk after retention with opt_in true: %v", err)
	}
}

func TestMixedBulkDeleteSkipsNonManaged(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	nasDir := t.TempDir()
	managedDir := t.TempDir()
	nasKey := "nas/song.flac"
	manKey := "uploads/aa/managed.flac"
	nasAbs := filepath.Join(nasDir, filepath.FromSlash(nasKey))
	manAbs := filepath.Join(managedDir, filepath.FromSlash(manKey))
	for _, p := range []string{nasAbs, manAbs} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("audio"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	nasSID, manSID := uuid.New(), uuid.New()
	nasLib, manLib := uuid.New(), uuid.New()
	nasTrack, manTrack := uuid.New(), uuid.New()
	insertProv := func(id uuid.UUID, name, typ, root string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			INSERT INTO storage_providers (id, name, type, config_enc) VALUES ($1,$2,$3,$4)`,
			id, name, typ, []byte(root)); err != nil {
			t.Fatal(err)
		}
	}
	insertProv(nasSID, "nas-"+nasSID.String()[:8], "local", nasDir)
	insertProv(manSID, "man-"+manSID.String()[:8], "managed", managedDir)
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id) VALUES
		($1, $2, 'music', $3), ($4, $5, 'music', $6)`,
		nasLib, "NAS "+nasLib.String()[:8], nasSID,
		manLib, "Man "+manLib.String()[:8], manSID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title) VALUES ($1,$2,'nas'), ($3,$4,'managed')`,
		nasTrack, nasLib, manTrack, manLib); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO track_files (track_id, library_id, storage_key, size_bytes, quality) VALUES
		($1,$2,$3,5,'original'), ($4,$5,$6,5,'original')`,
		nasTrack, nasLib, nasKey, manTrack, manLib, manKey); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=ANY($1)`, []uuid.UUID{nasTrack, manTrack})
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=ANY($1)`, []uuid.UUID{nasLib, manLib})
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=ANY($1)`, []uuid.UUID{nasSID, manSID})
	})

	s := &Server{Pool: pool}
	n, skipped, err := s.deleteTrackIDs(ctx, []uuid.UUID{nasTrack, manTrack}, false, uuid.Nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("deleted %d want 2", n)
	}
	if len(skipped) != 1 {
		t.Fatalf("skipped %v", skipped)
	}
	if _, err := os.Stat(nasAbs); err != nil {
		t.Fatalf("NAS file should be skipped, got %v", err)
	}
	if _, err := os.Stat(manAbs); err == nil {
		t.Fatal("managed file should be deleted")
	}
}

func TestMergeLibraryIntoPreservesListenHistory(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	prov, srcLib, destLib := uuid.New(), uuid.New(), uuid.New()
	winner, loser := uuid.New(), uuid.New()
	user := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc)
		VALUES ($1,$2,'managed',$3)`, prov, "adm-"+prov.String()[:8], []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id) VALUES
		($1,$2,'music',$5), ($3,$4,'music',$5)`,
		srcLib, "src-"+srcLib.String()[:8], destLib, "dest-"+destLib.String()[:8], prov); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title) VALUES ($1,$2,'win'), ($3,$4,'lose')`,
		winner, destLib, loser, srcLib); err != nil {
		t.Fatal(err)
	}
	hash := "adm-hash-" + winner.String()[:8]
	if _, err := pool.Exec(ctx, `
		INSERT INTO track_files (track_id, library_id, storage_key, size_bytes, content_hash, quality) VALUES
		($1,$2,$3,8,$7,'original'), ($4,$5,$6,8,$7,'original')`,
		winner, destLib, "d/"+winner.String()[:8], loser, srcLib, "s/"+loser.String()[:8], hash); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`,
		user, "adm-"+user.String()[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO listen_history (user_id, track_id, duration_ms, source)
		VALUES ($1,$2,60000,'web')`, user, loser); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM listen_history WHERE user_id=$1`, user)
		_, _ = pool.Exec(c, `DELETE FROM play_counts WHERE user_id=$1`, user)
		_, _ = pool.Exec(c, `DELETE FROM track_files WHERE track_id=ANY($1)`, []uuid.UUID{winner, loser})
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=ANY($1)`, []uuid.UUID{winner, loser})
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=ANY($1)`, []uuid.UUID{srcLib, destLib})
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, prov)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id=$1`, user)
	})

	srv := &Server{Pool: pool}
	if _, err := srv.mergeLibraryInto(ctx, srcLib, destLib, uuid.Nil); err != nil {
		t.Fatal(err)
	}
	var histWinner, histLoser, loserN int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM listen_history WHERE user_id=$1 AND track_id=$2`, user, winner).Scan(&histWinner)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM listen_history WHERE user_id=$1 AND track_id=$2`, user, loser).Scan(&histLoser)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE id=$1`, loser).Scan(&loserN)
	if histWinner != 1 || histLoser != 0 || loserN != 0 {
		t.Fatalf("history winner=%d loser=%d loser_track=%d", histWinner, histLoser, loserN)
	}
}
