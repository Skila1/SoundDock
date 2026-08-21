package search

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPlayFilterSQLNeverPlayed(t *testing.T) {
	q := Parse("played:never")
	sql, args := playFilterSQL(context.Background(), q, nil)
	if !strings.Contains(sql, "NOT EXISTS") || !strings.Contains(sql, "play_counts") {
		t.Fatalf("sql=%s args=%v", sql, args)
	}
	uid := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	sql, args = playFilterSQL(WithUser(context.Background(), uid), q, nil)
	if !strings.Contains(sql, "pc.user_id") || len(args) != 1 {
		t.Fatalf("user scoped sql=%s args=%v", sql, args)
	}
}

func TestPlayFilterSQLLastPlayed(t *testing.T) {
	q := Parse("last_played:7d")
	sql, args := playFilterSQL(context.Background(), q, nil)
	if !strings.Contains(sql, "last_played_at >=") || len(args) != 1 {
		t.Fatalf("sql=%s args=%v", sql, args)
	}
	if _, ok := args[0].(time.Time); !ok {
		t.Fatalf("want time arg, got %T", args[0])
	}
	q = Parse("last_played:>30d")
	sql, _ = playFilterSQL(context.Background(), q, nil)
	if !strings.Contains(sql, "last_played_at <") {
		t.Fatalf("before: %s", sql)
	}
}
