package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/archive"
)

// adminArchive reports the archive policy, how many songs are archived, how
// many a run would archive now, and a page of archived songs.
func (s *Server) adminArchive(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := archive.Load(ctx, s.Pool)
	pending, _ := archive.Preview(ctx, s.Pool, p.Days)
	var total int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE archived_at IS NOT NULL`).Scan(&total)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT t.id, t.title, coalesce(al.title,''), `+listenArtistSQL+`, t.archived_at,
		       (SELECT max(h.played_at) FROM listen_history h WHERE h.track_id=t.id)
		FROM tracks t LEFT JOIN albums al ON al.id=t.album_id
		WHERE t.archived_at IS NOT NULL
		  AND ($1 = '' OR t.title ILIKE $2 ESCAPE '\' OR coalesce(al.title,'') ILIKE $2 ESCAPE '\'
		       OR EXISTS (SELECT 1 FROM track_artists ta JOIN artists ar ON ar.id=ta.artist_id WHERE ta.track_id=t.id AND ar.name ILIKE $2 ESCAPE '\'))
		ORDER BY t.archived_at DESC, t.title
		LIMIT $3 OFFSET $4`, q, likePattern(q), limit, offset)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	items := scanMaps(rows, "id", "title", "album", "artist", "archived_at", "last_played_at")
	if items == nil {
		items = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{
		"policy": p, "archived": total, "pending": pending, "items": items,
	})
}

func (s *Server) adminPutArchive(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled *bool `json:"enabled"`
		Days    *int  `json:"days"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	p := archive.Load(r.Context(), s.Pool)
	if body.Enabled != nil {
		p.Enabled = *body.Enabled
	}
	if body.Days != nil {
		p.Days = *body.Days
	}
	if err := archive.Save(r.Context(), s.Pool, p); err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	writeJSON(w, 200, archive.Load(r.Context(), s.Pool))
}

func (s *Server) adminRunArchive(w http.ResponseWriter, r *http.Request) {
	if s.Jobs == nil {
		n, err := archive.Run(r.Context(), s.Pool)
		if err != nil {
			writeErr(w, 500, "db", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"archived": n})
		return
	}
	jid, err := s.Jobs.Enqueue(r.Context(), archive.JobType, map[string]any{"manual": true})
	if err != nil {
		s.writeJobErr(w, err)
		return
	}
	writeJSON(w, 202, map[string]any{"job_id": jid})
}

func (s *Server) adminRestoreArchive(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []uuid.UUID `json:"ids"`
		All bool        `json:"all"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	ids := body.IDs
	if body.All {
		ids = nil
	} else if len(ids) == 0 {
		writeErr(w, 400, "invalid", "ids or all required")
		return
	}
	n, err := archive.Restore(r.Context(), s.Pool, ids)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"restored": n})
}
