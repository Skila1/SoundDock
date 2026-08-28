package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
)

func TestTrackCursorRoundtrip(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ts := time.Date(2026, 8, 29, 10, 11, 12, 123456789, time.UTC)
	raw := encodeTrackCursor(ts, id)
	gotTS, gotID, err := parseTrackCursor(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !gotTS.Equal(ts) || gotID != id {
		t.Fatalf("cursor %s -> %s %s", raw, gotTS, gotID)
	}
	if _, _, err := parseTrackCursor("%%%"); err == nil {
		t.Fatal("bad cursor must error")
	}
	if _, _, err := parseTrackCursor(""); err != nil {
		t.Fatal(err)
	}
}

func TestListTracksSQLContract(t *testing.T) {
	sql := listTracksSQL()
	if !strings.Contains(sql, "LIMIT $4") {
		t.Fatal("paginated LIMIT")
	}
	if !strings.Contains(sql, "t.created_at DESC") || !strings.Contains(sql, "t.id DESC") {
		t.Fatal("stable created_at,id order")
	}
	if !strings.Contains(sql, "library_id = ANY($1)") {
		t.Fatal("grant-scoped")
	}
	if strings.Contains(sql, "LIMIT 10000") {
		t.Fatal("must not dump 10k")
	}
}

func TestTrackPageLimit(t *testing.T) {
	if trackPageLimit("") != defaultTrackPage {
		t.Fatal("default 100")
	}
	if trackPageLimit("2") != 2 {
		t.Fatal("honor limit")
	}
	if trackPageLimit("999") != maxTrackPage {
		t.Fatal("clamp")
	}
}

func TestListTracksCursorContract(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Server{Pool: pool}
	fix := seedGrantLibs(t, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO library_grants (library_id, user_id, actions)
		VALUES ($1,$2, ARRAY['read','stream'])`, fix.libA, fix.userID); err != nil {
		t.Fatal(err)
	}
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title, duration_ms, created_at)
		VALUES
			($1,$4,'one',1000, timestamptz '2026-01-03 00:00:00Z'),
			($2,$4,'two',1000, timestamptz '2026-01-02 00:00:00Z'),
			($3,$4,'three',1000, timestamptz '2026-01-01 00:00:00Z')`,
		a, b, c, fix.libA); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE id=ANY($1)`, []uuid.UUID{a, b, c})
	})

	u := &auth.User{ID: fix.userID, Username: "user", Permissions: []string{"tracks.read"}}
	rec := httptest.NewRecorder()
	s.listTracks(rec, authedJSON(u, "GET", "/api/v1/tracks?limit=2", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var page struct {
		Items      []map[string]any `json:"items"`
		NextCursor *string          `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items %d want 2 body %s", len(page.Items), rec.Body.String())
	}
	if page.NextCursor == nil || *page.NextCursor == "" {
		t.Fatal("next_cursor required when more rows exist")
	}
	if asString(page.Items[0]["id"]) != a.String() || asString(page.Items[1]["id"]) != b.String() {
		t.Fatalf("order %+v", page.Items)
	}

	rec2 := httptest.NewRecorder()
	s.listTracks(rec2, authedJSON(u, "GET", "/api/v1/tracks?limit=2&cursor="+*page.NextCursor, nil))
	if rec2.Code != 200 {
		t.Fatalf("page2 status %d body %s", rec2.Code, rec2.Body.String())
	}
	var page2 struct {
		Items      []map[string]any `json:"items"`
		NextCursor *string          `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &page2); err != nil {
		t.Fatal(err)
	}
	if len(page2.Items) != 1 || asString(page2.Items[0]["id"]) != c.String() {
		t.Fatalf("page2 %+v", page2.Items)
	}
	if page2.NextCursor != nil {
		t.Fatalf("last page next_cursor %v", page2.NextCursor)
	}
}
