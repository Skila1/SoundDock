package audit

import (
	"context"
	"net"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/oplog"
)

type Log struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Log { return &Log{pool: pool} }

// Event records an administrative change. Meta is scrubbed of credentials and
// the row carries the request id so it links to the matching Activity entry.
func (l *Log) Event(ctx context.Context, actor *uuid.UUID, action, target, ip string, meta map[string]any) {
	if l == nil || l.pool == nil {
		return
	}
	reqID := ""
	if r := oplog.RequestFrom(ctx); r != nil {
		reqID = r.ID
		if ip == "" {
			ip = r.IP
		}
		if actor == nil {
			actor, _, _ = r.Actor()
		}
		r.MarkAudited()
	}
	// Callers pass r.RemoteAddr; store the address without the port.
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	if actor != nil && *actor == uuid.Nil {
		actor = nil
	}
	if meta != nil {
		meta = oplog.RedactDetails(meta)
	}
	_, err := l.pool.Exec(ctx, `INSERT INTO audit_events (actor_user_id, action, target, ip, meta, request_id) VALUES ((SELECT id FROM users WHERE id=$1),$2,$3,$4,coalesce($5::jsonb,'{}'),$6)`,
		actor, action, target, ip, jsonOrEmpty(meta), reqID)
	if err != nil {
		// Schemas before 0027 have no request_id column.
		_, _ = l.pool.Exec(ctx, `INSERT INTO audit_events (actor_user_id, action, target, ip, meta) VALUES ((SELECT id FROM users WHERE id=$1),$2,$3,$4,coalesce($5::jsonb,'{}'))`,
			actor, action, target, ip, jsonOrEmpty(meta))
	}
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
