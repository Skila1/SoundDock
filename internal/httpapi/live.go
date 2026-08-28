package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/sounddock/sounddock/internal/playback"
)

// ListenLive wakes on compact pg_notify, loads session state from the DB, then
// publishes to authorized SSE subscribers. It never treats the notify payload
// as an authoritative queue body.
func (s *Server) ListenLive(ctx context.Context) {
	if s == nil || s.Pool == nil {
		return
	}
	for ctx.Err() == nil {
		if err := s.listenLiveOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("live listen", "err", err)
			time.Sleep(time.Second)
		}
	}
}

func (s *Server) listenLiveOnce(ctx context.Context) error {
	conn, err := s.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+playback.NotifyChannel); err != nil {
		return err
	}
	for ctx.Err() == nil {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if n == nil {
			continue
		}
		s.handleLivePayload(ctx, n.Payload)
	}
	return nil
}

func (s *Server) handleLivePayload(ctx context.Context, raw string) {
	var sig playback.Signal
	if err := json.Unmarshal([]byte(raw), &sig); err != nil {
		return
	}
	switch sig.Scope {
	case "", "session", "party":
		sid, err := uuid.Parse(sig.SID)
		if err != nil || sid == uuid.Nil || s.Play == nil {
			return
		}
		q, err := s.Play.Get(ctx, sid)
		if err != nil || q == nil {
			return
		}
		s.attachQueueMediaState(ctx, q)
		switch sig.T {
		case "session.playhead":
			s.publishQueueSSE(sid, q, true)
		case "party.state":
			s.sessionHub().publish(sid, "party.state", map[string]any{"sid": sig.SID, "rev": sig.Rev, "resync": sig.Resync})
		default:
			s.publishQueueSSE(sid, q, false)
		}
	case "user":
		uid, err := uuid.Parse(sig.Actor)
		if err != nil || uid == uuid.Nil {
			return
		}
		switch sig.T {
		case "job.progress":
			s.sessionHub().publishUser(uid, "job.progress", map[string]any{
				"job_id":   sig.RID,
				"progress": sig.Rev,
				"keys":     sig.Keys,
			})
		case "resource.invalidate":
			s.publishUserInvalidate(uid, sig)
		}
	}
}

func (s *Server) publishInvalidate(sig playback.Signal) {
	payload := map[string]any{
		"scope":  sig.Scope,
		"keys":   sig.Keys,
		"ids":    filterInvalidateIDs(sig),
		"resync": sig.Resync,
	}
	s.sessionHub().broadcastScoped(sig.Scope, "resource.invalidate", payload)
}

func (s *Server) publishUserInvalidate(userID uuid.UUID, sig playback.Signal) {
	payload := map[string]any{
		"scope":  sig.Scope,
		"keys":   sig.Keys,
		"ids":    filterInvalidateIDs(sig),
		"resync": sig.Resync,
	}
	s.sessionHub().publishUser(userID, "resource.invalidate", payload)
}

func filterInvalidateIDs(sig playback.Signal) []string {
	if len(sig.IDs) > 32 {
		return nil
	}
	return sig.IDs
}

func (h *sessionHub) broadcastScoped(scope, name string, payload any) {
	if h == nil {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, live := range h.sessions {
		for sub := range live.subs {
			select {
			case sub.ch <- sseEvent{name: name, data: data}:
			default:
			}
		}
	}
	_ = scope
}

func (h *sessionHub) publishUser(userID uuid.UUID, name string, payload any) {
	if h == nil || userID == uuid.Nil {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, live := range h.sessions {
		if _, ok := live.presence[userID]; !ok {
			continue
		}
		ev := sseEvent{name: name, data: data}
		for sub := range live.subs {
			select {
			case sub.ch <- ev:
			default:
			}
		}
	}
}

// WaitForNotification is satisfied by pgx connections.
var _ = pgx.ErrNoRows
