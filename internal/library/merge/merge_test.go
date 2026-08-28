package merge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/testdb"
)

func testPool(t *testing.T) *pgxpool.Pool {
	return testdb.Open(t)
}

type seed struct {
	prov, srcLib, destLib uuid.UUID
	winner, loser         uuid.UUID
	user                  uuid.UUID
	uniqueSrc             uuid.UUID
}

func seedMerge(t *testing.T, pool *pgxpool.Pool) seed {
	t.Helper()
	ctx := context.Background()
	s := seed{
		prov: uuid.New(), srcLib: uuid.New(), destLib: uuid.New(),
		winner: uuid.New(), loser: uuid.New(), user: uuid.New(),
		uniqueSrc: uuid.New(),
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc)
		VALUES ($1, $2, 'managed', $3)`, s.prov, "merge-"+s.prov.String()[:8], []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id) VALUES
		($1, $2, 'music', $5), ($3, $4, 'music', $5)`,
		s.srcLib, "src-"+s.srcLib.String()[:8],
		s.destLib, "dest-"+s.destLib.String()[:8], s.prov); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title) VALUES
		($1,$2,'winner'), ($3,$4,'loser'), ($5,$4,'unique-src')`,
		s.winner, s.destLib, s.loser, s.srcLib, s.uniqueSrc); err != nil {
		t.Fatal(err)
	}
	hash := "merge-hash-" + s.winner.String()[:8]
	if _, err := pool.Exec(ctx, `
		INSERT INTO track_files (track_id, library_id, storage_key, size_bytes, content_hash, quality) VALUES
		($1,$2,$3,10,$7,'original'),
		($4,$5,$6,10,$7,'original'),
		($8,$5,$9,4,'unique-hash','original')`,
		s.winner, s.destLib, "dest/"+s.winner.String()[:8]+".flac",
		s.loser, s.srcLib, "src/"+s.loser.String()[:8]+".flac",
		hash,
		s.uniqueSrc, "src/"+s.uniqueSrc.String()[:8]+".flac"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, username, password_hash, display_name)
		VALUES ($1,$2,'x',$2)`, s.user, "merge-"+s.user.String()[:8]); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM playback_queue_items WHERE track_id=ANY($1)`, []uuid.UUID{s.winner, s.loser, s.uniqueSrc})
		_, _ = pool.Exec(c, `DELETE FROM playback_sessions WHERE current_track_id=ANY($1) OR owner_key LIKE $2`, []uuid.UUID{s.winner, s.loser, s.uniqueSrc}, "merge-"+s.user.String()[:8]+"%")
		_, _ = pool.Exec(c, `DELETE FROM listen_events WHERE user_id=$1`, s.user)
		_, _ = pool.Exec(c, `DELETE FROM listen_history WHERE user_id=$1`, s.user)
		_, _ = pool.Exec(c, `DELETE FROM play_counts WHERE user_id=$1`, s.user)
		_, _ = pool.Exec(c, `DELETE FROM favourites WHERE user_id=$1`, s.user)
		_, _ = pool.Exec(c, `DELETE FROM track_files WHERE track_id=ANY($1)`, []uuid.UUID{s.winner, s.loser, s.uniqueSrc})
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=ANY($1)`, []uuid.UUID{s.winner, s.loser, s.uniqueSrc})
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=ANY($1)`, []uuid.UUID{s.srcLib, s.destLib})
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, s.prov)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id=$1`, s.user)
	})
	return s
}

func TestTracksRemapHistoryAndPlayCounts(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := seedMerge(t, pool)

	older := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		INSERT INTO listen_history (user_id, track_id, played_at, duration_ms, source)
		VALUES ($1,$2,$3,180000,'web'), ($1,$4,$5,120000,'web')`,
		s.user, s.loser, newer, s.winner, older); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO play_counts (user_id, track_id, count, skip_count, last_played_at) VALUES
		($1,$2,3,1,$4), ($1,$3,2,4,$5)`,
		s.user, s.winner, s.loser, older, newer); err != nil {
		t.Fatal(err)
	}

	if err := Tracks(ctx, pool, s.winner, s.loser); err != nil {
		t.Fatal(err)
	}

	var loserN int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE id=$1`, s.loser).Scan(&loserN); err != nil {
		t.Fatal(err)
	}
	if loserN != 0 {
		t.Fatalf("loser still exists")
	}

	var histWinner, histLoser int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM listen_history WHERE user_id=$1 AND track_id=$2`, s.user, s.winner).Scan(&histWinner); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM listen_history WHERE user_id=$1 AND track_id=$2`, s.user, s.loser).Scan(&histLoser); err != nil {
		t.Fatal(err)
	}
	if histWinner != 2 {
		t.Fatalf("history on winner=%d want 2", histWinner)
	}
	if histLoser != 0 {
		t.Fatalf("history still on loser=%d", histLoser)
	}

	var count, skips int
	var last time.Time
	if err := pool.QueryRow(ctx, `
		SELECT count, skip_count, last_played_at FROM play_counts
		WHERE user_id=$1 AND track_id=$2`, s.user, s.winner).Scan(&count, &skips, &last); err != nil {
		t.Fatal(err)
	}
	if count != 5 || skips != 5 {
		t.Fatalf("play_counts count=%d skip_count=%d want 5/5", count, skips)
	}
	if !last.Equal(newer) {
		t.Fatalf("last_played_at %v want %v", last, newer)
	}
	var loserCounts int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM play_counts WHERE track_id=$1`, s.loser).Scan(&loserCounts)
	if loserCounts != 0 {
		t.Fatalf("play_counts leftover on loser: %d", loserCounts)
	}
}

func TestTracksBlocksActiveLoser(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := seedMerge(t, pool)

	if _, err := pool.Exec(ctx, `
		INSERT INTO listen_history (user_id, track_id, duration_ms, source)
		VALUES ($1,$2,1000,'web')`, s.user, s.loser); err != nil {
		t.Fatal(err)
	}
	sid := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO playback_sessions (id, kind, owner_key, current_track_id, status)
		VALUES ($1,'web_device',$2,$3,'playing')`,
		sid, "merge-"+s.user.String()[:8], s.loser); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_sessions WHERE id=$1`, sid)
	})

	err := Tracks(ctx, pool, s.winner, s.loser)
	if !errors.Is(err, ErrTrackInUse) {
		t.Fatalf("err=%v want ErrTrackInUse", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE id=$1`, s.loser).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("loser must still exist, count=%d", n)
	}
	var histLoser int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM listen_history WHERE track_id=$1`, s.loser).Scan(&histLoser)
	if histLoser != 1 {
		t.Fatalf("history must stay on loser when blocked, got %d", histLoser)
	}
}

func TestLibraryIntoHashDupeKeepsHistory(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := seedMerge(t, pool)

	if _, err := pool.Exec(ctx, `
		INSERT INTO listen_history (user_id, track_id, duration_ms, source)
		VALUES ($1,$2,90000,'web')`, s.user, s.loser); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO play_counts (user_id, track_id, count, skip_count, last_played_at)
		VALUES ($1,$2,1,2,now())`, s.user, s.loser); err != nil {
		t.Fatal(err)
	}

	moved, err := LibraryInto(ctx, pool, s.srcLib, s.destLib)
	if err != nil {
		t.Fatal(err)
	}
	if moved != 1 {
		t.Fatalf("moved=%d want 1 (unique src track)", moved)
	}

	var loserN int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE id=$1`, s.loser).Scan(&loserN)
	if loserN != 0 {
		t.Fatalf("hash-dupe loser still exists")
	}
	var lib uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT library_id FROM tracks WHERE id=$1`, s.uniqueSrc).Scan(&lib); err != nil {
		t.Fatal(err)
	}
	if lib != s.destLib {
		t.Fatalf("unique track library %s want dest %s", lib, s.destLib)
	}
	var histWinner, histLoser int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM listen_history WHERE user_id=$1 AND track_id=$2`, s.user, s.winner).Scan(&histWinner)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM listen_history WHERE user_id=$1 AND track_id=$2`, s.user, s.loser).Scan(&histLoser)
	if histWinner != 1 || histLoser != 0 {
		t.Fatalf("history winner=%d loser=%d want 1/0", histWinner, histLoser)
	}
	var count, skips int
	if err := pool.QueryRow(ctx, `SELECT count, skip_count FROM play_counts WHERE user_id=$1 AND track_id=$2`, s.user, s.winner).Scan(&count, &skips); err != nil {
		t.Fatal(err)
	}
	if count != 1 || skips != 2 {
		t.Fatalf("play_counts count=%d skip=%d want 1/2", count, skips)
	}
	var srcLibN int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM libraries WHERE id=$1`, s.srcLib).Scan(&srcLibN)
	if srcLibN != 0 {
		t.Fatalf("source library still exists")
	}
}

func TestTracksBlocksDiscordDecoder(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := seedMerge(t, pool)

	sid := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO playback_sessions (id, kind, owner_key, current_track_id, status, renderer_kind, renderer_id)
		VALUES ($1,'web_device',$2,$3,'stopped','discord','bot-1')`,
		sid, "merge-dc-"+s.user.String()[:8], s.loser); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_sessions WHERE id=$1`, sid)
	})

	err := Tracks(ctx, pool, s.winner, s.loser)
	if !errors.Is(err, ErrTrackInUse) {
		t.Fatalf("err=%v want ErrTrackInUse", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE id=$1`, s.loser).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("loser must still exist")
	}
}
