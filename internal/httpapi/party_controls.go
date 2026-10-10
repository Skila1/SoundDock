package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/playback"
)

// partyFor resolves the party a request acts on: an explicit session_id, the
// party the caller has joined, or the caller's own session (as host).
func (s *Server) partyFor(r *http.Request, bodyID *uuid.UUID, deviceID string) (uuid.UUID, error) {
	sid, err := s.partySession(r, bodyID)
	if err != nil || sid != uuid.Nil {
		return sid, err
	}
	u := currentUser(r)
	if found, ok, ferr := s.Play.FindPartyForUser(r.Context(), u.ID); ferr == nil && ok {
		return found, nil
	}
	return s.Play.WebSession(r.Context(), u.ID, firstNonEmpty(deviceID, requestDeviceID(r, nil)))
}

func (s *Server) writePartyState(w http.ResponseWriter, r *http.Request, sid uuid.UUID) {
	st, err := s.Play.GetParty(r.Context(), sid)
	if err != nil {
		writeErr(w, 500, "party", err.Error())
		return
	}
	writeJSON(w, 200, st)
}

func partyErr(w http.ResponseWriter, err error) {
	if errors.Is(err, playback.ErrPartyForbidden) {
		writeErr(w, 403, "party_forbidden", err.Error())
		return
	}
	writeErr(w, 403, "party", err.Error())
}

func (s *Server) putPartyPermissions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID   *uuid.UUID                `json:"session_id"`
		DeviceID    string                    `json:"device_id"`
		Permissions playback.PartyPermissions `json:"permissions"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	sid, err := s.partyFor(r, body.SessionID, body.DeviceID)
	if err != nil {
		writeErr(w, 400, "party", err.Error())
		return
	}
	if err := s.Play.SetPartyPermissions(r.Context(), sid, currentUser(r).ID, body.Permissions); err != nil {
		partyErr(w, err)
		return
	}
	s.writePartyState(w, r, sid)
}

func (s *Server) setPartyMember(w http.ResponseWriter, r *http.Request) {
	member, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeErr(w, 400, "invalid", "user id")
		return
	}
	var body struct {
		SessionID *uuid.UUID `json:"session_id"`
		DeviceID  string     `json:"device_id"`
		Role      string     `json:"role"`
	}
	_ = decodeJSON(r, &body)
	sid, err := s.partyFor(r, body.SessionID, body.DeviceID)
	if err != nil {
		writeErr(w, 400, "party", err.Error())
		return
	}
	if r.Method == http.MethodDelete {
		err = s.Play.RemovePartyMember(r.Context(), sid, currentUser(r).ID, member)
	} else {
		err = s.Play.SetPartyMemberRole(r.Context(), sid, currentUser(r).ID, member, body.Role)
	}
	if err != nil {
		partyErr(w, err)
		return
	}
	s.writePartyState(w, r, sid)
}

// partyControl lets party members skip, go back, pause or resume the host's
// playback when the host allows it.
func (s *Server) partyControl(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID *uuid.UUID `json:"session_id"`
		DeviceID  string     `json:"device_id"`
		Action    string     `json:"action"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	perm := map[string]string{"skip": "skip", "previous": "skip", "pause": "pause", "resume": "pause"}[body.Action]
	if perm == "" {
		writeErr(w, 400, "invalid", "action must be skip, previous, pause or resume")
		return
	}
	sid, err := s.partyFor(r, body.SessionID, body.DeviceID)
	if err != nil {
		writeErr(w, 400, "party", err.Error())
		return
	}
	if err := s.Play.PartyAllowed(r.Context(), sid, currentUser(r).ID, perm); err != nil {
		partyErr(w, err)
		return
	}
	if err := s.Play.Control(s.withQueueRequester(r), sid, body.Action, nil); err != nil {
		writeErr(w, 400, "queue", err.Error())
		return
	}
	s.writePartyState(w, r, sid)
}

// partyQueue adds songs to the host's queue for members allowed to.
func (s *Server) partyQueue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID *uuid.UUID       `json:"session_id"`
		DeviceID  string           `json:"device_id"`
		TrackIDs  []string         `json:"track_ids"`
		Tracks    []queueTrackHint `json:"tracks"`
		Next      bool             `json:"next"`
	}
	if err := decodeJSON(r, &body); err != nil || len(body.TrackIDs) == 0 {
		writeErr(w, 400, "invalid", "track_ids required")
		return
	}
	sid, err := s.partyFor(r, body.SessionID, body.DeviceID)
	if err != nil {
		writeErr(w, 400, "party", err.Error())
		return
	}
	if err := s.Play.PartyAllowed(r.Context(), sid, currentUser(r).ID, "add"); err != nil {
		partyErr(w, err)
		return
	}
	ctx, refs := s.acquirePlayCtx(r, body.TrackIDs, body.Tracks)
	ids, err := s.resolvePlayTracks(ctx, refs)
	if err != nil {
		writeErr(w, 502, "resolve", err.Error())
		return
	}
	if err := s.Play.Add(s.withQueueRequester(r), sid, ids, body.Next); err != nil {
		writeErr(w, 400, "queue", err.Error())
		return
	}
	s.writePartyState(w, r, sid)
}
