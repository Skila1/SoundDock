package playback

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	NotifyChannel   = "sounddock_live"
	notifySoftLimit = 2048
)

// Signal is the compact pg_notify payload. It must never include a queue body.
type Signal struct {
	T      string   `json:"t"`
	SID    string   `json:"sid,omitempty"`
	RID    string   `json:"rid,omitempty"`
	Rev    int64    `json:"rev,omitempty"`
	Seq    int64    `json:"seq,omitempty"`
	Scope  string   `json:"scope"`
	Actor  string   `json:"actor,omitempty"`
	Resync bool     `json:"resync,omitempty"`
	Keys   []string `json:"keys,omitempty"`
	IDs    []string `json:"ids,omitempty"`
}

func EncodeSignal(sig Signal) ([]byte, error) {
	b, err := json.Marshal(sig)
	if err != nil {
		return nil, err
	}
	if len(b) <= notifySoftLimit {
		return b, nil
	}
	return json.Marshal(Signal{T: sig.T, SID: sig.SID, RID: sig.RID, Rev: sig.Rev, Seq: sig.Seq, Scope: sig.Scope, Resync: true})
}

func Notify(ctx context.Context, pool *pgxpool.Pool, sig Signal) error {
	if pool == nil {
		return nil
	}
	if sig.Scope == "" {
		sig.Scope = "session"
	}
	b, err := EncodeSignal(sig)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `SELECT pg_notify($1, $2)`, NotifyChannel, string(b))
	return err
}

func (e *Engine) notifySession(ctx context.Context, sid uuid.UUID, t string) {
	if e == nil || e.pool == nil || sid == uuid.Nil {
		return
	}
	var rev, seq int64
	_ = e.pool.QueryRow(ctx, `SELECT state_revision, playhead_sequence FROM playback_sessions WHERE id=$1`, sid).Scan(&rev, &seq)
	_ = Notify(ctx, e.pool, Signal{T: t, SID: sid.String(), Rev: rev, Seq: seq, Scope: "session"})
}

func (e *Engine) commitSession(ctx context.Context, tx interface {
	Commit(context.Context) error
}, sid uuid.UUID, t string) error {
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	e.notifySession(ctx, sid, t)
	e.notifyPersonalLibrary(ctx)
	return nil
}

func (e *Engine) notifyPersonalLibrary(ctx context.Context) {
	if e == nil || e.pool == nil || originFrom(ctx) != OriginUser {
		return
	}
	userID, _ := requesterFrom(ctx)
	uid, ok := userID.(uuid.UUID)
	if !ok || uid == uuid.Nil {
		return
	}
	_ = Notify(ctx, e.pool, Signal{
		T:     "resource.invalidate",
		Scope: "user",
		Actor: uid.String(),
		Keys:  []string{"personal-library", "home"},
	})
}
