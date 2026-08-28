package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/listen"
	"github.com/sounddock/sounddock/internal/playback"
	"github.com/sounddock/sounddock/internal/scrobble"
)

func requestPlaybackTarget(r *http.Request, extra map[string]any, bodyTarget string) string {
	if extra != nil {
		if s, ok := extra["target"].(string); ok && strings.TrimSpace(s) != "" {
			return strings.ToLower(strings.TrimSpace(s))
		}
	}
	if strings.TrimSpace(bodyTarget) != "" {
		return strings.ToLower(strings.TrimSpace(bodyTarget))
	}
	if q := r.URL.Query().Get("target"); q != "" {
		return strings.ToLower(strings.TrimSpace(q))
	}
	if h := r.Header.Get("X-Playback-Target"); h != "" {
		return strings.ToLower(strings.TrimSpace(h))
	}
	return ""
}

func (s *Server) playSession(r *http.Request, extra map[string]any, bodyTarget, deviceID string) (uuid.UUID, error) {
	if requestPlaybackTarget(r, extra, bodyTarget) == "discord" {
		return s.discordPlaySession(r)
	}
	u := currentUser(r)
	return s.Play.WebSession(r.Context(), u.ID, firstNonEmpty(deviceID, requestDeviceID(r, extra)))
}

func (s *Server) writePlaySessionErr(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errNotInVoice) {
		writeErr(w, 409, "not_in_voice", err.Error())
		return true
	}
	if errors.Is(err, errGuildDisabled) {
		writeErr(w, 403, "guild_disabled", err.Error())
		return true
	}
	writeErr(w, 500, "queue", err.Error())
	return true
}

func requestDeviceID(r *http.Request, extra map[string]any) string {
	if extra != nil {
		if s, ok := extra["device_id"].(string); ok && s != "" {
			return s
		}
	}
	if q := r.URL.Query().Get("device_id"); q != "" {
		return q
	}
	if h := r.Header.Get("X-Device-ID"); h != "" {
		return h
	}
	if c, err := r.Cookie("sd_device"); err == nil && c.Value != "" {
		return c.Value
	}
	return ""
}

func (s *Server) webPlaySession(r *http.Request, extra map[string]any) (uuid.UUID, error) {
	return s.playSession(r, extra, "", "")
}

func (s *Server) getQueue(w http.ResponseWriter, r *http.Request) {
	sid, err := s.webPlaySession(r, nil)
	if s.writePlaySessionErr(w, err) {
		return
	}
	q, err := s.Play.Get(r.Context(), sid)
	if err != nil {
		writeErr(w, 500, "queue", err.Error())
		return
	}
	writeJSON(w, 200, q)
}

func (s *Server) putQueue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TrackIDs []string `json:"track_ids"`
		Start    int      `json:"start"`
		DeviceID string   `json:"device_id"`
		Target   string   `json:"target"`
	}
	_ = decodeJSON(r, &body)
	sid, err := s.playSession(r, nil, body.Target, body.DeviceID)
	if s.writePlaySessionErr(w, err) {
		return
	}
	ids, err := s.resolveQueueTracks(r.Context(), body.TrackIDs)
	if err != nil {
		writeErr(w, 502, "scapex", err.Error())
		return
	}
	if err := s.Play.Replace(r.Context(), sid, ids, body.Start); err != nil {
		writeErr(w, 400, "queue", err.Error())
		return
	}
	if len(ids) > 0 && s.Hooks != nil {
		start := body.Start
		if start < 0 || start >= len(ids) {
			start = 0
		}
		s.Hooks.Emit(r.Context(), "playback.started", map[string]any{"track_id": ids[start]})
	}
	q, _ := s.Play.Get(r.Context(), sid)
	writeJSON(w, 200, q)
}

func (s *Server) queueAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TrackIDs []string `json:"track_ids"`
		Next     bool     `json:"next"`
		DeviceID string   `json:"device_id"`
		Target   string   `json:"target"`
	}
	_ = decodeJSON(r, &body)
	sid, err := s.playSession(r, nil, body.Target, body.DeviceID)
	if s.writePlaySessionErr(w, err) {
		return
	}
	ids, err := s.resolveQueueTracks(r.Context(), body.TrackIDs)
	if err != nil {
		writeErr(w, 502, "scapex", err.Error())
		return
	}
	if err := s.Play.Add(r.Context(), sid, ids, body.Next); err != nil {
		writeErr(w, 400, "queue", err.Error())
		return
	}
	q, _ := s.Play.Get(r.Context(), sid)
	writeJSON(w, 200, q)
}

func (s *Server) queueControl(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action   string         `json:"action"`
		Extra    map[string]any `json:"extra"`
		DeviceID string         `json:"device_id"`
		Target   string         `json:"target"`
	}
	_ = decodeJSON(r, &body)
	if body.Extra == nil {
		body.Extra = map[string]any{}
	}
	sid, err := s.playSession(r, body.Extra, body.Target, body.DeviceID)
	if s.writePlaySessionErr(w, err) {
		return
	}
	s.maybeListenSkip(r, sid, body.Action, body.Extra)
	if err := s.Play.Control(r.Context(), sid, body.Action, body.Extra); err != nil {
		writeErr(w, 400, "control", err.Error())
		return
	}
	if body.Action == "seek" {
		s.maybeListenProgress(r, sid, body.Extra)
	}
	if body.Action == "stop" && s.Hooks != nil {
		s.Hooks.Emit(r.Context(), "playback.finished", map[string]any{"session": sid})
	}
	q, _ := s.Play.Get(r.Context(), sid)
	writeJSON(w, 200, q)
}

func (s *Server) postListen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TrackID    uuid.UUID `json:"track_id"`
		PositionMs int       `json:"position_ms"`
		DurationMs int       `json:"duration_ms"`
		Source     string    `json:"source"`
		Event      string    `json:"event"`
		DeviceID   string    `json:"device_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "listen", "invalid json")
		return
	}
	if body.TrackID == uuid.Nil {
		writeErr(w, 400, "listen", "track_id required")
		return
	}
	if _, ok := listen.NormalizeSource(body.Source); !ok {
		writeErr(w, 400, "listen", "invalid source")
		return
	}
	if body.Source == "import" {
		writeErr(w, 400, "listen", "source import is only set by history import")
		return
	}
	if body.DurationMs <= 0 {
		_ = s.Pool.QueryRow(r.Context(), `SELECT duration_ms FROM tracks WHERE id=$1`, body.TrackID).Scan(&body.DurationMs)
	}
	u := currentUser(r)
	kind := body.Event
	if kind == "" {
		kind = "progress"
	}
	err := listen.For(s.Pool).Record(r.Context(), listen.Event{
		UserID:     u.ID,
		TrackID:    body.TrackID,
		PositionMs: body.PositionMs,
		DurationMs: body.DurationMs,
		Source:     body.Source,
		Kind:       kind,
	})
	if err != nil {
		if errors.Is(err, listen.ErrSource) {
			writeErr(w, 400, "listen", err.Error())
			return
		}
		writeErr(w, 500, "listen", err.Error())
		return
	}
	_ = scrobble.New(s.Pool, s.Box, s.Search).HandleListen(r.Context(), u.ID, scrobble.Event{
		TrackID: body.TrackID, PositionMS: body.PositionMs, DurationMS: body.DurationMs,
		Source: body.Source, Kind: kind,
	})
	sid, serr := s.Play.WebSession(r.Context(), u.ID, firstNonEmpty(body.DeviceID, requestDeviceID(r, nil)))
	if serr == nil {
		_ = s.Play.SetPosition(r.Context(), sid, body.PositionMs)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) getParty(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	sid, err := s.partySession(r, nil)
	if err != nil {
		writeErr(w, 400, "party", err.Error())
		return
	}
	if sid == uuid.Nil {
		found, ok, ferr := s.Play.FindPartyForUser(r.Context(), u.ID)
		if ferr != nil {
			writeErr(w, 500, "party", ferr.Error())
			return
		}
		if !ok {
			own, oerr := s.webPlaySession(r, nil)
			if oerr != nil {
				writeErr(w, 500, "party", oerr.Error())
				return
			}
			st, gerr := s.Play.GetParty(r.Context(), own)
			if gerr != nil {
				writeErr(w, 500, "party", gerr.Error())
				return
			}
			writeJSON(w, 200, st)
			return
		}
		sid = found
	}
	st, err := s.Play.GetParty(r.Context(), sid)
	if err != nil {
		writeErr(w, 500, "party", err.Error())
		return
	}
	writeJSON(w, 200, st)
}

func (s *Server) postParty(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled          bool       `json:"enabled"`
		ExpiresInSeconds int        `json:"expires_in_seconds"`
		SessionID        *uuid.UUID `json:"session_id"`
		DeviceID         string     `json:"device_id"`
	}
	_ = decodeJSON(r, &body)
	u := currentUser(r)
	sid, err := s.partySession(r, body.SessionID)
	if err != nil {
		writeErr(w, 400, "party", err.Error())
		return
	}
	if sid == uuid.Nil {
		sid, err = s.Play.WebSession(r.Context(), u.ID, firstNonEmpty(body.DeviceID, requestDeviceID(r, nil)))
		if err != nil {
			writeErr(w, 500, "party", err.Error())
			return
		}
	}
	if body.Enabled {
		own, _ := s.Play.WebSession(r.Context(), u.ID, firstNonEmpty(body.DeviceID, requestDeviceID(r, nil)))
		if sid != own {
			if err := s.Play.JoinParty(r.Context(), sid, u.ID); err != nil {
				writeErr(w, 403, "party", err.Error())
				return
			}
		} else {
			exp, err := s.Play.EnableParty(r.Context(), sid, u.ID, time.Duration(body.ExpiresInSeconds)*time.Second)
			if err != nil {
				writeErr(w, 403, "party", err.Error())
				return
			}
			s.enqueuePartyExpire(r, sid, exp)
		}
	} else {
		own, _ := s.Play.WebSession(r.Context(), u.ID, firstNonEmpty(body.DeviceID, requestDeviceID(r, nil)))
		if sid != own {
			if err := s.Play.LeaveParty(r.Context(), sid, u.ID); err != nil {
				writeErr(w, 403, "party", err.Error())
				return
			}
		} else {
			if err := s.Play.DisableParty(r.Context(), sid, u.ID); err != nil {
				if leaveErr := s.Play.LeaveParty(r.Context(), sid, u.ID); leaveErr != nil {
					writeErr(w, 403, "party", err.Error())
					return
				}
			}
		}
	}
	st, err := s.Play.GetParty(r.Context(), sid)
	if err != nil {
		writeErr(w, 500, "party", err.Error())
		return
	}
	writeJSON(w, 200, st)
}

func (s *Server) postPartyVote(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TrackID   uuid.UUID  `json:"track_id"`
		SessionID *uuid.UUID `json:"session_id"`
		DeviceID  string     `json:"device_id"`
	}
	_ = decodeJSON(r, &body)
	if body.TrackID == uuid.Nil {
		writeErr(w, 400, "party", "track_id required")
		return
	}
	u := currentUser(r)
	sid, err := s.partySession(r, body.SessionID)
	if err != nil {
		writeErr(w, 400, "party", err.Error())
		return
	}
	if sid == uuid.Nil {
		found, ok, ferr := s.Play.FindPartyForUser(r.Context(), u.ID)
		if ferr != nil {
			writeErr(w, 500, "party", ferr.Error())
			return
		}
		if ok {
			sid = found
		} else {
			sid, err = s.Play.WebSession(r.Context(), u.ID, firstNonEmpty(body.DeviceID, requestDeviceID(r, nil)))
			if err != nil {
				writeErr(w, 500, "party", err.Error())
				return
			}
		}
	}
	if err := s.Play.Vote(r.Context(), sid, u.ID, body.TrackID); err != nil {
		writeErr(w, 403, "party", err.Error())
		return
	}
	st, err := s.Play.GetParty(r.Context(), sid)
	if err != nil {
		writeErr(w, 500, "party", err.Error())
		return
	}
	writeJSON(w, 200, st)
}

func (s *Server) partySession(r *http.Request, bodyID *uuid.UUID) (uuid.UUID, error) {
	if bodyID != nil && *bodyID != uuid.Nil {
		return *bodyID, nil
	}
	if q := r.URL.Query().Get("session_id"); q != "" {
		id, err := uuid.Parse(q)
		if err != nil {
			return uuid.Nil, fmt.Errorf("invalid session_id")
		}
		return id, nil
	}
	return uuid.Nil, nil
}

func (s *Server) enqueuePartyExpire(r *http.Request, sid uuid.UUID, exp time.Time) {
	if s.Jobs == nil {
		return
	}
	jid, err := s.Jobs.Enqueue(r.Context(), "party.expire", playback.ExpirePayload{SessionID: sid})
	if err != nil {
		return
	}
	_, _ = s.Pool.Exec(r.Context(), `UPDATE jobs SET run_after=$2 WHERE id=$1`, jid, exp)
}

func (s *Server) maybeListenSkip(r *http.Request, sid uuid.UUID, action string, extra map[string]any) {
	if action != "skip" && action != "next" && action != "previous" {
		return
	}
	q, err := s.Play.Get(r.Context(), sid)
	if err != nil {
		return
	}
	stopAfter, _ := q["stop_after_current"].(bool)
	tid := trackIDFromQueue(q)
	if tid == uuid.Nil {
		return
	}
	pos, _ := q["position_ms"].(int)
	if ms, ok := extra["position_ms"]; ok {
		switch t := ms.(type) {
		case float64:
			pos = int(t)
		case int:
			pos = t
		}
	}
	dur := 0
	_ = s.Pool.QueryRow(r.Context(), `SELECT duration_ms FROM tracks WHERE id=$1`, tid).Scan(&dur)
	src := extraStringMap(extra, "source")
	_ = listen.For(s.Pool).Record(r.Context(), listen.Event{
		UserID:     currentUser(r).ID,
		TrackID:    tid,
		PositionMs: pos,
		DurationMs: dur,
		Source:     src,
		Kind:       "skip",
		StopAfter:  stopAfter && action != "previous",
	})
}

func (s *Server) maybeListenProgress(r *http.Request, sid uuid.UUID, extra map[string]any) {
	q, err := s.Play.Get(r.Context(), sid)
	if err != nil {
		return
	}
	tid := trackIDFromQueue(q)
	if tid == uuid.Nil {
		return
	}
	pos, _ := extraIntMap(extra, "position_ms")
	dur := 0
	_ = s.Pool.QueryRow(r.Context(), `SELECT duration_ms FROM tracks WHERE id=$1`, tid).Scan(&dur)
	_ = listen.For(s.Pool).Record(r.Context(), listen.Event{
		UserID:     currentUser(r).ID,
		TrackID:    tid,
		PositionMs: pos,
		DurationMs: dur,
		Source:     extraStringMap(extra, "source"),
		Kind:       "progress",
	})
}

func trackIDFromQueue(q map[string]any) uuid.UUID {
	switch v := q["current_track_id"].(type) {
	case uuid.UUID:
		return v
	case *uuid.UUID:
		if v != nil {
			return *v
		}
	}
	return uuid.Nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func extraStringMap(extra map[string]any, key string) string {
	if extra == nil {
		return ""
	}
	s, _ := extra[key].(string)
	return s
}

func extraIntMap(extra map[string]any, key string) (int, bool) {
	if extra == nil {
		return 0, false
	}
	v, ok := extra[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	default:
		return 0, false
	}
}
