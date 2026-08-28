package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
	"github.com/sounddock/sounddock/internal/jobs"
)

func TestDeleteTracksCompletesInRequest(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	sid, libID, trackID := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc)
		VALUES ($1,$2,'managed',$3)`, sid, "del-"+sid.String()[:8], []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id)
		VALUES ($1,$2,'music',$3)`, libID, "lib-"+libID.String()[:8], sid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title) VALUES ($1,$2,'gone')`, trackID, libID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, libID)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, sid)
	})

	var before int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE type='tracks.bulk_delete'`).Scan(&before); err != nil {
		t.Fatal(err)
	}

	s := &Server{Pool: pool, Jobs: jobs.New(pool, nil)}
	admin := &auth.User{ID: uuid.New(), Username: "deladmin", IsAdmin: true}
	req := authedJSON(admin, http.MethodPost, "/api/v1/tracks/bulk", map[string]any{
		"ids":    []string{trackID.String()},
		"delete": true,
	})
	rec := httptest.NewRecorder()
	s.deleteTracks(rec, req, []uuid.UUID{trackID}, false, uuid.Nil, false)
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if queued, _ := body["queued"].(bool); queued {
		t.Fatal("delete must not enqueue a job")
	}
	if body["deleted"] != float64(1) {
		t.Fatalf("deleted %v", body["deleted"])
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE id=$1`, trackID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("track still present")
	}
	var after int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE type='tracks.bulk_delete'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("enqueued delete job: %d -> %d", before, after)
	}
}

func TestBulkTrackMetadataCompletesInRequest(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	sid, libID, trackID := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc)
		VALUES ($1,$2,'managed',$3)`, sid, "meta-"+sid.String()[:8], []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id)
		VALUES ($1,$2,'music',$3)`, libID, "lib-"+libID.String()[:8], sid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title, genre_text) VALUES ($1,$2,'song','Pop')`, trackID, libID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, libID)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, sid)
	})

	var before int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE type='tracks.metadata'`).Scan(&before); err != nil {
		t.Fatal(err)
	}

	s := &Server{Pool: pool, Jobs: jobs.New(pool, nil)}
	admin := &auth.User{ID: uuid.New(), Username: "metaadmin", IsAdmin: true}
	req := authedJSON(admin, http.MethodPost, "/api/v1/tracks/bulk/metadata", map[string]any{
		"ids":   []string{trackID.String()},
		"genre": "Disco",
	})
	rec := httptest.NewRecorder()
	s.bulkTrackMetadata(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if queued, _ := body["queued"].(bool); queued {
		t.Fatal("metadata must not enqueue a job")
	}
	var genre string
	if err := pool.QueryRow(ctx, `SELECT genre_text FROM tracks WHERE id=$1`, trackID).Scan(&genre); err != nil {
		t.Fatal(err)
	}
	if genre != "Disco" {
		t.Fatalf("genre %q", genre)
	}
	var after int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE type='tracks.metadata'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("enqueued metadata job: %d -> %d", before, after)
	}
}
