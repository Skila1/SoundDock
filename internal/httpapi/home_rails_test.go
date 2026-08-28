package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
)

func TestHomeKeysPopulatedWhenDataExists(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Server{Pool: pool}
	fix := seedGrantLibs(t, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO library_grants (library_id, user_id, actions)
		VALUES ($1,$2, ARRAY['read','stream'])`, fix.libA, fix.userID); err != nil {
		t.Fatal(err)
	}
	recentID := uuid.New()
	playedID := uuid.New()
	hiddenID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title, duration_ms, created_at)
		VALUES ($1,$2,'recent',180000, now()), ($3,$2,'played',180000, now()-interval '2 days'), ($4,$5,'hidden',180000, now())`,
		recentID, fix.libA, playedID, hiddenID, fix.libB); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO play_counts (user_id, track_id, count, last_played_at)
		VALUES ($1,$2,9, now()), ($1,$3,4, now())`, fix.userID, playedID, hiddenID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM play_counts WHERE user_id=$1`, fix.userID)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=ANY($1)`, []uuid.UUID{recentID, playedID, hiddenID})
	})

	u := &auth.User{ID: fix.userID, Username: "user", Permissions: []string{"tracks.read"}}
	rec := httptest.NewRecorder()
	s.home(rec, authedJSON(u, "GET", "/api/v1/home", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		RecentlyAdded []map[string]any `json:"recently_added"`
		MostPlayed    []map[string]any `json:"most_played"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !homeHasTrack(body.RecentlyAdded, recentID) {
		t.Fatalf("recently_added missing granted track: %+v", body.RecentlyAdded)
	}
	if homeHasTrack(body.RecentlyAdded, hiddenID) {
		t.Fatalf("recently_added leaked ungranted library: %+v", body.RecentlyAdded)
	}
	if !homeHasTrack(body.MostPlayed, playedID) {
		t.Fatalf("most_played missing play_counts row: %+v", body.MostPlayed)
	}
	if homeHasTrack(body.MostPlayed, hiddenID) {
		t.Fatalf("most_played leaked ungranted library: %+v", body.MostPlayed)
	}
}

func homeHasTrack(rows []map[string]any, id uuid.UUID) bool {
	want := id.String()
	for _, row := range rows {
		if asString(row["id"]) == want {
			return true
		}
	}
	return false
}
