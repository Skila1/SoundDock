package audit

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Log struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Log { return &Log{pool: pool} }

func (l *Log) Event(ctx context.Context, actor *uuid.UUID, action, target, ip string, meta map[string]any) {
	if l == nil {
		return
	}
	_, _ = l.pool.Exec(ctx, `INSERT INTO audit_events (actor_user_id, action, target, ip, meta) VALUES ($1,$2,$3,$4,coalesce($5::jsonb,'{}'))`,
		actor, action, target, ip, jsonOrEmpty(meta))
}

func jsonOrEmpty(m map[string]any) string {
	if m == nil {
		return "{}"
	}
	b, err := marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}
