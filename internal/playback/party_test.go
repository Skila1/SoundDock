package playback

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func partyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("SD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SD_TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skip(err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skip(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestPartyEnableVoteExpire(t *testing.T) {
	pool := partyPool(t)
	ctx := context.Background()
	e := New(pool)
	track := uuid.MustParse("00000000-0000-4000-8000-000000000050")
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE id=$1`, track).Scan(&n); err != nil || n != 1 {
		t.Skip("fixture track missing")
	}
	hostID := uuid.New()
	guestID := uuid.New()
	for _, u := range []uuid.UUID{hostID, guestID} {
		name := "p1p-" + u.String()[:8]
		if _, err := pool.Exec(ctx, `INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`, u, name); err != nil {
			t.Skip(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM party_votes WHERE session_id IN (SELECT id FROM playback_sessions WHERE user_id=$1)`, hostID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM party_members WHERE session_id IN (SELECT id FROM playback_sessions WHERE user_id=$1)`, hostID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_sessions WHERE user_id=$1`, hostID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1 OR id=$2`, hostID, guestID)
	})
	sid, err := e.WebSession(ctx, hostID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.EnableParty(ctx, sid, guestID, time.Hour); err == nil {
		t.Fatal("guest cannot enable")
	}
	exp, err := e.EnableParty(ctx, sid, hostID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !exp.After(time.Now()) {
		t.Fatal("expiry")
	}
	if err := e.JoinParty(ctx, sid, guestID); err != nil {
		t.Fatal(err)
	}
	if err := e.Vote(ctx, sid, guestID, track); err != nil {
		t.Fatal(err)
	}
	st, err := e.GetParty(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Enabled || len(st.Members) != 2 || len(st.Votes) != 1 {
		t.Fatalf("state %+v", st)
	}
	if err := e.DisableParty(ctx, sid, guestID); err == nil {
		t.Fatal("guest cannot disable")
	}
	_, _ = pool.Exec(ctx, `UPDATE playback_sessions SET party_expires_at=now()-interval '1 second' WHERE id=$1`, sid)
	if err := e.ExpireParty(ctx, sid); err != nil {
		t.Fatal(err)
	}
	st, err = e.GetParty(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if st.Enabled {
		t.Fatal("expired party still enabled")
	}
	if len(st.Members) != 0 || len(st.Votes) != 0 {
		t.Fatalf("cleared %+v", st)
	}
}
