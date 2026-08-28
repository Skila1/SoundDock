package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
	"github.com/sounddock/sounddock/internal/library/merge"
)

const tracksMergePerm = "tracks.merge"

type duplicateReviewTrack struct {
	ID         uuid.UUID `json:"id"`
	Title      string    `json:"title"`
	Artist     string    `json:"artist"`
	DurationMS int       `json:"duration_ms"`
	Duration   int       `json:"duration"`
}

type duplicateReviewGroup struct {
	ID      uuid.UUID              `json:"id"`
	GroupID *uuid.UUID             `json:"group_id"`
	Status  string                 `json:"status"`
	Reason  string                 `json:"reason"`
	Tracks  []duplicateReviewTrack `json:"tracks"`
}

// MountDuplicateReview registers duplicate-review routes on the admin router
// (typically /api/v1/admin). W8-http mounts this under requireAdmin and HasPerm.
func (s *Server) MountDuplicateReview(r chi.Router) {
	if r == nil {
		return
	}
	r.Get("/duplicate-review", s.adminDuplicateReviewList)
	r.Post("/duplicate-review/{id}/merge", s.adminDuplicateReviewMerge)
	r.Post("/duplicate-review/{id}/ignore", s.adminDuplicateReviewIgnore)
}

func (s *Server) requireTracksMerge(w http.ResponseWriter, r *http.Request) bool {
	if !auth.HasPerm(currentUser(r), tracksMergePerm) {
		writeErr(w, http.StatusForbidden, "forbidden", "track merge not permitted")
		return false
	}
	s.ensureTracksMergePerm(r.Context())
	return true
}

func (s *Server) ensureTracksMergePerm(ctx context.Context) {
	if s == nil || s.Pool == nil {
		return
	}
	_, _ = s.Pool.Exec(ctx, `
		INSERT INTO permissions (name, description)
		VALUES ('tracks.merge', 'Merge duplicate catalogue tracks into a winner')
		ON CONFLICT DO NOTHING`)
	_, _ = s.Pool.Exec(ctx, `
		INSERT INTO role_permissions (role_id, permission_id)
		SELECT r.id, p.id FROM roles r, permissions p
		WHERE r.name = 'Administrator' AND p.name = 'tracks.merge'
		ON CONFLICT DO NOTHING`)
}

func (s *Server) adminDuplicateReviewList(w http.ResponseWriter, r *http.Request) {
	if s.Pool == nil {
		writeJSON(w, 200, []duplicateReviewGroup{})
		return
	}
	rows, err := s.Pool.Query(r.Context(), `
		SELECT id, group_id, status, reason, track_ids
		FROM duplicate_review_groups
		WHERE status='open'
		ORDER BY created_at DESC
		LIMIT 200`)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	groups := make([]duplicateReviewGroup, 0)
	for rows.Next() {
		var g duplicateReviewGroup
		var groupID *uuid.UUID
		var trackIDs []uuid.UUID
		if err := rows.Scan(&g.ID, &groupID, &g.Status, &g.Reason, &trackIDs); err != nil {
			continue
		}
		g.GroupID = groupID
		g.Tracks = s.duplicateReviewTracks(r.Context(), trackIDs)
		if len(g.Tracks) < 2 {
			continue
		}
		groups = append(groups, g)
	}
	writeJSON(w, 200, groups)
}

func (s *Server) duplicateReviewTracks(ctx context.Context, ids []uuid.UUID) []duplicateReviewTrack {
	if s.Pool == nil || len(ids) == 0 {
		return []duplicateReviewTrack{}
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT t.id, t.title, t.duration_ms,
		  coalesce((
		    SELECT string_agg(ar.name, ', ' ORDER BY ta.position)
		    FROM track_artists ta JOIN artists ar ON ar.id=ta.artist_id
		    WHERE ta.track_id=t.id AND ta.role='primary'
		  ), '') AS artist
		FROM tracks t
		WHERE t.id = ANY($1)
		ORDER BY t.title, t.id`, ids)
	if err != nil {
		return []duplicateReviewTrack{}
	}
	defer rows.Close()
	out := make([]duplicateReviewTrack, 0, len(ids))
	for rows.Next() {
		var t duplicateReviewTrack
		if err := rows.Scan(&t.ID, &t.Title, &t.DurationMS, &t.Artist); err != nil {
			continue
		}
		t.Duration = t.DurationMS
		out = append(out, t)
	}
	return out
}

func (s *Server) adminDuplicateReviewMerge(w http.ResponseWriter, r *http.Request) {
	if !s.requireTracksMerge(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid review id")
		return
	}
	var body struct {
		WinnerID uuid.UUID   `json:"winner_id"`
		LoserIDs []uuid.UUID `json:"loser_ids"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	if body.WinnerID == uuid.Nil || len(body.LoserIDs) == 0 {
		writeErr(w, 400, "invalid", "winner_id and loser_ids required")
		return
	}
	if s.Pool == nil {
		writeErr(w, 503, "db", "database is not available")
		return
	}
	var status string
	var members []uuid.UUID
	err = s.Pool.QueryRow(r.Context(), `
		SELECT status, track_ids FROM duplicate_review_groups WHERE id=$1`, id).Scan(&status, &members)
	if err != nil {
		writeErr(w, 404, "not_found", "review group not found")
		return
	}
	if status != "open" {
		writeErr(w, 409, "conflict", "review group is not open")
		return
	}
	allowed := map[uuid.UUID]struct{}{}
	for _, m := range members {
		allowed[m] = struct{}{}
	}
	if _, ok := allowed[body.WinnerID]; !ok {
		writeErr(w, 400, "invalid", "winner_id is not a member of this group")
		return
	}
	merged := 0
	remaining := map[uuid.UUID]struct{}{}
	for _, m := range members {
		remaining[m] = struct{}{}
	}
	for _, loser := range body.LoserIDs {
		if loser == uuid.Nil || loser == body.WinnerID {
			continue
		}
		if _, ok := allowed[loser]; !ok {
			writeErr(w, 400, "invalid", "loser_id is not a member of this group")
			return
		}
		err := merge.Tracks(r.Context(), s.Pool, body.WinnerID, loser)
		if errors.Is(err, merge.ErrTrackInUse) {
			writeJSON(w, http.StatusConflict, map[string]any{
				"code":     "track_in_use",
				"message":  "cannot merge a track that is currently playing",
				"loser_id": loser,
			})
			return
		}
		if err != nil {
			writeErr(w, 400, "merge", err.Error())
			return
		}
		delete(remaining, loser)
		merged++
	}
	left := make([]uuid.UUID, 0, len(remaining))
	for id := range remaining {
		left = append(left, id)
	}
	newStatus := "open"
	if merged > 0 && len(left) < 2 {
		newStatus = "merged"
	}
	_, _ = s.Pool.Exec(r.Context(), `
		UPDATE duplicate_review_groups SET status=$2, track_ids=$3, updated_at=now() WHERE id=$1`,
		id, newStatus, left)
	if u := currentUser(r); u != nil && s.Audit != nil {
		s.Audit.Event(r.Context(), &u.ID, "tracks.merge", id.String(), r.RemoteAddr, nil)
	}
	writeJSON(w, 200, map[string]any{"ok": true, "merged": merged, "status": newStatus})
}

func (s *Server) adminDuplicateReviewIgnore(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid review id")
		return
	}
	if s.Pool == nil {
		writeErr(w, 503, "db", "database is not available")
		return
	}
	tag, err := s.Pool.Exec(r.Context(), `
		UPDATE duplicate_review_groups SET status='ignored', updated_at=now()
		WHERE id=$1 AND status='open'`, id)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, 404, "not_found", "open review group not found")
		return
	}
	if u := currentUser(r); u != nil && s.Audit != nil {
		s.Audit.Event(r.Context(), &u.ID, "duplicate_review.ignore", id.String(), r.RemoteAddr, nil)
	}
	writeJSON(w, 200, map[string]any{"ok": true, "status": "ignored"})
}
