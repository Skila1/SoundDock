package external

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/testdb"
)

func testPool(t *testing.T) *pgxpool.Pool {
	return testdb.Open(t)
}

func TestRetainedSnapshotKeepsPreviousOnBlank(t *testing.T) {
	if retainedSnapshot("snap-1", "") != "snap-1" {
		t.Fatal("blank incoming must keep previous")
	}
	if retainedSnapshot("old", "new") != "new" {
		t.Fatal("incoming snapshot wins")
	}
}

func TestKeepMembershipFillStubNotInKeepIDs(t *testing.T) {
	id := uuid.New()
	if got, ok := keepMembership(id, false); ok || got != uuid.Nil {
		t.Fatal("Fill stubs are pending_acquire, not membership")
	}
	if got, ok := keepMembership(id, true); !ok || got != id {
		t.Fatal("playable mapped tracks stay in keepIDs")
	}
	if _, ok := keepMembership(uuid.Nil, true); ok {
		t.Fatal("nil id")
	}
}

func TestSnapshotUnchanged(t *testing.T) {
	t.Parallel()
	if !snapshotUnchanged("snap-1", "ok", "snap-1") {
		t.Fatal("matching snapshot + ok should skip")
	}
	if snapshotUnchanged("snap-1", "failed", "snap-1") {
		t.Fatal("failed last status must not skip")
	}
	if snapshotUnchanged("old", "ok", "new") {
		t.Fatal("changed snapshot must not skip")
	}
	if snapshotUnchanged("", "ok", "") {
		t.Fatal("empty snapshot is not a skip signal")
	}
}

func TestReconcilePlaylistEntriesIdempotent(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	userID, libID, sid := uuid.New(), uuid.New(), uuid.New()
	a, b := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc) VALUES ($1,$2,'managed',$3)`,
		sid, "mem-"+sid.String()[:8], []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id) VALUES ($1,$2,'music',$3)`,
		libID, "L-"+libID.String()[:8], sid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title) VALUES ($1,$3,'a'), ($2,$3,'b')`, a, b, libID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO track_files (track_id, library_id, storage_key, size_bytes, quality)
		VALUES ($1,$3,'a.m4a',1,'original'), ($2,$3,'b.m4a',1,'original')`, a, b, libID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`,
		userID, "sync-"+userID.String()[:8]); err != nil {
		t.Fatal(err)
	}
	var pl uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO playlists (user_id, name) VALUES ($1,'sync') RETURNING id`, userID).Scan(&pl); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM playlist_entries WHERE playlist_id=$1`, pl)
		_, _ = pool.Exec(c, `DELETE FROM playlists WHERE id=$1`, pl)
		_, _ = pool.Exec(c, `DELETE FROM track_files WHERE track_id IN ($1,$2)`, a, b)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id IN ($1,$2)`, a, b)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, libID)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, sid)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id=$1`, userID)
	})

	desired := []uuid.UUID{a, b}
	if err := reconcilePlaylistEntries(ctx, pool, pl, desired, "mirror"); err != nil {
		t.Fatal(err)
	}
	rowIDs, tracks, err := loadPlaylistTrackIDs(ctx, pool, pl)
	if err != nil {
		t.Fatal(err)
	}
	if !sameUUIDs(tracks, desired) {
		t.Fatalf("tracks %v want %v", tracks, desired)
	}
	if err := reconcilePlaylistEntries(ctx, pool, pl, desired, "mirror"); err != nil {
		t.Fatal(err)
	}
	rowIDs2, tracks2, err := loadPlaylistTrackIDs(ctx, pool, pl)
	if err != nil {
		t.Fatal(err)
	}
	if !sameUUIDs(tracks2, desired) || !sameUUIDs(rowIDs, rowIDs2) {
		t.Fatalf("unchanged playlist rewritten: rows %v -> %v tracks %v", rowIDs, rowIDs2, tracks2)
	}
}

func TestMappedPlayableDoesNotNeedRefetch(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	userID, libID, sid, trackID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc) VALUES ($1,$2,'managed',$3)`,
		sid, "map-"+sid.String()[:8], []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id) VALUES ($1,$2,'music',$3)`,
		libID, "M-"+libID.String()[:8], sid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tracks (id, library_id, title) VALUES ($1,$2,'mapped')`, trackID, libID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO track_files (track_id, library_id, storage_key, size_bytes, quality)
		VALUES ($1,$2,'m.m4a',4,'original')`, trackID, libID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`,
		userID, "map-"+userID.String()[:8]); err != nil {
		t.Fatal(err)
	}
	ext := "sp-" + uuid.NewString()[:8]
	if _, err := pool.Exec(ctx, `
		INSERT INTO external_track_mappings (provider, provider_track_id, sounddock_track_id, mapping_source, confidence)
		VALUES ('spotify',$1,$2,'isrc',1)`, ext, trackID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM external_track_mappings WHERE provider_track_id=$1`, ext)
		_, _ = pool.Exec(c, `DELETE FROM track_files WHERE track_id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, libID)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, sid)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id=$1`, userID)
	})
	got := mappedPlayable(ctx, pool, "spotify", ext)
	if got != trackID {
		t.Fatalf("mapped %s want %s", got, trackID)
	}
}

func TestReconcileTxFailureLeavesMembership(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	userID, libID, sid, a := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc) VALUES ($1,$2,'managed',$3)`,
		sid, "tx-"+sid.String()[:8], []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id) VALUES ($1,$2,'music',$3)`,
		libID, "T-"+libID.String()[:8], sid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tracks (id, library_id, title) VALUES ($1,$2,'a')`, a, libID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`,
		userID, "tx-"+userID.String()[:8]); err != nil {
		t.Fatal(err)
	}
	var pl uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO playlists (user_id, name) VALUES ($1,'tx') RETURNING id`, userID).Scan(&pl); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO playlist_entries (playlist_id, track_id, position) VALUES ($1,$2,0)`, pl, a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM playlist_entries WHERE playlist_id=$1`, pl)
		_, _ = pool.Exec(c, `DELETE FROM playlists WHERE id=$1`, pl)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=$1`, a)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, libID)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, sid)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id=$1`, userID)
	})

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := reconcilePlaylistEntries(ctx, tx, pl, nil, "mirror"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM playlist_entries WHERE playlist_id=$1`, pl).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("rollback lost membership, count=%d", n)
	}
}
