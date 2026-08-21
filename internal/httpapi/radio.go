package httpapi

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/radio"
)

func (s *Server) getRadio(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		writeErr(w, 400, "invalid", "kind required")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	var seed uuid.UUID
	if raw := r.URL.Query().Get("seed_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeErr(w, 400, "invalid", "seed_id must be a uuid")
			return
		}
		seed = id
	}
	decade := radio.ParseDecade(r.URL.Query().Get("decade"))
	req := radio.Request{
		Kind:   kind,
		SeedID: seed,
		Genre:  r.URL.Query().Get("genre"),
		Decade: decade,
		Limit:  limit,
		UserID: u.ID,
		Libs:   s.libraryIDs(r.Context(), u),
	}
	res, err := radio.New(s.Pool).Select(r.Context(), req)
	if err != nil {
		switch err {
		case radio.ErrUnknownKind:
			writeErr(w, 400, "invalid", "kind must be library, artist, album, track, genre, decade, or quick_mix")
		case radio.ErrSeed:
			writeErr(w, 400, "invalid", "seed_id required")
		case radio.ErrDecade:
			writeErr(w, 400, "invalid", "decade required")
		default:
			writeErr(w, 500, "db", err.Error())
		}
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) radioSeeds(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	out, err := radio.New(s.Pool).Seeds(r.Context(), s.libraryIDs(r.Context(), u))
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) radioRefresh(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var body radio.RefreshPayload
	_ = decodeJSON(r, &body)
	if body.Kind == "" {
		body.Kind = r.URL.Query().Get("kind")
	}
	if !radio.ValidKind(body.Kind) {
		writeErr(w, 400, "invalid", "kind required")
		return
	}
	body.UserID = u.ID
	jid, err := s.Jobs.Enqueue(r.Context(), "radio.refresh", body)
	if err != nil {
		writeErr(w, 500, "job", err.Error())
		return
	}
	writeJSON(w, 202, map[string]any{"job_id": jid, "ok": true})
}
