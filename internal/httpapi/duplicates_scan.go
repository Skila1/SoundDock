package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/sounddock/sounddock/internal/dupes"
	"github.com/sounddock/sounddock/internal/library/merge"
)

// Title-based duplicate scan. Results live in a JSON file in the data dir,
// replaced by each scan; merge and delete act on real tracks.

func (s *Server) dupeStore() *dupes.Store { return dupes.NewStore(s.Cfg.DataDir) }

func (s *Server) adminDuplicateScanResult(w http.ResponseWriter, r *http.Request) {
	res, err := s.dupeStore().Load(r.Context(), s.Pool)
	if err != nil {
		writeErr(w, 500, "dupes", err.Error())
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) adminDuplicateScan(w http.ResponseWriter, r *http.Request) {
	if s.Pool == nil {
		writeErr(w, 503, "db", "database is not available")
		return
	}
	var body struct {
		LibraryID *uuid.UUID `json:"library_id"`
	}
	_ = decodeJSON(r, &body)
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	res, err := dupes.Scan(ctx, s.Pool, body.LibraryID)
	if err != nil {
		writeErr(w, 500, "dupes", err.Error())
		return
	}
	st := s.dupeStore()
	if err := st.Save(res); err != nil {
		writeErr(w, 500, "dupes", err.Error())
		return
	}
	if u := currentUser(r); u != nil && s.Audit != nil {
		s.Audit.Event(r.Context(), &u.ID, "duplicates.scan", "", r.RemoteAddr, nil)
	}
	res, _ = st.Load(r.Context(), s.Pool)
	writeJSON(w, 200, res)
}

func (s *Server) adminDuplicateMerge(w http.ResponseWriter, r *http.Request) {
	if !s.requireTracksMerge(w, r) {
		return
	}
	var body struct {
		WinnerID uuid.UUID   `json:"winner_id"`
		LoserIDs []uuid.UUID `json:"loser_ids"`
	}
	if err := decodeJSON(r, &body); err != nil || body.WinnerID == uuid.Nil || len(body.LoserIDs) == 0 {
		writeErr(w, 400, "invalid", "winner_id and loser_ids required")
		return
	}
	merged := 0
	for _, loser := range body.LoserIDs {
		if loser == uuid.Nil || loser == body.WinnerID {
			continue
		}
		err := merge.Tracks(r.Context(), s.Pool, body.WinnerID, loser)
		if errors.Is(err, merge.ErrTrackInUse) {
			writeJSON(w, http.StatusConflict, map[string]any{"code": "track_in_use", "message": "cannot merge a song that is playing right now", "loser_id": loser, "merged": merged})
			return
		}
		if err != nil {
			writeErr(w, 400, "merge", err.Error())
			return
		}
		merged++
	}
	if u := currentUser(r); u != nil && s.Audit != nil {
		s.Audit.Event(r.Context(), &u.ID, "tracks.merge", body.WinnerID.String(), r.RemoteAddr, nil)
	}
	writeJSON(w, 200, map[string]any{"ok": true, "merged": merged})
}

func (s *Server) adminDuplicateIgnore(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TrackIDs []uuid.UUID `json:"track_ids"`
	}
	if err := decodeJSON(r, &body); err != nil || len(body.TrackIDs) < 2 {
		writeErr(w, 400, "invalid", "track_ids needs at least two songs")
		return
	}
	if err := s.dupeStore().Ignore(body.TrackIDs); err != nil {
		writeErr(w, 500, "dupes", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
