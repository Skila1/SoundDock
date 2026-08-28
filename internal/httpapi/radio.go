package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/radio"
	"github.com/sounddock/sounddock/internal/scapex"
)

func (s *Server) getRadio(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		writeErr(w, 400, "invalid", "kind required")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	fillYouTube := strings.EqualFold(r.URL.Query().Get("fill"), "youtube")
	if fillYouTube {
		limit = radio.ClampFill(limit)
	}
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
		Kind:    kind,
		SeedID:  seed,
		Genre:   r.URL.Query().Get("genre"),
		Decade:  decade,
		Limit:   limit,
		UserID:  u.ID,
		Libs:    s.libraryIDs(r.Context(), u),
		Exclude: parseUUIDList(r.URL.Query().Get("exclude")),
		Recent:  atoiDefault(r.URL.Query().Get("recent"), 40),
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
	if fillYouTube && seed != uuid.Nil && s.hasAudioListener(r) {
		need := limit - len(res.TrackIDs)
		if need > 0 {
			have := append(append([]uuid.UUID{}, res.TrackIDs...), req.Exclude...)
			hits := s.youtubeFillHits(r.Context(), seed, need, have)
			res.Hits = hits
			for _, h := range hits {
				if h.ID != "" {
					res.YoutubeIDs = append(res.YoutubeIDs, h.ID)
				}
			}
		}
	}
	writeJSON(w, 200, res)
}

func (s *Server) youtubeFillHits(ctx context.Context, seed uuid.UUID, need int, have []uuid.UUID) []scapex.Hit {
	if s != nil && s.youtubeFillHook != nil {
		var hits []scapex.Hit
		for _, id := range s.youtubeFillHook(ctx, seed, need, have) {
			hits = append(hits, scapex.Hit{Type: "youtube", ID: id})
		}
		return hits
	}
	return s.similarYouTubeHits(ctx, seed, need, have)
}

func (s *Server) hasAudioListener(r *http.Request) bool {
	if s == nil || s.Play == nil {
		return false
	}
	sid, err := s.attachedPlaySession(r, nil, "")
	if err != nil || sid == uuid.Nil {
		return false
	}
	ok, err := s.Play.HasAudioListener(r.Context(), sid)
	return err == nil && ok
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
		s.writeJobErr(w, err)
		return
	}
	writeJSON(w, 202, map[string]any{"job_id": jid, "ok": true})
}

func parseUUIDList(raw string) []uuid.UUID {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []uuid.UUID
	for _, p := range strings.Split(raw, ",") {
		id, err := uuid.Parse(strings.TrimSpace(p))
		if err != nil {
			continue
		}
		out = append(out, id)
	}
	return out
}

func atoiDefault(raw string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return def
	}
	return n
}
