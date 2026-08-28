package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	MediaStateReady           = "ready"
	MediaStateRestoring       = "restoring"
	MediaStateMissingExternal = "missing_external"

	streamCodeUnavailable         = "media_unavailable"
	streamCodeUnavailableExternal = "media_unavailable_external"
)

// Playability is GET /tracks/{id}/playability. Browser recovery uses this, not stream 409.
type Playability struct {
	State    string     `json:"state"`
	IntentID *uuid.UUID `json:"intent_id,omitempty"`
}

// mediaProbe is the DB-free input to classifyMediaState.
type mediaProbe struct {
	Found       bool
	HasOriginal bool
	Acquisition string
	OpenIntent  bool
	IntentID    *uuid.UUID
}

func managedAcquisition(acq string) bool {
	switch strings.ToLower(strings.TrimSpace(acq)) {
	case "youtube", "scapex":
		return true
	default:
		return false
	}
}

func classifyMediaState(p mediaProbe) Playability {
	if p.HasOriginal {
		return Playability{State: MediaStateReady}
	}
	if managedAcquisition(p.Acquisition) || p.OpenIntent {
		out := Playability{State: MediaStateRestoring}
		if p.IntentID != nil && *p.IntentID != uuid.Nil {
			out.IntentID = p.IntentID
		}
		return out
	}
	return Playability{State: MediaStateMissingExternal}
}

// streamMissingCodes is the defensive stream mapping. Managed youtube/scapex
// stubs are 409 so HTMLAudio does not treat them as a mystery 404. NAS/local
// holes without those acquisitions are 404. Does not start ScapeX.
func streamMissingCodes(acq string) (status int, code, msg string) {
	if managedAcquisition(acq) {
		return http.StatusConflict, streamCodeUnavailable, "media is being restored"
	}
	return http.StatusNotFound, streamCodeUnavailableExternal, "media missing from storage"
}

func (s *Server) getTrackPlayability(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid", "track id required")
		return
	}
	if s.Pool == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable", "database unavailable")
		return
	}
	p, err := s.lookupPlayability(r.Context(), id)
	if err != nil {
		if err == pgx.ErrNoRows || err == errNoRows {
			writeErr(w, http.StatusNotFound, "not_found", "track not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) lookupPlayability(ctx context.Context, id uuid.UUID) (Playability, error) {
	byID, err := s.lookupMediaStates(ctx, []uuid.UUID{id})
	if err != nil {
		return Playability{}, err
	}
	p, ok := byID[id]
	if !ok {
		return Playability{}, errNoRows
	}
	return p, nil
}

func (s *Server) lookupMediaStates(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]Playability, error) {
	out := make(map[uuid.UUID]Playability, len(ids))
	if s == nil || s.Pool == nil || len(ids) == 0 {
		return out, nil
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT t.id, t.acquisition,
		  EXISTS (
		    SELECT 1 FROM track_files tf
		    WHERE tf.track_id=t.id AND tf.quality='original' AND tf.deleted_at IS NULL
		  )
		FROM tracks t WHERE t.id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	probes := map[uuid.UUID]mediaProbe{}
	for rows.Next() {
		var id uuid.UUID
		var acq string
		var hasOrig bool
		if err := rows.Scan(&id, &acq, &hasOrig); err != nil {
			continue
		}
		probes[id] = mediaProbe{Found: true, HasOriginal: hasOrig, Acquisition: acq}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.mergeOpenIntents(ctx, ids, probes)
	for id, p := range probes {
		out[id] = classifyMediaState(p)
	}
	return out, nil
}

func (s *Server) mergeOpenIntents(ctx context.Context, ids []uuid.UUID, probes map[uuid.UUID]mediaProbe) {
	if s.Pool == nil || len(ids) == 0 {
		return
	}
	var present bool
	if err := s.Pool.QueryRow(ctx, `SELECT to_regclass('public.acquisition_intents') IS NOT NULL`).Scan(&present); err != nil || !present {
		return
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT track_id, id FROM acquisition_intents
		WHERE track_id = ANY($1)
		  AND status IN ('open','queued','pending','running','retry')
		ORDER BY created_at DESC`, ids)
	if err != nil {
		return
	}
	defer rows.Close()
	seen := map[uuid.UUID]bool{}
	for rows.Next() {
		var tid, iid uuid.UUID
		if err := rows.Scan(&tid, &iid); err != nil {
			continue
		}
		if seen[tid] {
			continue
		}
		seen[tid] = true
		p := probes[tid]
		p.OpenIntent = true
		if iid != uuid.Nil {
			id := iid
			p.IntentID = &id
		}
		probes[tid] = p
	}
}

func (s *Server) attachQueueMediaState(ctx context.Context, q map[string]any) {
	if q == nil || s == nil || s.Pool == nil {
		return
	}
	ids := queueTrackIDs(q)
	if len(ids) == 0 {
		return
	}
	byID, err := s.lookupMediaStates(ctx, ids)
	if err != nil {
		return
	}
	applyMediaStateToQueue(q, byID)
}

func queueTrackIDs(q map[string]any) []uuid.UUID {
	if q == nil {
		return nil
	}
	seen := map[uuid.UUID]bool{}
	var ids []uuid.UUID
	add := func(id uuid.UUID) {
		if id == uuid.Nil || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	add(uuidFromQueue(q, "current_track_id"))
	switch items := q["items"].(type) {
	case []map[string]any:
		for _, it := range items {
			add(trackIDFromAny(it["track_id"]))
		}
	case []any:
		for _, raw := range items {
			it, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			add(trackIDFromAny(it["track_id"]))
		}
	}
	return ids
}

func applyMediaStateToQueue(q map[string]any, byID map[uuid.UUID]Playability) {
	if q == nil {
		return
	}
	patchItem := func(it map[string]any) {
		if it == nil {
			return
		}
		id := trackIDFromAny(it["track_id"])
		p, ok := byID[id]
		if !ok {
			p = Playability{State: MediaStateMissingExternal}
		}
		it["media_state"] = p.State
		if p.IntentID != nil && *p.IntentID != uuid.Nil {
			it["intent_id"] = *p.IntentID
		} else {
			delete(it, "intent_id")
		}
	}
	switch items := q["items"].(type) {
	case []map[string]any:
		for _, it := range items {
			patchItem(it)
		}
	case []any:
		for _, raw := range items {
			it, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			patchItem(it)
		}
	}
	cur := uuidFromQueue(q, "current_track_id")
	if cur == uuid.Nil {
		delete(q, "current_media_state")
		delete(q, "current_intent_id")
		return
	}
	p, ok := byID[cur]
	if !ok {
		p = Playability{State: MediaStateMissingExternal}
	}
	q["current_media_state"] = p.State
	if p.IntentID != nil && *p.IntentID != uuid.Nil {
		q["current_intent_id"] = *p.IntentID
	} else {
		delete(q, "current_intent_id")
	}
}

func trackIDFromAny(v any) uuid.UUID {
	switch t := v.(type) {
	case uuid.UUID:
		return t
	case *uuid.UUID:
		if t != nil {
			return *t
		}
	case string:
		id, err := uuid.Parse(t)
		if err == nil {
			return id
		}
	}
	return uuid.Nil
}

func (s *Server) respondQueueMedia(w http.ResponseWriter, r *http.Request, sid uuid.UUID, q map[string]any, emit string) {
	s.attachQueueMediaState(r.Context(), q)
	s.respondQueue(w, r, sid, q, emit)
}

func (s *Server) lookupTrackAcquisition(ctx context.Context, id uuid.UUID) (acq string, found bool) {
	if s == nil || s.Pool == nil {
		return "", false
	}
	err := s.Pool.QueryRow(ctx, `SELECT acquisition FROM tracks WHERE id=$1`, id).Scan(&acq)
	return acq, err == nil
}
