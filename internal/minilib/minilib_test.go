package minilib

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/testdb"
)

// Dummy hash — users.password_hash is not verified in these tests.
const testPasswordHash = "$2a$10$abcdefghijklmnopqrstuuABCDEFGHIJKLMNOPQRSTUVWXYZ012345"

func TestCanSeePrivacy(t *testing.T) {
	ownerUser := uuid.New()
	other := uuid.New()
	priv := Owner{ID: uuid.New(), UserID: ownerUser, Visibility: "private"}
	pub := Owner{ID: uuid.New(), UserID: ownerUser, Visibility: "public"}

	if !CanSee(ownerUser, false, priv) {
		t.Fatal("owner must see private library")
	}
	if CanSee(other, false, priv) {
		t.Fatal("peer must not see private library")
	}
	if !CanSee(other, true, priv) {
		t.Fatal("admin must inspect private library")
	}
	if !CanSee(other, false, pub) {
		t.Fatal("peer must see public library")
	}
	if Inspecting(ownerUser, false, priv) {
		t.Fatal("owner is not inspecting")
	}
	if !Inspecting(other, true, priv) {
		t.Fatal("admin inspect flag")
	}
}

func TestRecordAndReconcile(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	userID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,$3,$2)`, userID, "ml-"+userID.String()[:8], testPasswordHash); err != nil {
		t.Fatal(err)
	}
	stor := uuid.New()
	lib := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO storage_providers (id, name, type, config_enc) VALUES ($1,$2,'local',$3)`, stor, "ml-"+stor.String()[:8], []byte("/tmp")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO libraries (id, name, kind, storage_provider_id) VALUES ($1,$2,'music',$3)`, lib, "ml-"+lib.String()[:8], stor); err != nil {
		t.Fatal(err)
	}
	trackA, trackB := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO tracks (id, library_id, title, duration_ms) VALUES ($1,$2,'A',1000), ($3,$2,'B',1000)`, trackA, lib, trackB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM personal_library_entries WHERE track_id IN ($1,$2)`, trackA, trackB)
		_, _ = pool.Exec(c, `DELETE FROM personal_library_owners WHERE user_id=$1 OR discord_user_id='999000111222'`, userID)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id IN ($1,$2)`, trackA, trackB)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, lib)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, stor)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id=$1`, userID)
	})

	if err := Record(ctx, pool, "autoplay", userID, "", []uuid.UUID{trackA}); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM personal_library_entries e JOIN personal_library_owners o ON o.id=e.owner_id WHERE o.user_id=$1`, userID).Scan(&n)
	if n != 0 {
		t.Fatalf("autoplay must not record: %d", n)
	}

	if err := Record(ctx, pool, "user", uuid.Nil, "999000111222", []uuid.UUID{trackA, trackA}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT e.request_count FROM personal_library_entries e
		JOIN personal_library_owners o ON o.id=e.owner_id
		WHERE o.discord_user_id='999000111222' AND e.track_id=$1`, trackA).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("discord-only count %d", count)
	}

	if err := Reconcile(ctx, pool, userID, "999000111222"); err != nil {
		t.Fatal(err)
	}
	if err := Record(ctx, pool, "user", userID, "999000111222", []uuid.UUID{trackB}); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM personal_library_entries e
		JOIN personal_library_owners o ON o.id=e.owner_id
		WHERE o.user_id=$1`, userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("merged library size %d", n)
	}
	var owners int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM personal_library_owners WHERE discord_user_id='999000111222' OR user_id=$1`, userID).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	if owners != 1 {
		t.Fatalf("want one owner after reconcile, got %d", owners)
	}
}

func TestReconcileDoesNotStealOtherAccount(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	a, b := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{a, b} {
		if _, err := pool.Exec(ctx, `INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,$3,$2)`, id, "ml-"+id.String()[:8], testPasswordHash); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := EnsureOwner(ctx, pool, a, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureOwner(ctx, pool, b, "888777666555"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM personal_library_owners WHERE user_id IN ($1,$2)`, a, b)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id IN ($1,$2)`, a, b)
	})
	if err := Reconcile(ctx, pool, a, "888777666555"); err != nil {
		t.Fatal(err)
	}
	var bid uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT user_id FROM personal_library_owners WHERE discord_user_id='888777666555'`).Scan(&bid); err != nil {
		t.Fatal(err)
	}
	if bid != b {
		t.Fatalf("stole identity: %s", bid)
	}
}

func TestDetachDiscordThenRelinkMerges(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	userID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,$3,$2)`, userID, "ml-"+userID.String()[:8], testPasswordHash); err != nil {
		t.Fatal(err)
	}
	stor, lib, trackA, trackB := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO storage_providers (id, name, type, config_enc) VALUES ($1,$2,'local',$3)`, stor, "ml-"+stor.String()[:8], []byte("/tmp")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO libraries (id, name, kind, storage_provider_id) VALUES ($1,$2,'music',$3)`, lib, "ml-"+lib.String()[:8], stor); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tracks (id, library_id, title, duration_ms) VALUES ($1,$2,'A',1000), ($3,$2,'B',1000)`, trackA, lib, trackB); err != nil {
		t.Fatal(err)
	}
	const did = "777666555444"
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM personal_library_entries WHERE track_id IN ($1,$2)`, trackA, trackB)
		_, _ = pool.Exec(c, `DELETE FROM personal_library_owners WHERE user_id=$1 OR discord_user_id=$2`, userID, did)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id IN ($1,$2)`, trackA, trackB)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, lib)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, stor)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id=$1`, userID)
	})
	if err := Record(ctx, pool, "user", userID, did, []uuid.UUID{trackA}); err != nil {
		t.Fatal(err)
	}
	if err := DetachDiscord(ctx, pool, userID); err != nil {
		t.Fatal(err)
	}
	if err := Record(ctx, pool, "user", uuid.Nil, did, []uuid.UUID{trackB}); err != nil {
		t.Fatal(err)
	}
	if err := Reconcile(ctx, pool, userID, did); err != nil {
		t.Fatal(err)
	}
	var n, owners int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM personal_library_entries e
		JOIN personal_library_owners o ON o.id=e.owner_id
		WHERE o.user_id=$1`, userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("relink merge size %d", n)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM personal_library_owners WHERE discord_user_id=$1 OR user_id=$2`, did, userID).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	if owners != 1 {
		t.Fatalf("want one owner after relink, got %d", owners)
	}
}
