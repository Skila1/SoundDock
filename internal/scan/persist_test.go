package scan

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestUpdateExistingTrackSQLHonoursFieldLocks(t *testing.T) {
	sql := updateExistingTrackSQL
	if !strings.Contains(sql, "metadata_locks") {
		t.Fatal("rescan must consult metadata_locks")
	}
	if !strings.Contains(sql, "locked=false") {
		t.Fatal("global tracks.locked must still skip the update")
	}
	for _, field := range []string{"title", "album", "disc_number", "track_number", "year", "genre", "mbid"} {
		needle := "field='" + field + "'"
		if !strings.Contains(sql, needle) {
			t.Fatalf("missing per-field lock for %s", field)
		}
	}
}

func TestTrackOrFieldLockedNilScanner(t *testing.T) {
	s := &Scanner{}
	if s.trackOrFieldLocked(t.Context(), uuid.New(), "title") {
		t.Fatal("nil pool must not report locked")
	}
}

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

func requireReviewSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema='public' AND table_name='duplicate_review_groups'`).Scan(&n)
	if err != nil || n == 0 {
		t.Skip("0017 duplicate_review_groups not applied")
	}
}

func TestPersistDuplicateGroupsNotOneRowPerPair(t *testing.T) {
	pool := testPool(t)
	requireReviewSchema(t, pool)
	ctx := context.Background()
	fix := seedScanDup(t, pool, 3)

	sc := New(pool, nil, nil, nil)
	for _, fid := range fix.fileIDs {
		sc.writeDuplicateGroup(ctx, fid, fix.hash)
	}
	sc.persistDuplicateGroups(ctx, uuid.Nil)

	var groups, members, reviewN int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM duplicate_groups WHERE blocking_key=$1`, contentHashBlockingKey(fix.hash)).Scan(&groups); err != nil {
		t.Fatal(err)
	}
	if groups != 1 {
		t.Fatalf("content_hash groups=%d want 1 (not one per pair)", groups)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM duplicates d
		JOIN duplicate_groups g ON g.id=d.group_id
		WHERE g.blocking_key=$1`, contentHashBlockingKey(fix.hash)).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if members != 3 {
		t.Fatalf("duplicate members=%d want 3 (one per file, not C(3,2) pairs)", members)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM duplicate_review_groups r
		JOIN duplicate_groups g ON g.id=r.group_id
		WHERE g.blocking_key=$1`, contentHashBlockingKey(fix.hash)).Scan(&reviewN); err != nil {
		t.Fatal(err)
	}
	if reviewN != 1 {
		t.Fatalf("review rows=%d want 1", reviewN)
	}

	var atGroups, atMembers int
	key0 := artistTitleClusterKey(ArtistTitleBlockingKey("Linkin Park", fix.title), 0)
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM duplicate_groups WHERE blocking_key=$1`, key0).Scan(&atGroups); err != nil {
		t.Fatal(err)
	}
	if atGroups != 1 {
		t.Fatalf("artist_title groups=%d want 1", atGroups)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM duplicates d
		JOIN duplicate_groups g ON g.id=d.group_id
		WHERE g.blocking_key=$1`, key0).Scan(&atMembers); err != nil {
		t.Fatal(err)
	}
	if atMembers != 3 {
		t.Fatalf("artist_title members=%d want 3", atMembers)
	}
}

func TestPersistDuplicateGroupsSkipsWhenJobCancelled(t *testing.T) {
	pool := testPool(t)
	requireReviewSchema(t, pool)
	ctx := context.Background()
	fix := seedScanDup(t, pool, 2)
	jobID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO jobs (id, type, status, cancel_requested) VALUES ($1,'library.scan','running',true)`, jobID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM jobs WHERE id=$1`, jobID)
	})

	sc := New(pool, nil, nil, nil)
	sc.persistDuplicateGroups(ctx, jobID)

	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM duplicate_groups WHERE blocking_key=$1`,
		artistTitleClusterKey(ArtistTitleBlockingKey("Linkin Park", fix.title), 0)).Scan(&n)
	if n != 0 {
		t.Fatalf("cancelled job must not persist artist_title groups, got %d", n)
	}
}

type scanDupFix struct {
	prov, lib, artist uuid.UUID
	trackIDs, fileIDs []uuid.UUID
	hash, title       string
}

func seedScanDup(t *testing.T, pool *pgxpool.Pool, n int) scanDupFix {
	t.Helper()
	ctx := context.Background()
	f := scanDupFix{
		prov: uuid.New(), lib: uuid.New(), artist: uuid.New(),
		hash:  "dup-hash-" + uuid.NewString()[:12],
		title: "Numb " + uuid.NewString()[:8],
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc)
		VALUES ($1,$2,'managed',$3)`, f.prov, "scan-"+f.prov.String()[:8], []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id) VALUES ($1,$2,'music',$3)`,
		f.lib, "scan-lib-"+f.lib.String()[:8], f.prov); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO artists (id, name, sort_name) VALUES ($1,'Linkin Park','Linkin Park')`, f.artist); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		tid, fid := uuid.New(), uuid.New()
		if _, err := pool.Exec(ctx, `
			INSERT INTO tracks (id, library_id, title, duration_ms) VALUES ($1,$2,$3,$4)`,
			tid, f.lib, f.title, 187000+i*400); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO track_artists (track_id, artist_id, role, position) VALUES ($1,$2,'primary',0)`, tid, f.artist); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO track_files (id, track_id, library_id, storage_key, size_bytes, content_hash, quality)
			VALUES ($1,$2,$3,$4,8,$5,'original')`, fid, tid, f.lib, "scan/"+fid.String()[:8], f.hash); err != nil {
			t.Fatal(err)
		}
		f.trackIDs = append(f.trackIDs, tid)
		f.fileIDs = append(f.fileIDs, fid)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM duplicate_review_groups WHERE track_ids && $1::uuid[]`, f.trackIDs)
		_, _ = pool.Exec(c, `DELETE FROM duplicates WHERE track_file_id=ANY($1)`, f.fileIDs)
		_, _ = pool.Exec(c, `DELETE FROM duplicate_groups WHERE blocking_key=$1 OR blocking_key LIKE $2`,
			contentHashBlockingKey(f.hash), "artist_title:"+ArtistTitleBlockingKey("Linkin Park", f.title)+"%")
		_, _ = pool.Exec(c, `DELETE FROM track_files WHERE id=ANY($1)`, f.fileIDs)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=ANY($1)`, f.trackIDs)
		_, _ = pool.Exec(c, `DELETE FROM artists WHERE id=$1`, f.artist)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, f.lib)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, f.prov)
	})
	return f
}
