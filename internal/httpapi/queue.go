package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
	"github.com/sounddock/sounddock/internal/listen"
	"github.com/sounddock/sounddock/internal/playback"
	"github.com/sounddock/sounddock/internal/scapex"
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

// attachedPlaySession returns the bound guild session when the caller is a
// verified Discord user currently in the bot's bound voice channel. Otherwise
// it returns their personal web_device session. Unlinked users never attach.
// target=discord is not a second-queue selector.
func (s *Server) attachedPlaySession(r *http.Request, extra map[string]any, deviceID string) (uuid.UUID, error) {
	if attached, ok := s.attachedBoundSession(r); ok {
		return attached, nil
	}
	u := currentUser(r)
	return s.Play.WebSession(r.Context(), u.ID, firstNonEmpty(deviceID, requestDeviceID(r, extra)))
}

func (s *Server) playSession(r *http.Request, extra map[string]any, bodyTarget, deviceID string) (uuid.UUID, error) {
	sid, err := s.attachedPlaySession(r, extra, deviceID)
	if err != nil {
		return uuid.Nil, err
	}
	if requestPlaybackTarget(r, extra, bodyTarget) == "discord" {
		if err := s.bindSessionToDiscord(r, sid, extra); err != nil {
			return uuid.Nil, err
		}
	}
	return sid, nil
}

func (s *Server) bindSessionToDiscord(r *http.Request, sid uuid.UUID, extra map[string]any) error {
	g, c, ok := s.findUserVoice(r)
	if !ok {
		return errNotInVoice
	}
	expected, _ := extraInt64Map(extra, "expected_binding_revision")
	rendererID := extraStringMap(extra, "renderer_id")
	gen, _ := extraInt64Map(extra, "renderer_generation")
	if gen == 0 {
		gen, _ = extraInt64Map(extra, "generation")
	}
	_, err := s.ensureDiscordJoin(r, g, c, sid, expected, rendererID, gen)
	return err
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
	if writePlaybackConflict(w, err) {
		return true
	}
	writeErr(w, 500, "queue", err.Error())
	return true
}

func writePlaybackConflict(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, playback.ErrBindConflict):
		writeErr(w, 409, "bind_conflict", err.Error())
		return true
	case errors.Is(err, playback.ErrLeaseConflict):
		writeErr(w, 409, "lease_conflict", err.Error())
		return true
	case errors.Is(err, playback.ErrCommandConflict):
		writeErr(w, 409, "command_conflict", err.Error())
		return true
	case errors.Is(err, playback.ErrUndoStale):
		writeErr(w, 409, "undo_stale", err.Error())
		return true
	default:
		return false
	}
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
	return s.attachedPlaySession(r, extra, "")
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
	s.respondQueueMedia(w, r, sid, q, "")
}

type queueTrackHint struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	DurationMS int    `json:"duration_ms"`
}

func collectTrackHints(ids []string, hints []queueTrackHint) ([]string, map[string]scapex.TrackHint) {
	out := map[string]scapex.TrackHint{}
	for _, h := range hints {
		id := strings.TrimSpace(h.ID)
		if id == "" {
			continue
		}
		hint := scapex.TrackHint{Title: h.Title, Artist: h.Artist, DurationMS: h.DurationMS}
		out[id] = hint
		if ref := scapex.CanonicalSourceRef(id); ref != "" {
			out[ref] = hint
		}
		found := false
		for _, existing := range ids {
			if existing == id {
				found = true
				break
			}
		}
		if !found {
			ids = append(ids, id)
		}
	}
	return ids, out
}

func (s *Server) acquirePlayCtx(r *http.Request, ids []string, hints []queueTrackHint) (context.Context, []string) {
	ids, hintMap := collectTrackHints(ids, hints)
	return withTrackHints(s.withAcquirePolicy(r.Context()), hintMap), ids
}

func (s *Server) putQueue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TrackIDs                []string         `json:"track_ids"`
		Tracks                  []queueTrackHint `json:"tracks"`
		Start                   int              `json:"start"`
		DeviceID                string           `json:"device_id"`
		Target                  string           `json:"target"`
		ExpectedBindingRevision int64            `json:"expected_binding_revision"`
		RendererID              string           `json:"renderer_id"`
		RendererGeneration      int64            `json:"renderer_generation"`
		CommandID               string           `json:"command_id"`
	}
	_ = decodeJSON(r, &body)
	sid, err := s.playSession(r, bindExtra(nil, body.ExpectedBindingRevision, body.RendererID, body.RendererGeneration), body.Target, body.DeviceID)
	if s.writePlaySessionErr(w, err) {
		return
	}
	ctx, refs := s.acquirePlayCtx(r, body.TrackIDs, body.Tracks)
	ids, err := s.resolvePlayTracks(ctx, refs)
	if s.writeAcquireErr(w, err) {
		return
	}
	cmdID := strings.TrimSpace(body.CommandID)
	if cmdID == "" {
		if err := s.Play.Replace(s.withQueueRequester(r), sid, ids, body.Start); err != nil {
			writeErr(w, 400, "queue", err.Error())
			return
		}
	} else {
		extra := map[string]any{"track_ids": ids, "start": body.Start, "command_id": cmdID}
		if err := s.Play.Control(s.withQueueRequester(r), sid, "replace", extra); err != nil {
			if errors.Is(err, playback.ErrCommandConflict) {
				writeErr(w, 409, "command_conflict", err.Error())
				return
			}
			writeErr(w, 400, "queue", err.Error())
			return
		}
	}
	if len(ids) > 0 && s.Hooks != nil {
		start := body.Start
		if start < 0 || start >= len(ids) {
			start = 0
		}
		s.Hooks.Emit(r.Context(), "playback.started", map[string]any{"track_id": ids[start]})
	}
	q, _ := s.Play.Get(r.Context(), sid)
	s.respondQueueMedia(w, r, sid, q, "state")
}

func (s *Server) writeAcquireErr(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), "library write not granted") || strings.Contains(err.Error(), "library stream not granted") {
		writeErr(w, 403, "library_grant", err.Error())
		return true
	}
	writeErr(w, 502, "scapex", err.Error())
	return true
}

func (s *Server) queueAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TrackIDs                []string         `json:"track_ids"`
		Tracks                  []queueTrackHint `json:"tracks"`
		Next                    bool             `json:"next"`
		DeviceID                string           `json:"device_id"`
		Target                  string           `json:"target"`
		ExpectedBindingRevision int64            `json:"expected_binding_revision"`
		RendererID              string           `json:"renderer_id"`
		RendererGeneration      int64            `json:"renderer_generation"`
		CommandID               string           `json:"command_id"`
	}
	_ = decodeJSON(r, &body)
	sid, err := s.playSession(r, bindExtra(nil, body.ExpectedBindingRevision, body.RendererID, body.RendererGeneration), body.Target, body.DeviceID)
	if s.writePlaySessionErr(w, err) {
		return
	}
	ctx, refs := s.acquirePlayCtx(r, body.TrackIDs, body.Tracks)
	ids, err := s.resolvePlayTracks(ctx, refs)
	if s.writeAcquireErr(w, err) {
		return
	}
	cmdID := strings.TrimSpace(body.CommandID)
	if cmdID == "" {
		if err := s.Play.Add(s.withQueueRequester(r), sid, ids, body.Next); err != nil {
			writeErr(w, 400, "queue", err.Error())
			return
		}
	} else {
		extra := map[string]any{"track_ids": ids, "next": body.Next, "command_id": cmdID}
		if err := s.Play.Control(s.withQueueRequester(r), sid, "add", extra); err != nil {
			if errors.Is(err, playback.ErrCommandConflict) {
				writeErr(w, 409, "command_conflict", err.Error())
				return
			}
			writeErr(w, 400, "queue", err.Error())
			return
		}
	}
	q, _ := s.Play.Get(r.Context(), sid)
	s.respondQueueMedia(w, r, sid, q, "state")
}

func (s *Server) queueControl(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action                  string         `json:"action"`
		Extra                   map[string]any `json:"extra"`
		DeviceID                string         `json:"device_id"`
		Target                  string         `json:"target"`
		CommandID               string         `json:"command_id"`
		ExpectedBindingRevision int64          `json:"expected_binding_revision"`
		RendererID              string         `json:"renderer_id"`
		RendererGeneration      int64          `json:"renderer_generation"`
	}
	_ = decodeJSON(r, &body)
	if body.Extra == nil {
		body.Extra = map[string]any{}
	}
	if strings.TrimSpace(extraStringMap(body.Extra, "command_id")) == "" && strings.TrimSpace(body.CommandID) != "" {
		body.Extra["command_id"] = body.CommandID
	}
	body.Extra = bindExtra(body.Extra, body.ExpectedBindingRevision, firstNonEmpty(body.RendererID, extraStringMap(body.Extra, "renderer_id")), body.RendererGeneration)
	if body.Action == "switch_renderer" {
		if controlOutputPref(body.Extra) == "" {
			body.Extra["output_pref"] = playback.OutputBrowser
		}
		body.Action = "output_pref"
	}
	sid, err := s.playSession(r, body.Extra, body.Target, body.DeviceID)
	if s.writePlaySessionErr(w, err) {
		return
	}
	if body.Action == "output_pref" {
		pref := controlOutputPref(body.Extra)
		if pref == playback.OutputBrowser {
			rid := firstNonEmpty(extraStringMap(body.Extra, "renderer_id"), body.DeviceID, requestDeviceID(r, body.Extra), "http-browser")
			gen, _ := extraInt64Map(body.Extra, "renderer_generation")
			if gen == 0 {
				gen, _ = extraInt64Map(body.Extra, "generation")
			}
			if err := s.Play.SwitchRendererToBrowser(r.Context(), sid, rid, gen); err != nil {
				if writePlaybackConflict(w, err) {
					return
				}
				writeErr(w, 400, "control", err.Error())
				return
			}
		} else if pref == playback.OutputDiscord && requestPlaybackTarget(r, body.Extra, body.Target) != "discord" {
			if err := s.bindSessionToDiscord(r, sid, body.Extra); err != nil {
				if s.writePlaySessionErr(w, err) {
					return
				}
			}
		}
	}
	switch body.Action {
	case "skip", "next", "previous":
		s.maybeListenSkip(r, sid, body.Extra)
	}
	if err := s.Play.Control(s.withQueueRequester(r), sid, body.Action, body.Extra); err != nil {
		if writePlaybackConflict(w, err) {
			return
		}
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
	attachUndoToQueue(q, body.Extra)
	s.respondQueueMedia(w, r, sid, q, "state+playhead")
}

func (s *Server) queueRendererAcquire(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RendererID         string `json:"renderer_id"`
		ExpectedGeneration int64  `json:"expected_generation"`
		DeviceID           string `json:"device_id"`
	}
	_ = decodeJSON(r, &body)
	sid, err := s.attachedPlaySession(r, nil, body.DeviceID)
	if s.writePlaySessionErr(w, err) {
		return
	}
	rid := firstNonEmpty(body.RendererID, requestDeviceID(r, nil), "http-browser")
	gen, err := s.Play.AcquireBrowserRenderer(r.Context(), sid, rid, body.ExpectedGeneration)
	if err != nil {
		if writePlaybackConflict(w, err) {
			return
		}
		writeErr(w, 500, "renderer", err.Error())
		return
	}
	q, _ := s.Play.Get(r.Context(), sid)
	if q == nil {
		q = map[string]any{}
	}
	q["generation"] = gen
	s.respondQueueMedia(w, r, sid, q, "state")
}

func (s *Server) postListen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TrackID            uuid.UUID `json:"track_id"`
		PositionMs         int       `json:"position_ms"`
		DurationMs         int       `json:"duration_ms"`
		Source             string    `json:"source"`
		Event              string    `json:"event"`
		DeviceID           string    `json:"device_id"`
		ClientID           string    `json:"client_id"`
		PlaybackInstanceID uuid.UUID `json:"playback_instance_id"`
		PlayheadSequence   int64     `json:"playhead_sequence"`
		Status             string    `json:"status"`
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
	clientID := firstNonEmpty(body.ClientID, requestClientID(r, nil))
	deviceID := firstNonEmpty(body.DeviceID, requestDeviceID(r, nil))
	instanceID := body.PlaybackInstanceID
	seq := body.PlayheadSequence
	rendererKind, rendererID, status := "", "", body.Status
	rate := 1.0
	sid, serr := s.Play.WebSession(r.Context(), u.ID, firstNonEmpty(deviceID, requestDeviceID(r, nil)))
	if serr == nil {
		if q, err := s.Play.Get(r.Context(), sid); err == nil {
			if instanceID == uuid.Nil {
				instanceID = uuidFromQueue(q, "playback_instance_id")
			}
			if seq == 0 {
				seq, _ = extraInt64Map(q, "playhead_sequence")
			}
			rendererKind = extraStringMap(q, "renderer_kind")
			rendererID = stringFromQueue(q, "renderer_id")
			if status == "" {
				status = extraStringMap(q, "status")
			}
			if rateVal := floatFromQueue(q, "playback_rate"); rateVal > 0 {
				rate = rateVal
			}
		}
	}
	err := s.scrobbleSvc().HandleListen(r.Context(), u.ID, scrobble.Event{
		TrackID: body.TrackID, PositionMS: body.PositionMs, DurationMS: body.DurationMs,
		Source: body.Source, Kind: kind,
		PlaybackInstanceID: instanceID, PlayheadSequence: seq,
		ClientID: clientID, DeviceID: deviceID, Status: status,
		PlaybackRate: rate, RendererKind: rendererKind, RendererID: rendererID,
	})
	if err != nil {
		writeErr(w, 500, "listen", err.Error())
		return
	}
	if serr == nil {
		_ = s.Play.SetPosition(r.Context(), sid, body.PositionMs)
		s.touchPresenceFromRequest(r, sid)
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

func (s *Server) maybeListenSkip(r *http.Request, sid uuid.UUID, extra map[string]any) {
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
	seq, _ := extraInt64Map(q, "playhead_sequence")
	listenTrue := true
	_ = s.scrobbleSvc().HandleListen(r.Context(), currentUser(r).ID, scrobble.Event{
		TrackID: tid, PositionMS: pos, DurationMS: dur,
		Source: extraStringMap(extra, "source"), Kind: "skip",
		PlaybackInstanceID: uuidFromQueue(q, "playback_instance_id"),
		PlayheadSequence:   seq,
		ClientID:           extraStringMap(extra, "client_id"),
		DeviceID:           extraStringMap(extra, "device_id"),
		Status:             extraStringMap(q, "status"),
		PlaybackRate:       floatFromQueue(q, "playback_rate"),
		RendererKind:       extraStringMap(q, "renderer_kind"),
		RendererID:         stringFromQueue(q, "renderer_id"),
		AudioListener:      &listenTrue,
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
	seq, _ := extraInt64Map(q, "playhead_sequence")
	_ = s.scrobbleSvc().HandleListen(r.Context(), currentUser(r).ID, scrobble.Event{
		TrackID: tid, PositionMS: pos, DurationMS: dur,
		Source: extraStringMap(extra, "source"), Kind: "progress",
		PlaybackInstanceID: uuidFromQueue(q, "playback_instance_id"),
		PlayheadSequence:   seq,
		ClientID:           extraStringMap(extra, "client_id"),
		DeviceID:           extraStringMap(extra, "device_id"),
		Status:             extraStringMap(q, "status"),
		PlaybackRate:       floatFromQueue(q, "playback_rate"),
		RendererKind:       extraStringMap(q, "renderer_kind"),
		RendererID:         stringFromQueue(q, "renderer_id"),
	})
}

func trackIDFromQueue(q map[string]any) uuid.UUID {
	return uuidFromQueue(q, "current_track_id")
}

func uuidFromQueue(q map[string]any, key string) uuid.UUID {
	if q == nil {
		return uuid.Nil
	}
	switch v := q[key].(type) {
	case uuid.UUID:
		return v
	case *uuid.UUID:
		if v != nil {
			return *v
		}
	case string:
		id, err := uuid.Parse(v)
		if err == nil {
			return id
		}
	}
	return uuid.Nil
}

func stringFromQueue(q map[string]any, key string) string {
	if q == nil {
		return ""
	}
	switch v := q[key].(type) {
	case string:
		return v
	case *string:
		if v != nil {
			return *v
		}
	}
	return ""
}

func floatFromQueue(q map[string]any, key string) float64 {
	if q == nil {
		return 0
	}
	switch v := q[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case int32:
		return float64(v)
	}
	return 0
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func (s *Server) withQueueRequester(r *http.Request) context.Context {
	ctx := r.Context()
	u := currentUser(r)
	if u == nil {
		return playback.WithOrigin(ctx, playback.OriginUser)
	}
	ctx = playback.WithRequester(ctx, u.ID, s.discordUserID(r))
	return playback.WithOrigin(ctx, playback.OriginUser)
}

func attachUndoToQueue(q, extra map[string]any) {
	if q == nil || extra == nil {
		return
	}
	if u, ok := extra["undo"]; ok && u != nil {
		q["undo"] = u
	}
	if g, ok := extra["undo_generation"]; ok && g != nil {
		q["undo_generation"] = g
	}
}

func extraStringMap(extra map[string]any, key string) string {
	if extra == nil {
		return ""
	}
	s, _ := extra[key].(string)
	return s
}

func extraIntMap(extra map[string]any, key string) (int, bool) {
	n, ok := extraInt64Map(extra, key)
	return int(n), ok
}

func extraInt64Map(extra map[string]any, key string) (int64, bool) {
	if extra == nil {
		return 0, false
	}
	v, ok := extra[key]
	if !ok || v == nil {
		return 0, false
	}
	return anyInt64(v)
}

func anyInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case int64:
		return t, true
	case int:
		return int64(t), true
	case float64:
		return int64(t), true
	case float32:
		return int64(t), true
	default:
		return 0, false
	}
}

func bindExtra(extra map[string]any, expected int64, rendererID string, gen int64) map[string]any {
	if extra == nil {
		extra = map[string]any{}
	}
	if expected != 0 {
		extra["expected_binding_revision"] = expected
	}
	if rendererID != "" {
		extra["renderer_id"] = rendererID
	}
	if gen != 0 {
		extra["renderer_generation"] = gen
	}
	return extra
}

func controlOutputPref(extra map[string]any) string {
	pref := strings.ToLower(strings.TrimSpace(extraStringMap(extra, "output_pref")))
	if pref == "" {
		pref = strings.ToLower(strings.TrimSpace(extraStringMap(extra, "pref")))
	}
	if pref == "" {
		pref = strings.ToLower(strings.TrimSpace(extraStringMap(extra, "renderer_kind")))
	}
	return pref
}

var errAcquireNoDB = errString("database unavailable")

func (s *Server) jobsAcquireReady() bool {
	return s != nil && s.Jobs != nil && s.Jobs.Started()
}

func (s *Server) withAcquirePolicy(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	pol := s.loadAcquisitionPolicy(ctx)
	in, _ := ctx.Value(acquireKey{}).(scapex.IntentInput)
	if in.MediaPolicyID == "" && pol.MediaPolicyID != "" {
		in.MediaPolicyID = pol.MediaPolicyID
	}
	return WithAcquisitionIntent(ctx, in)
}

// resolvePlayTracks never waits on yt-dlp. Library UUIDs still go through
// resolveQueueTracks when that path is non-blocking. YouTube-shaped refs use
// W6-scapex enqueueYouTubeRefs when jobs are running; otherwise a restoring
// placeholder is inserted and 200 is returned instead of 502 after a long fetch.
func (s *Server) resolvePlayTracks(ctx context.Context, refs []string) ([]uuid.UUID, error) {
	tracks, youtube := scapex.ParseTrackRefs(refs)
	if err := s.requireStreamGrantOnTracks(ctx, tracks); err != nil {
		return nil, err
	}
	var out []uuid.UUID
	if len(tracks) > 0 {
		if s.ScapeX != nil && !s.jobsAcquireReady() {
			out = append(out, tracks...)
		} else {
			ids, err := s.resolveQueueTracks(ctx, uuidStrings(tracks))
			if err != nil {
				return nil, err
			}
			out = append(out, ids...)
		}
	}
	if len(youtube) == 0 {
		return out, nil
	}
	if s.jobsAcquireReady() {
		ids, err := s.enqueueYouTubeRefs(ctx, youtube)
		if err != nil {
			return nil, err
		}
		return append(out, ids...), nil
	}
	stubs, err := s.ensureRestoringPlaceholders(ctx, youtube)
	if err != nil {
		return nil, err
	}
	s.enqueueAcquireRefs(ctx, youtube)
	return append(out, stubs...), nil
}

func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

func (s *Server) enqueueAcquireRefs(ctx context.Context, refs []string) {
	if s == nil || s.Jobs == nil || len(refs) == 0 {
		return
	}
	_, _ = s.Jobs.Enqueue(ctx, "scapex.fetch", map[string]any{"urls": refs})
}

func (s *Server) ensureRestoringPlaceholders(ctx context.Context, youtube []string) ([]uuid.UUID, error) {
	if len(youtube) == 0 {
		return nil, nil
	}
	if s == nil || s.Pool == nil {
		return nil, errAcquireNoDB
	}
	lib, err := s.writableAcquireLibrary(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(youtube))
	for _, raw := range youtube {
		id, err := s.ensureYouTubePlaceholder(ctx, lib, raw)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func (s *Server) requireStreamGrantOnTracks(ctx context.Context, ids []uuid.UUID) error {
	if s == nil || s.Pool == nil || len(ids) == 0 {
		return nil
	}
	u, _ := ctx.Value(userKey).(*auth.User)
	if u == nil || u.IsAdmin {
		return nil
	}
	rows, err := s.Pool.Query(ctx, `SELECT id, library_id FROM tracks WHERE id = ANY($1)`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, libID uuid.UUID
		if err := rows.Scan(&id, &libID); err != nil {
			continue
		}
		if !s.userHasLibraryAction(ctx, u, libID, "stream") {
			return errString("library stream not granted")
		}
	}
	return rows.Err()
}

func (s *Server) writableAcquireLibrary(ctx context.Context) (uuid.UUID, error) {
	u, _ := ctx.Value(userKey).(*auth.User)
	var allowed []uuid.UUID
	if u != nil && !u.IsAdmin {
		allowed = s.libraryIDsFor(ctx, u, "write")
		if len(allowed) == 0 {
			return uuid.Nil, errString("library write not granted")
		}
	}
	q := `
		SELECT l.id
		FROM libraries l
		JOIN storage_providers sp ON sp.id = l.storage_provider_id
		WHERE l.read_only = false AND sp.type IN ('managed', 'local')`
	args := []any{}
	if len(allowed) > 0 {
		q += ` AND l.id = ANY($1)`
		args = append(args, allowed)
	}
	q += `
		ORDER BY CASE WHEN l.is_default THEN 0 ELSE 1 END,
		         CASE WHEN lower(l.name) = 'music' THEN 0 ELSE 1 END,
		         l.created_at
		LIMIT 1`
	var id uuid.UUID
	err := s.Pool.QueryRow(ctx, q, args...).Scan(&id)
	if err != nil {
		if u != nil && !u.IsAdmin {
			return uuid.Nil, errString("library write not granted")
		}
		return uuid.Nil, errString("no writable library for acquisition")
	}
	return id, nil
}

func (s *Server) ensureYouTubePlaceholder(ctx context.Context, lib uuid.UUID, raw string) (uuid.UUID, error) {
	vid := youtubeVideoID(raw)
	watch := raw
	if u := youtubeWatchURL(raw); u != "" {
		watch = u
	}
	var existing uuid.UUID
	err := s.Pool.QueryRow(ctx, `
		SELECT t.id FROM tracks t
		WHERE t.acquisition IN ('youtube', 'scapex')
		  AND t.acquisition_ref IN ($1, $2, $3)
		ORDER BY EXISTS (
		    SELECT 1 FROM track_files tf
		    WHERE tf.track_id=t.id AND tf.quality='original' AND tf.deleted_at IS NULL
		  ) DESC, t.created_at DESC
		LIMIT 1`, vid, watch, strings.TrimSpace(raw)).Scan(&existing)
	ref := vid
	if ref == "" {
		ref = strings.TrimSpace(raw)
	}
	hint := hintForRef(ctx, ref)
	if err == nil && existing != uuid.Nil {
		scapex.ApplyStubHint(ctx, s.Pool, existing, hint)
		return existing, nil
	}
	return scapex.EnsureStubTrack(ctx, s.Pool, lib, ref, hint)
}

func youtubeWatchURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if vid := youtubeVideoID(raw); len(vid) == 11 {
		return "https://www.youtube.com/watch?v=" + vid
	}
	return raw
}

func youtubeVideoID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if isYouTubeVideoID(raw) {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if v := strings.TrimSpace(u.Query().Get("v")); isYouTubeVideoID(v) {
		return v
	}
	host := strings.ToLower(u.Host)
	if strings.Contains(host, "youtu.be") {
		id := strings.Trim(u.Path, "/")
		if i := strings.IndexByte(id, '/'); i >= 0 {
			id = id[:i]
		}
		if isYouTubeVideoID(id) {
			return id
		}
	}
	if strings.Contains(host, "youtube.com") && strings.Contains(u.Path, "/shorts/") {
		id := strings.Trim(strings.TrimPrefix(u.Path, "/shorts/"), "/")
		if isYouTubeVideoID(id) {
			return id
		}
	}
	return ""
}

func isYouTubeVideoID(s string) bool {
	if len(s) != 11 {
		return false
	}
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}
