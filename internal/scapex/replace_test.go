package scapex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestReplaceCoalesceKeyIncludesDestPolicyAndTrack(t *testing.T) {
	lib := uuid.MustParse("00000000-0000-4000-8000-000000000020")
	trackA, trackB := uuid.New(), uuid.New()
	a := ReplaceCoalesceKey(trackA, "youtube", "dQw4w9WgXcQ", lib.String(), "m4a-0")
	b := ReplaceCoalesceKey(trackA, "youtube", "dQw4w9WgXcQ", lib.String(), "opus-0")
	c := ReplaceCoalesceKey(trackA, "youtube", "dQw4w9WgXcQ", uuid.New().String(), "m4a-0")
	d := ReplaceCoalesceKey(trackB, "youtube", "dQw4w9WgXcQ", lib.String(), "m4a-0")
	if a == b || a == c || a == d {
		t.Fatal(a, b, c, d)
	}
	if !strings.Contains(a, lib.String()) || !strings.Contains(a, "m4a-0") || !strings.Contains(a, trackA.String()) {
		t.Fatalf("missing dest/policy/track: %s", a)
	}
}

func TestReplaceStorageKeyIsNewObject(t *testing.T) {
	track, job := uuid.New(), uuid.New()
	key := ReplaceStorageKey(track, job, ".m4a")
	if strings.Contains(key, "inbox") {
		t.Fatal(key)
	}
	if key == "old/key.m4a" {
		t.Fatal("collided with old key")
	}
	if !strings.Contains(key, track.String()) || !strings.HasSuffix(key, ".m4a") {
		t.Fatal(key)
	}
}

func TestAcquireReplaceRejectsNonYouTube(t *testing.T) {
	d := NewDockWithPool(nil, t.TempDir())
	svc := NewService(stubYT{}, d)
	_, err := svc.AcquireReplace(context.Background(), ReplaceOpts{
		URLs:        []string{"https://example.com/a.mp3"},
		DestLibrary: uuid.New(),
		TrackID:     uuid.New(),
	})
	if err == nil {
		t.Fatal("expected reject")
	}
}

func TestAcquireReplaceUsesJobWorkNotSharedInbox(t *testing.T) {
	inbox := t.TempDir()
	d := NewDockWithPool(nil, inbox)
	src := filepath.Join(t.TempDir(), "dQw4w9WgXcQ.m4a")
	if err := os.WriteFile(src, []byte("audio-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	yt := quotaYT{stubYT: stubYT{tracks: []LocalTrack{{
		Path: src, VideoID: "dQw4w9WgXcQ", Title: "Numb",
	}}}, destCopy: true}
	svc := NewService(yt, d)
	jobID := uuid.New()
	locals, err := svc.AcquireReplace(context.Background(), ReplaceOpts{
		JobID:       jobID,
		URLs:        []string{"dQw4w9WgXcQ"},
		DestLibrary: uuid.New(),
		Policy:      DefaultMediaPolicy,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d.JobWork(jobID)) })
	if len(locals) != 1 {
		t.Fatalf("locals %d", len(locals))
	}
	shared := SharedInboxName(inbox, "dQw4w9WgXcQ", ".m4a")
	if _, err := os.Stat(shared); err == nil {
		t.Fatal("shared inbox file exists")
	}
	if !strings.Contains(filepath.ToSlash(locals[0].Path), jobID.String()) {
		t.Fatalf("not job-scoped: %s", locals[0].Path)
	}
	if _, err := os.Stat(d.JobInbox(jobID)); err == nil {
		t.Fatal("must not commit fetch inbox")
	}
}

func TestAcquireReplaceQuotaFailCleansWork(t *testing.T) {
	inbox := t.TempDir()
	d := NewDockWithPool(nil, inbox)
	src := filepath.Join(t.TempDir(), "dQw4w9WgXcQ.m4a")
	if err := os.WriteFile(src, []byte("audio-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	yt := quotaYT{stubYT: stubYT{tracks: []LocalTrack{{
		Path: src, VideoID: "dQw4w9WgXcQ", Title: "Numb",
	}}}, destCopy: true}
	svc := NewService(yt, d)
	jobID := uuid.New()
	_, err := svc.AcquireReplace(context.Background(), ReplaceOpts{
		JobID:       jobID,
		URLs:        []string{"dQw4w9WgXcQ"},
		DestLibrary: uuid.New(),
		Quota: func(context.Context, uuid.UUID, uuid.UUID, int64) error {
			return errString("library storage quota exceeded")
		},
	})
	if err == nil {
		t.Fatal("expected quota error")
	}
	if _, err := os.Stat(d.JobWork(jobID)); err == nil {
		t.Fatal("work dir left after quota fail")
	}
}

func TestCommitReplaceInsertsTrackSourcesAndKeepsOldKey(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	provID, libID, trackID := uuid.New(), uuid.New(), uuid.New()
	oldKey := "library/old-" + trackID.String()[:8] + ".m4a"
	newKey := ReplaceStorageKey(trackID, uuid.New(), ".m4a")
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc)
		VALUES ($1,$2,'managed',$3)`, provID, "rep-"+provID.String()[:8], []byte(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id, root_prefix, read_only)
		VALUES ($1,$2,'music',$3,'',false)`, libID, "rep "+libID.String()[:8], provID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title, duration_ms, acquisition)
		VALUES ($1,$2,'Replace Me',1000,'youtube')`, trackID, libID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO track_files (track_id, library_id, storage_key, size_bytes, content_hash, quality)
		VALUES ($1,$2,$3,11,'oldhash','original')`, trackID, libID, oldKey); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM track_sources WHERE track_id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM track_files WHERE track_id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, libID)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, provID)
	})

	jobID := uuid.New()
	retired, err := CommitReplace(ctx, pool, CommitReplaceInput{
		TrackID:     trackID,
		LibraryID:   libID,
		StorageKey:  newKey,
		SizeBytes:   22,
		ContentHash: "newhash",
		Codec:       "aac",
		Container:   "mp4",
		DurationMS:  2000,
		Provider:    ProviderYouTube,
		SourceRef:   "dQw4w9WgXcQ",
		JobID:       jobID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(retired) != 1 || retired[0].StorageKey != oldKey {
		t.Fatalf("retired %+v", retired)
	}

	var oldStored, oldQuality string
	var oldDeleted bool
	if err := pool.QueryRow(ctx, `
		SELECT storage_key, quality, deleted_at IS NOT NULL
		FROM track_files WHERE track_id=$1 AND storage_key=$2`, trackID, oldKey).
		Scan(&oldStored, &oldQuality, &oldDeleted); err != nil {
		t.Fatal(err)
	}
	if oldStored != oldKey {
		t.Fatalf("old storage_key overwritten: %s", oldStored)
	}
	if oldStored == newKey {
		t.Fatal("old row swapped onto new key")
	}
	if !oldDeleted || oldQuality != QualityReplaced {
		t.Fatalf("old row not retired: quality=%s deleted=%v", oldQuality, oldDeleted)
	}

	var liveKey, liveHash string
	if err := pool.QueryRow(ctx, `
		SELECT storage_key, content_hash FROM track_files
		WHERE track_id=$1 AND quality='original' AND deleted_at IS NULL
		LIMIT 1`, trackID).Scan(&liveKey, &liveHash); err != nil {
		t.Fatal(err)
	}
	if liveKey != newKey || liveHash != "newhash" {
		t.Fatalf("live original %s %s", liveKey, liveHash)
	}

	var n int
	var srcRef string
	if err := pool.QueryRow(ctx, `
		SELECT count(*), max(source_ref) FROM track_sources WHERE track_id=$1 AND job_id=$2`,
		trackID, jobID).Scan(&n, &srcRef); err != nil {
		t.Fatal(err)
	}
	if n != 1 || srcRef != "dQw4w9WgXcQ" {
		t.Fatalf("track_sources n=%d ref=%s", n, srcRef)
	}

	busy, err := ReplaceMediaBusy(ctx, pool, trackID, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if busy {
		t.Fatal("expected idle")
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO playback_sessions (id, kind, owner_key, current_track_id, status)
		VALUES ($1,'user',$2,$3,'playing')`, uuid.New(), "rep-"+trackID.String()[:8], trackID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_sessions WHERE current_track_id=$1`, trackID)
	})
	busy, err = ReplaceMediaBusy(ctx, pool, trackID, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if !busy {
		t.Fatal("session current should block delete")
	}
}
