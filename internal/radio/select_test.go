package radio

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/testdb"
)

func TestTrackRadioDoesNotCrossGenreFromRecentSeeds(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lib, rap, classical, user := seedGenreFixture(t, pool)

	svc := New(pool)
	got, err := svc.Select(ctx, Request{
		Kind:    "track",
		SeedID:  rap,
		Limit:   8,
		UserID:  user,
		Libs:    []uuid.UUID{lib},
		Exclude: []uuid.UUID{rap},
		Recent:  40,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range got.TrackIDs {
		if id == classical {
			t.Fatalf("UK rap seed returned classical track %s", id)
		}
	}
}

func TestTrackRadioEmptyWithoutArtistOrGenre(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lib, seed := seedBareTrack(t, pool)

	svc := New(pool)
	got, err := svc.Select(ctx, Request{
		Kind:   "track",
		SeedID: seed,
		Limit:  8,
		Libs:   []uuid.UUID{lib},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.TrackIDs) != 0 {
		t.Fatalf("expected empty fill, got %v", got.TrackIDs)
	}
}

func seedGenreFixture(t *testing.T, pool *pgxpool.Pool) (lib, rap, classical, user uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	sid, lib := uuid.New(), uuid.New()
	rap, classical = uuid.New(), uuid.New()
	rapB, classB := uuid.New(), uuid.New()
	artistRap, artistClass := uuid.New(), uuid.New()
	gRap, gClass := uuid.New(), uuid.New()
	user = uuid.New()

	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc)
		VALUES ($1, $2, 'local', $3)`, sid, "radio-"+sid.String()[:8], []byte(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id, root_prefix, read_only)
		VALUES ($1, $2, 'music', $3, '', false)`, lib, "radio-"+lib.String()[:8], sid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, username, password_hash, display_name)
		VALUES ($1,$2,'x',$2)`, user, "radio-"+user.String()[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO artists (id, name) VALUES ($1,'Stormzy'), ($2,'Mozart')`, artistRap, artistClass); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO genres (id, name) VALUES ($1,$3), ($2,$4)`,
		gRap, gClass, "UK Rap "+gRap.String()[:8], "Classical "+gClass.String()[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title, duration_ms, genre_text)
		VALUES ($1,$2,'Vossi Bop',180000,'UK Rap'),
		       ($3,$2,'Big for Your Boots',200000,'UK Rap'),
		       ($4,$2,'Eine kleine Nachtmusik',300000,'Classical'),
		       ($5,$2,'Symphony 40',280000,'Classical')`,
		rap, lib, rapB, classical, classB); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO track_artists (track_id, artist_id, role, position)
		VALUES ($1,$2,'primary',0), ($3,$2,'primary',0), ($4,$5,'primary',0), ($6,$5,'primary',0)`,
		rap, artistRap, rapB, classical, artistClass, classB); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO track_genres (track_id, genre_id)
		VALUES ($1,$2), ($3,$2), ($4,$5), ($6,$5)`,
		rap, gRap, rapB, classical, gClass, classB); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO listen_history (user_id, track_id, played_at)
		VALUES ($1,$2, now() - interval '1 minute'), ($1,$3, now() - interval '2 minutes')`,
		user, classical, rap); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM listen_history WHERE user_id=$1`, user)
		_, _ = pool.Exec(c, `DELETE FROM track_genres WHERE track_id IN ($1,$2,$3,$4)`, rap, rapB, classical, classB)
		_, _ = pool.Exec(c, `DELETE FROM track_artists WHERE track_id IN ($1,$2,$3,$4)`, rap, rapB, classical, classB)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id IN ($1,$2,$3,$4)`, rap, rapB, classical, classB)
		_, _ = pool.Exec(c, `DELETE FROM genres WHERE id IN ($1,$2)`, gRap, gClass)
		_, _ = pool.Exec(c, `DELETE FROM artists WHERE id IN ($1,$2)`, artistRap, artistClass)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id=$1`, user)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, lib)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, sid)
	})
	return lib, rap, classical, user
}

func seedBareTrack(t *testing.T, pool *pgxpool.Pool) (lib, track uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	sid, lib, track := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc)
		VALUES ($1, $2, 'local', $3)`, sid, "bare-"+sid.String()[:8], []byte(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id, root_prefix, read_only)
		VALUES ($1, $2, 'music', $3, '', false)`, lib, "bare-"+lib.String()[:8], sid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title, duration_ms)
		VALUES ($1,$2,'Untitled',1000)`, track, lib); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=$1`, track)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, lib)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, sid)
	})
	return lib, track
}
