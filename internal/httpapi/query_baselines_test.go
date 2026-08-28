package httpapi

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestQueryBaselinesExplain(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	uid := uuid.New()
	lib := uuid.New()
	sid := uuid.New()
	track := uuid.New()
	sess := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc) VALUES ($1,$2,'managed',$3)`,
		sid, "qb-"+sid.String()[:8], []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id) VALUES ($1,$2,'music',$3)`,
		lib, "qb-"+lib.String()[:8], sid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`,
		uid, "qb-"+uid.String()[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title, duration_ms) VALUES ($1,$2,'qb',1000)`, track, lib); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO listen_history (user_id, track_id, played_at, duration_ms, source)
		VALUES ($1,$2,now(),1000,'web')`, uid, track); err != nil {
		t.Fatal(err)
	}
	_, _ = pool.Exec(ctx, `
		INSERT INTO listen_events (user_id, track_id, started_at, source, qualified_play, kind)
		VALUES ($1,$2,now(),'web',true,'qualify')`, uid, track)
	if _, err := pool.Exec(ctx, `
		INSERT INTO playback_sessions (id, kind, owner_key, user_id, status)
		VALUES ($1,'web_device',$2,$3,'paused')`, sess, uid.String()+":qb", uid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO library_grants (library_id, user_id, actions)
		VALUES ($1,$2, ARRAY['read','stream'])`, lib, uid); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM listen_events WHERE user_id=$1`, uid)
		_, _ = pool.Exec(c, `DELETE FROM listen_history WHERE user_id=$1`, uid)
		_, _ = pool.Exec(c, `DELETE FROM playback_queue_items WHERE session_id=$1`, sess)
		_, _ = pool.Exec(c, `DELETE FROM playback_sessions WHERE id=$1`, sess)
		_, _ = pool.Exec(c, `DELETE FROM library_grants WHERE library_id=$1`, lib)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=$1`, track)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, lib)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, sid)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id=$1`, uid)
	})

	queries := []struct {
		name string
		sql  string
		args []any
	}{
		{"home_continue_history", homeContinueSQL(false), []any{uid}},
		{"home_continue_events", homeContinueSQL(true), []any{uid}},
		{"home_recently_added", homeRecentlyAddedSQL(), []any{[]uuid.UUID{lib}}},
		{"home_most_played", homeMostPlayedSQL(), []any{uid, []uuid.UUID{lib}}},
		{"list_tracks_page", listTracksSQL(), []any{[]uuid.UUID{lib}, nil, uuid.Nil, 101}},
		{"listen_totals_history", listenTotalsSQL(false), []any{uid, []string{"web", "discord"}, nil, time.Now().Add(time.Hour)}},
		{"queue_snapshot", `
			SELECT s.id, s.status, s.renderer_kind, s.renderer_id, s.state_revision, s.playhead_sequence
			FROM playback_sessions s WHERE s.id=$1`, []any{sess}},
		{"queue_items", `
			SELECT q.id, q.position, q.track_id FROM playback_queue_items q
			WHERE q.session_id=$1 ORDER BY q.position`, []any{sess}},
		{"stats_rebuild_source", `
			SELECT user_id, track_id,
				(count(*) FILTER (WHERE kind = 'qualify'))::int,
				(count(*) FILTER (WHERE kind = 'skip'))::int,
				max(started_at) FILTER (WHERE kind = 'qualify')
			FROM listen_events
			GROUP BY user_id, track_id`, nil},
		{"acquisition_intent_coalesce", `
			SELECT id FROM acquisition_intents
			WHERE provider=$1 AND source_ref=$2 AND dest_library_id=$3 AND media_policy_id=$4
			  AND status IN ('queued','downloading','processing','scanning','ready')
			ORDER BY created_at LIMIT 1`, []any{"youtube", "dQw4w9WgXcQ", lib, "m4a-0"}},
		{"jobs_coalesce", `
			SELECT id FROM jobs
			WHERE type=$1 AND status IN ('queued','running','retry')
			  AND payload->>'coalesce_key'=$2
			ORDER BY created_at LIMIT 1`, []any{"scapex.fetch", "missing-key"}},
		{"duplicate_review_open", `
			SELECT id, group_id, status, reason, track_ids
			FROM duplicate_review_groups
			WHERE status='open'
			ORDER BY created_at DESC
			LIMIT 200`, nil},
		{"library_grant_filter", `
			SELECT library_id, actions FROM library_grants WHERE user_id=$1
			UNION ALL
			SELECT lg.library_id, lg.actions FROM library_grants lg
			JOIN user_roles ur ON ur.role_id=lg.role_id WHERE ur.user_id=$1`, []any{uid}},
	}

	for _, q := range queries {
		t.Run(q.name, func(t *testing.T) {
			plan := explainText(t, ctx, pool, q.sql, q.args...)
			t.Log(plan)
		})
	}
}

func explainText(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	rows, err := pool.Query(ctx, "EXPLAIN "+sql, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(&b, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
