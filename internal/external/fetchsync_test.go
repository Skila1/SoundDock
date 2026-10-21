package external

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/jobs"
)

func TestDownloadFailureReason(t *testing.T) {
	cases := []struct{ in, want string }{
		{"terminal job failure\nyt-dlp: ERROR: [youtube] abc123def45: Sign in to confirm your age. This video may be inappropriate for some users.",
			"Download failed: Sign in to confirm your age. This video may be inappropriate for some users."},
		{"terminal job failure\nyt-dlp produced no audio", "Download failed: yt-dlp produced no audio"},
		{"yt-dlp: WARNING: slow\nERROR: [youtube] x: Video unavailable", "Download failed: Video unavailable"},
		{"", "Download failed: unknown error"},
	}
	for _, c := range cases {
		if got := DownloadFailureReason(c.in); got != c.want {
			t.Errorf("DownloadFailureReason(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	long := DownloadFailureReason(strings.Repeat("y", 500))
	if !strings.HasSuffix(long, "...") || len(long) > len("Download failed: ")+maxItemErrorRunes+3 {
		t.Fatalf("long reasons are cut: %d", len(long))
	}
}

func TestFetchJobPayloadRefs(t *testing.T) {
	lib := uuid.New()
	raw, _ := json.Marshal(map[string]any{
		"urls":            []string{"https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		"source_refs":     []string{"dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		"dest_library_id": lib.String(),
	})
	p, ok := parseFetchJob(nil, jobs.Job{Type: FetchJobType, Payload: raw})
	if ok {
		t.Fatal("no pool means nothing to reconcile")
	}
	if got := p.refs(); len(got) != 0 {
		t.Fatalf("payload must not parse without a pool: %v", got)
	}
	var q fetchJobPayload
	_ = json.Unmarshal(raw, &q)
	if got := q.refs(); len(got) != 1 || got[0] != "dQw4w9WgXcQ" {
		t.Fatalf("refs %v", got)
	}
	if q.library() != lib {
		t.Fatalf("library %v", q.library())
	}
	q.SourceRefs = nil
	if got := q.refs(); len(got) != 1 || got[0] != "dQw4w9WgXcQ" {
		t.Fatalf("urls fallback %v", got)
	}
}

func TestWithoutID(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	got := withoutID([]uuid.UUID{a, b, a}, a)
	if len(got) != 1 || got[0] != b {
		t.Fatalf("got %v", got)
	}
}

type fetchFixture struct {
	pool                             *pgxpool.Pool
	user, lib, pl, epid, stub, other uuid.UUID
	ref                              string
}

func newFetchFixture(t *testing.T) fetchFixture {
	t.Helper()
	pool := testPool(t)
	ctx := context.Background()
	f := fetchFixture{pool: pool, user: uuid.New(), lib: uuid.New(), stub: uuid.New(), other: uuid.New()}
	f.ref = fmt.Sprintf("Fx%09d", uuid.New().ID()%1000000000)
	sid := uuid.New()
	mustExec(t, pool, `INSERT INTO storage_providers (id, name, type, config_enc) VALUES ($1,$2,'managed',$3)`, sid, "fx-"+sid.String()[:8], []byte("{}"))
	mustExec(t, pool, `INSERT INTO libraries (id, name, kind, storage_provider_id) VALUES ($1,$2,'music',$3)`, f.lib, "FX-"+f.lib.String()[:8], sid)
	mustExec(t, pool, `INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`, f.user, "fx-"+f.user.String()[:8])
	mustExec(t, pool, `INSERT INTO tracks (id, library_id, title, acquisition, acquisition_ref) VALUES ($1,$2,'stub','youtube',$3)`, f.stub, f.lib, f.ref)
	mustExec(t, pool, `INSERT INTO tracks (id, library_id, title) VALUES ($1,$2,'other')`, f.other, f.lib)
	mustExec(t, pool, `INSERT INTO track_files (track_id, library_id, storage_key, size_bytes, quality) VALUES ($1,$2,$3,1,'original')`, f.other, f.lib, "fx/"+f.other.String()+".m4a")
	if err := pool.QueryRow(ctx, `INSERT INTO playlists (user_id, name) VALUES ($1,'fx') RETURNING id`, f.user).Scan(&f.pl); err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, `INSERT INTO playlist_entries (playlist_id, track_id, position) VALUES ($1,$2,0), ($1,$3,1), ($1,$2,2)`, f.pl, f.other, f.stub)
	if err := pool.QueryRow(ctx, `
		INSERT INTO external_playlists (user_id, provider, external_playlist_id, sounddock_playlist_id, name)
		VALUES ($1,'spotify',$2,$3,'fx') RETURNING id`, f.user, "fx-"+f.pl.String()[:8], f.pl).Scan(&f.epid); err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, `
		INSERT INTO external_playlist_items (external_playlist_id, position, provider_track_id, title, mapped_track_id, match_status, match_confidence)
		VALUES ($1,0,'a','Other',$2,'exact',1), ($1,1,'b','Stub song',$3,'high',0.9)`, f.epid, f.other, f.stub)
	mustExec(t, pool, `
		INSERT INTO external_track_mappings (provider, provider_track_id, sounddock_track_id, mapping_source, confidence)
		VALUES ('spotify',$1,$2,'youtube',0.9)`, "fx-b-"+f.stub.String()[:8], f.stub)
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM playlists WHERE id=$1`, f.pl)
		_, _ = pool.Exec(c, `DELETE FROM external_playlists WHERE id=$1`, f.epid)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE library_id=$1`, f.lib)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id=$1`, f.user)
	})
	return f
}

func mustExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func fetchJob(t *testing.T, f fetchFixture) jobs.Job {
	raw, _ := json.Marshal(map[string]any{
		"urls":            []string{"https://www.youtube.com/watch?v=" + f.ref},
		"source_refs":     []string{f.ref},
		"coalesce_key":    "fx|" + f.ref,
		"dest_library_id": f.lib.String(),
	})
	return jobs.Job{ID: uuid.New(), Type: FetchJobType, Payload: raw}
}

func TestFailFetchJobSendsItemToNeedsReview(t *testing.T) {
	f := newFetchFixture(t)
	ctx := context.Background()
	FailFetchJob(ctx, f.pool, fetchJob(t, f), "terminal job failure\nyt-dlp: ERROR: [youtube] x: Video unavailable")

	var mapped *uuid.UUID
	var status, itemErr string
	if err := f.pool.QueryRow(ctx, `
		SELECT mapped_track_id, match_status, item_error FROM external_playlist_items
		WHERE external_playlist_id=$1 AND provider_track_id='b'`, f.epid).Scan(&mapped, &status, &itemErr); err != nil {
		t.Fatal(err)
	}
	if mapped != nil || status != "unmatched" || itemErr != "Download failed: Video unavailable" {
		t.Fatalf("item not back in Needs review: mapped=%v status=%q err=%q", mapped, status, itemErr)
	}
	rows, err := f.pool.Query(ctx, `SELECT track_id, position FROM playlist_entries WHERE playlist_id=$1 ORDER BY position`, f.pl)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var tid uuid.UUID
		var pos int
		_ = rows.Scan(&tid, &pos)
		got = append(got, fmt.Sprintf("%d:%v", pos, tid == f.other))
	}
	if strings.Join(got, ",") != "0:true,1:true" {
		t.Fatalf("dead stub must leave the playlist and positions close up: %v", got)
	}
	var maps int
	_ = f.pool.QueryRow(ctx, `SELECT count(*) FROM external_track_mappings WHERE sounddock_track_id=$1`, f.stub).Scan(&maps)
	if maps != 0 {
		t.Fatal("YouTube mapping to the dead stub must go")
	}
}

func TestFailFetchJobSkipsWhileAnotherFetchIsActive(t *testing.T) {
	f := newFetchFixture(t)
	ctx := context.Background()
	job := fetchJob(t, f)
	var active uuid.UUID
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO jobs (type, payload, status) VALUES ($1, $2, 'queued') RETURNING id`,
		FetchJobType, job.Payload).Scan(&active); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM jobs WHERE id=$1`, active) })
	FailFetchJob(ctx, f.pool, job, "Video unavailable")
	var status string
	_ = f.pool.QueryRow(ctx, `SELECT match_status FROM external_playlist_items WHERE external_playlist_id=$1 AND provider_track_id='b'`, f.epid).Scan(&status)
	if status != "high" {
		t.Fatalf("a retry may still fill the stub; got %q", status)
	}
}

func TestConfirmFetchJobMovesItemsOntoDownloadedTrack(t *testing.T) {
	f := newFetchFixture(t)
	ctx := context.Background()
	real := uuid.New()
	mustExec(t, f.pool, `INSERT INTO tracks (id, library_id, title, acquisition, acquisition_ref, created_at) VALUES ($1,$2,'real','youtube',$3, now()+interval '1 second')`, real, f.lib, f.ref)
	mustExec(t, f.pool, `INSERT INTO track_files (track_id, library_id, storage_key, size_bytes, quality) VALUES ($1,$2,$3,1,'original')`, real, f.lib, "fx/"+real.String()+".m4a")
	mustExec(t, f.pool, `UPDATE external_playlist_items SET item_error='old' WHERE external_playlist_id=$1`, f.epid)

	ConfirmFetchJob(ctx, f.pool, fetchJob(t, f))

	var mapped uuid.UUID
	var itemErr string
	if err := f.pool.QueryRow(ctx, `
		SELECT mapped_track_id, item_error FROM external_playlist_items
		WHERE external_playlist_id=$1 AND provider_track_id='b'`, f.epid).Scan(&mapped, &itemErr); err != nil {
		t.Fatal(err)
	}
	if mapped != real || itemErr != "" {
		t.Fatalf("item must point at the downloaded track: mapped=%v err=%q", mapped, itemErr)
	}
	var n int
	_ = f.pool.QueryRow(ctx, `SELECT count(*) FROM playlist_entries WHERE playlist_id=$1 AND track_id=$2`, f.pl, real).Scan(&n)
	if n != 1 {
		t.Fatalf("playlist row must follow the item, got %d", n)
	}
}

func TestFetchFailureForGoneStub(t *testing.T) {
	pool := testPool(t)
	reason, failed := fetchFailure(context.Background(), pool, uuid.New())
	if !failed || !strings.HasPrefix(reason, "Download failed: ") {
		t.Fatalf("a deleted stub cannot be mapped: %q %v", reason, failed)
	}
	if _, failed := fetchFailure(context.Background(), pool, uuid.Nil); failed {
		t.Fatal("nil id is not a failure")
	}
}
