package httpapi

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/playback"
)

func TestIsUploadedFile(t *testing.T) {
	cases := []struct {
		acq, key string
		want     bool
	}{
		{"", "uploads/aa/hash.flac", true},
		{"", "music/uploads/aa/hash.mp3", true},
		{"", "music/uploads/zip/0d6c3f4e-8b9a-4c55-9e2d-0f1a2b3c4d5e/a.mp3", true},
		{"youtube", "music/uploads/aa/hash.mp3", false},
		{"", "inbox/dQw4w9WgXcQ.opus", false},
		{"", "Artist/Album/01 Song.flac", false},
		{"", "music/imports/aa/hash.mp3", false},
	}
	for _, c := range cases {
		if got := isUploadedFile(c.acq, c.key); got != c.want {
			t.Errorf("isUploadedFile(%q, %q) = %v, want %v", c.acq, c.key, got, c.want)
		}
	}
}

func TestZipUploadSession(t *testing.T) {
	id := uuid.MustParse("0d6c3f4e-8b9a-4c55-9e2d-0f1a2b3c4d5e")
	if got := zipUploadSession("p/uploads/zip/" + id.String() + "/Album/01.mp3"); got != id {
		t.Fatalf("got %s", got)
	}
	if got := zipUploadSession("p/uploads/aa/hash.mp3"); got != uuid.Nil {
		t.Fatalf("got %s want nil", got)
	}
}

type uploadFixture struct {
	lib, album                   uuid.UUID
	albumTracks                  []uuid.UUID // track numbers 1..3
	single, otherUpload, youtube uuid.UUID
}

func seedUploadFixture(t *testing.T, pool *pgxpool.Pool) uploadFixture {
	t.Helper()
	ctx := context.Background()
	var f uploadFixture
	prov, uploader := uuid.New(), uuid.New()
	f.lib, f.album = uuid.New(), uuid.New()
	tag := f.lib.String()[:8]
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO storage_providers (id, name, type, config_enc) VALUES ($1, $2, 'local', $3)`, prov, "ap-"+tag, []byte(t.TempDir()))
	exec(`INSERT INTO libraries (id, name, kind, storage_provider_id, root_prefix, read_only) VALUES ($1, $2, 'music', $3, '', false)`, f.lib, "ap-"+tag, prov)
	exec(`INSERT INTO users (id, username, password_hash, display_name) VALUES ($1, $2, 'x', $2)`, uploader, "ap-"+tag)
	exec(`INSERT INTO albums (id, title, library_id) VALUES ($1, 'Upload Album', $2)`, f.album, f.lib)
	unknown := uuid.New()
	exec(`INSERT INTO albums (id, title, library_id) VALUES ($1, 'Unknown Album', $2)`, unknown, f.lib)

	addTrack := func(title string, album uuid.UUID, num int, acq, key, hash string) uuid.UUID {
		id := uuid.New()
		var alb any
		if album != uuid.Nil {
			alb = album
		}
		exec(`INSERT INTO tracks (id, library_id, title, album_id, track_number, acquisition) VALUES ($1, $2, $3, $4, $5, $6)`, id, f.lib, title, alb, num, acq)
		exec(`INSERT INTO track_files (track_id, library_id, storage_key, size_bytes, content_hash, quality) VALUES ($1, $2, $3, 8, $4, 'original')`, id, f.lib, key, hash)
		return id
	}
	upload := func(hash string) {
		exec(`INSERT INTO upload_sessions (user_id, library_id, filename, size_bytes, content_hash, staging_key, state) VALUES ($1, $2, 'f.mp3', 8, $3, 'x', 'complete')`, uploader, f.lib, hash)
	}
	for i := 1; i <= 3; i++ {
		h := uuid.NewString()
		f.albumTracks = append(f.albumTracks, addTrack("Album song", f.album, i, "", "uploads/"+h[:2]+"/"+h+".mp3", h))
		upload(h)
	}
	h1, h2 := uuid.NewString(), uuid.NewString()
	f.single = addTrack("Single", unknown, 0, "", "uploads/"+h1[:2]+"/"+h1+".mp3", h1)
	upload(h1)
	f.otherUpload = addTrack("Other upload", uuid.Nil, 0, "", "uploads/"+h2[:2]+"/"+h2+".mp3", h2)
	upload(h2)
	f.youtube = addTrack("YouTube song", uuid.Nil, 0, "youtube", "inbox/dQw4w9WgXcQ.opus", uuid.NewString())

	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE library_id=$1`, f.lib)
		_, _ = pool.Exec(c, `DELETE FROM albums WHERE library_id=$1`, f.lib)
		_, _ = pool.Exec(c, `DELETE FROM upload_sessions WHERE library_id=$1`, f.lib)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, f.lib)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, prov)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id=$1`, uploader)
	})
	return f
}

func TestAutoplayUploadPlaysRestOfAlbumInOrder(t *testing.T) {
	pool := testPool(t)
	f := seedUploadFixture(t, pool)
	s := &Server{Pool: pool}
	seed := f.albumTracks[1]
	got := s.autoplayUploadTracks(context.Background(), uuid.Nil, seed, []uuid.UUID{seed})
	want := []uuid.UUID{f.albumTracks[2], f.albumTracks[0]}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestAutoplayUploadFallsBackToUploaderSongs(t *testing.T) {
	pool := testPool(t)
	f := seedUploadFixture(t, pool)
	s := &Server{Pool: pool}
	// The single sits in the scanner's placeholder album, so no album rule.
	got := s.autoplayUploadTracks(context.Background(), uuid.Nil, f.single, []uuid.UUID{f.single})
	if len(got) == 0 {
		t.Fatal("expected uploader fallback")
	}
	for _, id := range got {
		if id == f.youtube || id == f.single {
			t.Fatalf("unexpected %s in %v", id, got)
		}
	}
	// Once the album is queued, a seed from it falls back to the uploader's songs too.
	have := append([]uuid.UUID{}, f.albumTracks...)
	got = s.autoplayUploadTracks(context.Background(), uuid.Nil, f.albumTracks[0], have)
	if len(got) != 2 {
		t.Fatalf("want single and other upload, got %v", got)
	}
}

func TestAutoplayUploadSkipsDownloadedSongs(t *testing.T) {
	pool := testPool(t)
	f := seedUploadFixture(t, pool)
	s := &Server{Pool: pool}
	if got := s.autoplayUploadTracks(context.Background(), uuid.Nil, f.youtube, nil); got != nil {
		t.Fatalf("downloaded seed should use normal autoplay, got %v", got)
	}
}

func TestReplenishAutoplayPrefersUploadedAlbum(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	f := seedUploadFixture(t, pool)
	play := playback.New(pool)
	s := &Server{Pool: pool, Play: play}
	u := seedQueueUser(t, pool, "")
	sid, err := play.WebSession(ctx, u.ID, "web")
	if err != nil {
		t.Fatal(err)
	}
	if err := play.Replace(ctx, sid, []uuid.UUID{f.albumTracks[0]}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE playback_sessions SET autoplay=true, user_id=$2 WHERE id=$1`, sid, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO library_grants (library_id, user_id) VALUES ($1, $2)`, f.lib, u.ID); err != nil {
		t.Fatal(err)
	}
	called := 0
	s.autoplaySelectHook = func(context.Context, uuid.UUID, []uuid.UUID) []uuid.UUID {
		called++
		return nil
	}
	if err := s.ReplenishAutoplay(ctx, sid); err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Fatal("normal autoplay ran although the album rule applied")
	}
	got, err := sessionQueueTrackIDs(ctx, s, sid)
	if err != nil {
		t.Fatal(err)
	}
	want := []uuid.UUID{f.albumTracks[0], f.albumTracks[1], f.albumTracks[2]}
	if len(got) != len(want) {
		t.Fatalf("queue %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("queue %v want %v", got, want)
		}
	}
}
