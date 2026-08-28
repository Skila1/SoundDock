package playback

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestMergeUndoRowsInsertsAtPosition(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	existing := []undoItem{{ID: a, TrackID: a, Position: 0, Origin: OriginUser}}
	snap := []undoItem{
		{ID: b, TrackID: b, Position: 1, Origin: OriginUser},
		{ID: c, TrackID: c, Position: 2, Origin: OriginUser},
	}
	got := mergeUndoRows(existing, snap)
	if len(got) != 3 {
		t.Fatalf("len %d", len(got))
	}
	if got[0].TrackID != a || got[1].TrackID != b || got[2].TrackID != c {
		t.Fatalf("order %+v", got)
	}
	for i, it := range got {
		if it.Position != i {
			t.Fatalf("position %d want %d", it.Position, i)
		}
	}
}

func TestMergeUndoRowsSkipsExistingID(t *testing.T) {
	a := uuid.New()
	existing := []undoItem{{ID: a, TrackID: a, Position: 0, Origin: OriginUser}}
	snap := []undoItem{{ID: a, TrackID: a, Position: 0, Origin: OriginUser}}
	got := mergeUndoRows(existing, snap)
	if len(got) != 1 {
		t.Fatalf("duplicated now-playing len=%d", len(got))
	}
}

func TestParseUndoItemsNestedAndFlat(t *testing.T) {
	tid := uuid.New()
	uid := uuid.New()
	extra := map[string]any{
		"undo_generation": int64(7),
		"items": []any{
			map[string]any{
				"id":           uuid.New().String(),
				"track_id":     tid.String(),
				"position":     float64(1),
				"origin":       OriginUser,
				"requested_by": map[string]any{"user_id": uid.String()},
			},
		},
	}
	gen, ok := undoGenerationOf(extra)
	if !ok || gen != 7 {
		t.Fatalf("generation %d %v", gen, ok)
	}
	items := parseUndoItems(extra)
	if len(items) != 1 || items[0].TrackID != tid {
		t.Fatalf("items %+v", items)
	}
	if items[0].RequestedByUserID == nil || *items[0].RequestedByUserID != uid {
		t.Fatalf("requester %+v", items[0].RequestedByUserID)
	}
}

func TestReplaceWritesRequestedByAndOrigin(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	a, b := fixtureTracks(t, pool)
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	ctx = WithRequester(ctx, userID, "discord-99")
	ctx = WithOrigin(ctx, OriginUser)
	if err := e.Replace(ctx, sid, []uuid.UUID{a, b}, 0); err != nil {
		t.Fatal(err)
	}
	var n int
	var origin string
	var reqUser uuid.UUID
	var reqDiscord string
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM playback_queue_items WHERE session_id=$1`, sid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("count %d", n)
	}
	if err := pool.QueryRow(context.Background(), `
		SELECT origin, requested_by_user_id, requested_by_discord_user_id
		FROM playback_queue_items WHERE session_id=$1 ORDER BY position LIMIT 1`, sid).Scan(&origin, &reqUser, &reqDiscord); err != nil {
		t.Fatal(err)
	}
	if origin != OriginUser {
		t.Fatalf("origin %q", origin)
	}
	if reqUser != userID {
		t.Fatalf("requested_by_user_id %s want %s", reqUser, userID)
	}
	if reqDiscord != "discord-99" {
		t.Fatalf("discord %q", reqDiscord)
	}
}

func TestAddDefaultOriginUser(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	a, b := fixtureTracks(t, pool)
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Replace(ctx, sid, []uuid.UUID{a}, 0); err != nil {
		t.Fatal(err)
	}
	if err := e.Add(ctx, sid, []uuid.UUID{b}, false); err != nil {
		t.Fatal(err)
	}
	var origin string
	var req *uuid.UUID
	if err := pool.QueryRow(ctx, `
		SELECT origin, requested_by_user_id FROM playback_queue_items
		WHERE session_id=$1 ORDER BY position DESC LIMIT 1`, sid).Scan(&origin, &req); err != nil {
		t.Fatal(err)
	}
	if origin != OriginUser {
		t.Fatalf("origin %q", origin)
	}
	if req != nil {
		t.Fatalf("requester %v want NULL", req)
	}
}

func TestQueueGetIncludesRequestedBy(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	a, _ := fixtureTracks(t, pool)
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Replace(WithRequester(ctx, userID, ""), sid, []uuid.UUID{a}, 0); err != nil {
		t.Fatal(err)
	}
	q, err := e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	items, _ := q["items"].([]map[string]any)
	if len(items) != 1 {
		t.Fatalf("items %d", len(items))
	}
	rb, ok := items[0]["requested_by"].(map[string]any)
	if !ok {
		t.Fatalf("requested_by %T %v", items[0]["requested_by"], items[0]["requested_by"])
	}
	if rb["user_id"] != userID {
		t.Fatalf("user_id %v", rb["user_id"])
	}
	if _, ok := rb["display_name"].(string); !ok {
		t.Fatalf("display_name %v", rb["display_name"])
	}
	if items[0]["origin"] != OriginUser {
		t.Fatalf("origin %v", items[0]["origin"])
	}
}

func TestAddWritesAutoplayOriginFromContext(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	a, b := fixtureTracks(t, pool)
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Replace(ctx, sid, []uuid.UUID{a}, 0); err != nil {
		t.Fatal(err)
	}
	if err := e.Add(WithOrigin(ctx, OriginAutoplay), sid, []uuid.UUID{b}, false); err != nil {
		t.Fatal(err)
	}
	var origin string
	if err := pool.QueryRow(ctx, `
		SELECT origin FROM playback_queue_items WHERE session_id=$1 ORDER BY position DESC LIMIT 1`, sid).Scan(&origin); err != nil {
		t.Fatal(err)
	}
	if origin != OriginAutoplay {
		t.Fatalf("origin %q", origin)
	}
}

func TestUndoRestoreAfterRemove(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	a, b := fixtureTracks(t, pool)
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Replace(WithRequester(ctx, userID, ""), sid, []uuid.UUID{a, b}, 0); err != nil {
		t.Fatal(err)
	}
	extra := map[string]any{"position": 1}
	if err := e.Control(ctx, sid, "remove", extra); err != nil {
		t.Fatal(err)
	}
	q, err := e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if queueLen(q) != 1 {
		t.Fatalf("after remove %d", queueLen(q))
	}
	gen, ok := extra["undo_generation"]
	if !ok {
		t.Fatal("missing undo_generation")
	}
	if int64OfAny(gen) != revOf(q) {
		t.Fatalf("undo_generation %v revision %d", gen, revOf(q))
	}
	undo, _ := extra["undo"].(map[string]any)
	if undo == nil {
		t.Fatal("missing undo snapshot")
	}
	if err := e.Control(ctx, sid, "undo", extra); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	items := queueItems(q)
	if len(items) != 2 {
		t.Fatalf("restored %d", len(items))
	}
	if items[0]["track_id"] != a || items[1]["track_id"] != b {
		t.Fatalf("tracks %v %v", items[0]["track_id"], items[1]["track_id"])
	}
	rb, _ := items[1]["requested_by"].(map[string]any)
	if rb == nil || rb["user_id"] != userID {
		t.Fatalf("restored requested_by %v", items[1]["requested_by"])
	}
}

func TestUndoStaleGeneration(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	a, b := fixtureTracks(t, pool)
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Replace(ctx, sid, []uuid.UUID{a, b}, 0); err != nil {
		t.Fatal(err)
	}
	extra := map[string]any{"position": 1}
	if err := e.Control(ctx, sid, "remove", extra); err != nil {
		t.Fatal(err)
	}
	if err := e.Control(ctx, sid, "pause", nil); err != nil {
		t.Fatal(err)
	}
	err = e.Control(ctx, sid, "undo", extra)
	if !errors.Is(err, ErrUndoStale) {
		t.Fatalf("stale: %v", err)
	}
	q, err := e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if queueLen(q) != 1 {
		t.Fatalf("stale undo mutated queue %d", queueLen(q))
	}
}

func TestUndoCommandIDReplayNoDuplicate(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	a, b := fixtureTracks(t, pool)
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Replace(ctx, sid, []uuid.UUID{a, b}, 0); err != nil {
		t.Fatal(err)
	}
	removeExtra := map[string]any{"position": 1}
	if err := e.Control(ctx, sid, "remove", removeExtra); err != nil {
		t.Fatal(err)
	}
	undoExtra := map[string]any{
		"undo_generation": removeExtra["undo_generation"],
		"items":           removeExtra["undo"].(map[string]any)["items"],
		"command_id":      "undo-cmd-1",
	}
	if err := e.Control(ctx, sid, "undo", undoExtra); err != nil {
		t.Fatal(err)
	}
	q, err := e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if queueLen(q) != 2 {
		t.Fatalf("after undo %d", queueLen(q))
	}
	if err := e.Control(ctx, sid, "undo", map[string]any{
		"undo_generation": undoExtra["undo_generation"],
		"items":           undoExtra["items"],
		"command_id":      "undo-cmd-1",
	}); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if queueLen(q) != 2 {
		t.Fatalf("replay duplicated %d", queueLen(q))
	}
	err = e.Control(ctx, sid, "undo", map[string]any{
		"undo_generation": undoExtra["undo_generation"],
		"items":           undoExtra["items"],
		"command_id":      "undo-cmd-2",
	})
	if !errors.Is(err, ErrUndoStale) {
		t.Fatalf("second undo: %v", err)
	}
}

func TestUndoClearKeepCurrentNoDuplicateNowPlaying(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	a, b := fixtureTracks(t, pool)
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Replace(ctx, sid, []uuid.UUID{a, b}, 0); err != nil {
		t.Fatal(err)
	}
	extra := map[string]any{}
	if err := e.Control(ctx, sid, "clear", extra); err != nil {
		t.Fatal(err)
	}
	q, err := e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if queueLen(q) != 1 {
		t.Fatalf("keep current %d", queueLen(q))
	}
	if items := queueItems(q); items[0]["track_id"] != a {
		t.Fatalf("now playing %v", items[0]["track_id"])
	}
	if err := e.Control(ctx, sid, "undo", extra); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	items := queueItems(q)
	if len(items) != 2 {
		t.Fatalf("restored upcoming %d", len(items))
	}
	if items[0]["track_id"] != a || items[1]["track_id"] != b {
		t.Fatalf("tracks %v %v", items[0]["track_id"], items[1]["track_id"])
	}
}

func queueLen(q map[string]any) int {
	return len(queueItems(q))
}

func queueItems(q map[string]any) []map[string]any {
	items, _ := q["items"].([]map[string]any)
	if items == nil {
		return []map[string]any{}
	}
	return items
}

func int64OfAny(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	}
	return 0
}
