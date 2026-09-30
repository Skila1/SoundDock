package playback

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestOnlyExplicitSongsLandInPersonalLibrary(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)

	stor, lib := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO storage_providers (id, name, type, config_enc) VALUES ($1,$2,'local',$3)`, stor, "pl-"+stor.String()[:8], []byte("/tmp")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO libraries (id, name, kind, storage_provider_id) VALUES ($1,$2,'music',$3)`, lib, "pl-"+lib.String()[:8], stor); err != nil {
		t.Fatal(err)
	}
	var tracks []uuid.UUID
	for i := 0; i < 6; i++ {
		id := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO tracks (id, library_id, title, duration_ms) VALUES ($1,$2,'T',1000)`, id, lib); err != nil {
			t.Fatal(err)
		}
		tracks = append(tracks, id)
	}
	sid, err := e.WebSession(ctx, userID, "browser-lib")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM personal_library_owners WHERE user_id=$1`, userID)
		_, _ = pool.Exec(c, `DELETE FROM playback_queue_items WHERE session_id=$1`, sid)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE library_id=$1`, lib)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, lib)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, stor)
	})
	user := WithOrigin(WithRequester(ctx, userID, ""), OriginUser)
	inLibrary := func() map[uuid.UUID]bool {
		rows, err := pool.Query(ctx, `
			SELECT e.track_id FROM personal_library_entries e
			JOIN personal_library_owners o ON o.id=e.owner_id WHERE o.user_id=$1`, userID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[uuid.UUID]bool{}
		for rows.Next() {
			var id uuid.UUID
			_ = rows.Scan(&id)
			out[id] = true
		}
		return out
	}

	// Playing a whole album or playlist is not a request for each song.
	if err := e.Replace(user, sid, tracks[:3], 1); err != nil {
		t.Fatal(err)
	}
	if got := inLibrary(); len(got) != 0 {
		t.Fatalf("playing a collection added %d songs", len(got))
	}
	// Queueing several at once is also a collection.
	if err := e.Add(user, sid, tracks[3:5], false); err != nil {
		t.Fatal(err)
	}
	if got := inLibrary(); len(got) != 0 {
		t.Fatalf("queueing a collection added %d songs", len(got))
	}
	// Picking one song is explicit, whether played or queued.
	if err := e.Replace(user, sid, tracks[5:6], 0); err != nil {
		t.Fatal(err)
	}
	if err := e.Add(user, sid, tracks[0:1], true); err != nil {
		t.Fatal(err)
	}
	got := inLibrary()
	if len(got) != 2 || !got[tracks[5]] || !got[tracks[0]] {
		t.Fatalf("explicit songs %v", got)
	}
	// Autoplay and radio never count, even one track at a time.
	if err := e.Add(WithOrigin(WithRequester(ctx, userID, ""), OriginAutoplay), sid, tracks[1:2], false); err != nil {
		t.Fatal(err)
	}
	if err := e.Add(WithOrigin(WithRequester(ctx, userID, ""), OriginRadio), sid, tracks[2:3], false); err != nil {
		t.Fatal(err)
	}
	if got := inLibrary(); len(got) != 2 {
		t.Fatalf("autoplay/radio added songs: %v", got)
	}
}
