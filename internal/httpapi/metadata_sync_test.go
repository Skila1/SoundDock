package httpapi

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSplitArtistNames(t *testing.T) {
	got := splitArtistNames(" Alpha , beta,, ALPHA ,Gamma ")
	want := []string{"Alpha", "beta", "Gamma"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
	if len(splitArtistNames(" , ")) != 0 {
		t.Fatal("blank list should be empty")
	}
}

func TestLockableTrackFieldsOnlyAutomatedFields(t *testing.T) {
	got := lockableTrackFields([]string{"title", "keep_forever", "artist", "manual_gain_db", "album", "lyrics", "explicit"})
	want := []string{"title", "artist", "album"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLockTrackFieldsSQLKeepsExistingLocks(t *testing.T) {
	if !strings.Contains(lockTrackFieldsSQL, "ON CONFLICT (entity_type, entity_id, field) DO NOTHING") {
		t.Fatal("auto locks must not overwrite an existing lock")
	}
	if !strings.Contains(lockTrackFieldsSQL, "t.id = ANY($1)") {
		t.Fatal("auto locks only for existing tracks")
	}
}

func TestMergeArtistDropDupesSQL(t *testing.T) {
	sql := mergeArtistDropDupesSQL("track_artists", "track_id")
	for _, want := range []string{"DELETE FROM track_artists f", "f.artist_id=$2", "i.track_id=f.track_id", "i.role=f.role", "i.artist_id=$1"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("missing %q in %s", want, sql)
		}
	}
}

// metaFixture is one library with one playable track credited to two artists,
// the second as featured, the way a MusicBrainz refresh leaves it.
type metaFixture struct {
	s                *Server
	lib, track, user uuid.UUID
}

func newMetaFixture(t *testing.T) metaFixture {
	pool := testPool(t)
	ctx := context.Background()
	stor, lib, track, user := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	tag := lib.String()[:8]
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO storage_providers (id, name, type, config_enc) VALUES ($1,$2,'local',$3)`, stor, "meta-"+tag, []byte("/tmp"))
	exec(`INSERT INTO libraries (id, name, kind, storage_provider_id) VALUES ($1,$2,'music',$3)`, lib, "meta-"+tag, stor)
	exec(`INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`, user, "meta-"+tag)
	exec(`INSERT INTO tracks (id, library_id, title, duration_ms, genre_text) VALUES ($1,$2,'Old Title',1000,'Rock')`, track, lib)
	var a1, a2 uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO artists (name) VALUES ($1) RETURNING id`, "Old Main "+tag).Scan(&a1); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO artists (name) VALUES ($1) RETURNING id`, "Old Feat "+tag).Scan(&a2); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO track_artists (track_id, artist_id, role, position) VALUES ($1,$2,'primary',0), ($1,$3,'featured',1)`, track, a1, a2)
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM metadata_locks WHERE entity_id=$1`, track)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=$1`, track)
		_, _ = pool.Exec(c, `DELETE FROM albums WHERE library_id=$1`, lib)
		_, _ = pool.Exec(c, `DELETE FROM artists WHERE name LIKE $1`, "%"+tag)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id=$1`, user)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, lib)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, stor)
	})
	return metaFixture{s: &Server{Pool: pool}, lib: lib, track: track, user: user}
}

func (f metaFixture) locks(t *testing.T) []string {
	t.Helper()
	rows, err := f.s.Pool.Query(context.Background(), `SELECT field FROM metadata_locks WHERE entity_type='track' AND entity_id=$1 ORDER BY field`, f.track)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func TestApplyTrackMetaShowsOnHomeAndLocksEditedFields(t *testing.T) {
	f := newMetaFixture(t)
	ctx := context.Background()
	tag := f.lib.String()[:8]
	title, genre, artist, album := "New Title", "Rock", "New Main "+tag+", Second "+tag, "New Album "+tag
	f.s.applyTrackMeta(ctx, f.track, trackMetaBody{Title: &title, Genre: &genre, Artist: &artist, Album: &album}, f.user)

	var gotTitle, gotAlbum, gotArtist string
	if err := f.s.Pool.QueryRow(ctx, `SELECT title, album, artist FROM (`+homeRecentlyAddedSQL()+`) x WHERE id=$2`, []uuid.UUID{f.lib}, f.track).
		Scan(&gotTitle, &gotAlbum, &gotArtist); err != nil {
		t.Fatal(err)
	}
	if gotTitle != title || gotAlbum != album || gotArtist != artist {
		t.Fatalf("home row = %q / %q / %q", gotTitle, gotAlbum, gotArtist)
	}
	if got := f.s.trackCreditNames(ctx, f.track); !slices.Equal(got, splitArtistNames(artist)) {
		t.Fatalf("featured credit left behind: %q", got)
	}
	// Genre was sent unchanged, so only the fields that changed are pinned.
	if got, want := f.locks(t), []string{"album", "artist", "title"}; !slices.Equal(got, want) {
		t.Fatalf("locks = %q want %q", got, want)
	}

	// Locks protect against scans, not editors: a second edit still lands.
	again := "Edited Again"
	f.s.applyTrackMeta(ctx, f.track, trackMetaBody{Title: &again}, f.user)
	if err := f.s.Pool.QueryRow(ctx, `SELECT title FROM tracks WHERE id=$1`, f.track).Scan(&gotTitle); err != nil {
		t.Fatal(err)
	}
	if gotTitle != again {
		t.Fatalf("locked field blocked the editor: %q", gotTitle)
	}
}

func TestReplaceTrackArtistsUnchangedKeepsRoles(t *testing.T) {
	f := newMetaFixture(t)
	ctx := context.Background()
	tag := f.lib.String()[:8]
	same := "Old Main " + tag + ", Old Feat " + tag
	if f.s.replaceTrackArtists(ctx, f.track, same) {
		t.Fatal("unchanged credit reported as changed")
	}
	var featured int
	_ = f.s.Pool.QueryRow(ctx, `SELECT count(*) FROM track_artists WHERE track_id=$1 AND role='featured'`, f.track).Scan(&featured)
	if featured != 1 {
		t.Fatalf("featured rows = %d, want 1", featured)
	}
	if len(f.locks(t)) != 0 {
		t.Fatal("saving an unchanged form must not lock fields")
	}
}

func TestLockAlbumTracksAfterRename(t *testing.T) {
	f := newMetaFixture(t)
	ctx := context.Background()
	var albumID uuid.UUID
	if err := f.s.Pool.QueryRow(ctx, `INSERT INTO albums (title, library_id) VALUES ('Tagged', $1) RETURNING id`, f.lib).Scan(&albumID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Pool.Exec(ctx, `UPDATE tracks SET album_id=$2 WHERE id=$1`, f.track, albumID); err != nil {
		t.Fatal(err)
	}
	f.s.lockAlbumTracks(ctx, albumID, f.user)
	f.s.lockAlbumTracks(ctx, albumID, uuid.Nil)
	if got := f.locks(t); !slices.Equal(got, []string{"album"}) {
		t.Fatalf("locks = %q", got)
	}
}
