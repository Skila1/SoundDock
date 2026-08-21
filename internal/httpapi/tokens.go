package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
)

const patTokenPrefix = "sdp_"

func patPrefix(plain string) string {
	if len(plain) <= 10 {
		return plain
	}
	return plain[:10]
}

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `
		SELECT id, name, prefix, scopes, last_used_at, created_at
		FROM personal_access_tokens
		WHERE user_id=$1 AND revoked_at IS NULL
		ORDER BY created_at DESC`, currentUser(r).ID)
	if err != nil {
		writeErr(w, 500, "token", err.Error())
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "name", "prefix", "scopes", "last_used_at", "created_at"))
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeErr(w, 400, "invalid", "name required")
		return
	}
	if body.Scopes == nil {
		body.Scopes = []string{}
	}
	secret, err := cryptox.RandomToken(32)
	if err != nil {
		writeErr(w, 500, "token", err.Error())
		return
	}
	plain := patTokenPrefix + secret
	hash := cryptox.HashToken(plain)
	prefix := patPrefix(plain)
	u := currentUser(r)
	var id uuid.UUID
	var created time.Time
	err = s.Pool.QueryRow(r.Context(), `
		INSERT INTO personal_access_tokens (user_id, name, prefix, secret_hash, scopes)
		VALUES ($1,$2,$3,$4,$5) RETURNING id, created_at`,
		u.ID, strings.TrimSpace(body.Name), prefix, hash, body.Scopes).Scan(&id, &created)
	if err != nil {
		writeErr(w, 500, "token", err.Error())
		return
	}
	s.Audit.Event(r.Context(), &u.ID, "token.create", body.Name, r.RemoteAddr, nil)
	writeJSON(w, 201, map[string]any{
		"id":         id,
		"name":       strings.TrimSpace(body.Name),
		"prefix":     prefix,
		"scopes":     body.Scopes,
		"created_at": created,
		"secret":     plain,
		"note":       "shown once",
	})
}

func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid id")
		return
	}
	u := currentUser(r)
	cmd, err := s.Pool.Exec(r.Context(), `
		UPDATE personal_access_tokens SET revoked_at=now()
		WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, id, u.ID)
	if err != nil {
		writeErr(w, 500, "token", err.Error())
		return
	}
	if cmd.RowsAffected() == 0 {
		writeErr(w, 404, "not_found", "token not found")
		return
	}
	s.Audit.Event(r.Context(), &u.ID, "token.revoke", id.String(), r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
