package scapex

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestEnsureStubTrack(t *testing.T) {
	pool := testPool(t)
	lib := uuid.MustParse("00000000-0000-4000-8000-000000000020")
	ctx := context.Background()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM libraries WHERE id=$1`, lib).Scan(&n); err != nil || n == 0 {
		t.Skip("fixture library missing")
	}
	ref := "W6stub" + uuid.NewString()[:5]
	if len(ref) > 11 {
		ref = ref[:11]
	}
	a, err := EnsureStubTrack(ctx, pool, lib, ref)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM tracks WHERE id=$1`, a) })
	b, err := EnsureStubTrack(ctx, pool, lib, ref)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("stub not reused: %s %s", a, b)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM track_files WHERE track_id=$1`, a).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("stub should have no files")
	}
}
