package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/storage"
)

func (s *Server) adminStorage(w http.ResponseWriter, r *http.Request) {
	s.dedupeManagedStorage(r.Context())
	rows, err := s.Pool.Query(r.Context(), `
		SELECT sp.id, sp.name, sp.type, sp.config_enc,
		       coalesce((SELECT count(*) FROM libraries l WHERE l.storage_provider_id=sp.id),0),
		       coalesce((SELECT json_agg(json_build_object('id', l.id, 'name', l.name))
		                 FROM libraries l WHERE l.storage_provider_id=sp.id), '[]'::json)
		FROM storage_providers sp
		ORDER BY CASE WHEN sp.type='managed' THEN 0 WHEN sp.type='local' THEN 1 ELSE 2 END, sp.name`)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var name, typ string
		var cfg []byte
		var libCount int
		var libs []byte
		if err := rows.Scan(&id, &name, &typ, &cfg, &libCount, &libs); err != nil {
			continue
		}
		item := s.storagePublic(r.Context(), id, name, typ, cfg, libCount, libs)
		out = append(out, item)
	}
	writeJSON(w, 200, out)
}

func (s *Server) adminCreateStorage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string          `json:"name"`
		Type   string          `json:"type"`
		Config json.RawMessage `json:"config"`
	}
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeErr(w, 400, "invalid", "name required")
		return
	}
	body.Type = strings.ToLower(strings.TrimSpace(body.Type))
	if body.Type == "" {
		body.Type = "local"
	}
	enc, err := s.encodeStorageConfig(body.Type, body.Config)
	if err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	var id uuid.UUID
	err = s.Pool.QueryRow(r.Context(), `INSERT INTO storage_providers (name, type, config_enc) VALUES ($1,$2,$3) RETURNING id`, body.Name, body.Type, enc).Scan(&id)
	if err != nil {
		writeErr(w, 400, "storage", err.Error())
		return
	}
	s.Audit.Event(r.Context(), &currentUser(r).ID, "storage.create", body.Name, r.RemoteAddr, nil)
	writeJSON(w, 201, map[string]any{"id": id})
}

func (s *Server) adminPatchStorage(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid id")
		return
	}
	var body struct {
		Name   string          `json:"name"`
		Config json.RawMessage `json:"config"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", "invalid body")
		return
	}
	var typ string
	var cfg []byte
	if err := s.Pool.QueryRow(r.Context(), `SELECT type, config_enc FROM storage_providers WHERE id=$1`, id).Scan(&typ, &cfg); err != nil {
		writeErr(w, 404, "not_found", "storage provider not found")
		return
	}
	if strings.TrimSpace(body.Name) != "" {
		_, _ = s.Pool.Exec(r.Context(), `UPDATE storage_providers SET name=$2 WHERE id=$1`, id, strings.TrimSpace(body.Name))
	}
	if len(body.Config) > 0 && string(body.Config) != "null" {
		merged := body.Config
		if typ == "s3" {
			merged = s.mergeS3Config(cfg, body.Config)
		}
		enc, err := s.encodeStorageConfig(typ, merged)
		if err != nil {
			writeErr(w, 400, "invalid", err.Error())
			return
		}
		_, _ = s.Pool.Exec(r.Context(), `UPDATE storage_providers SET config_enc=$2 WHERE id=$1`, id, enc)
	}
	s.Audit.Event(r.Context(), &currentUser(r).ID, "storage.update", id.String(), r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) adminDeleteStorage(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid id")
		return
	}
	var n int
	_ = s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM libraries WHERE storage_provider_id=$1`, id).Scan(&n)
	if n > 0 {
		writeErr(w, 409, "in_use", "This provider still has libraries. Move or delete those libraries first.")
		return
	}
	tag, err := s.Pool.Exec(r.Context(), `DELETE FROM storage_providers WHERE id=$1`, id)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, 404, "not_found", "storage provider not found")
		return
	}
	s.Audit.Event(r.Context(), &currentUser(r).ID, "storage.delete", id.String(), r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) mergeS3Config(prevEnc []byte, patch json.RawMessage) json.RawMessage {
	plain := prevEnc
	if s.Box != nil && len(prevEnc) > 0 {
		if p, err := s.Box.Decrypt(prevEnc); err == nil {
			plain = p
		}
	}
	var cur, next storage.S3Config
	_ = json.Unmarshal(plain, &cur)
	_ = json.Unmarshal(patch, &next)
	if next.Endpoint != "" {
		cur.Endpoint = next.Endpoint
	}
	if next.Region != "" {
		cur.Region = next.Region
	}
	if next.Bucket != "" {
		cur.Bucket = next.Bucket
	}
	if next.Prefix != "" || patchHas(patch, "prefix") {
		cur.Prefix = next.Prefix
	}
	if next.AccessKey != "" {
		cur.AccessKey = next.AccessKey
	}
	if next.SecretKey != "" {
		cur.SecretKey = next.SecretKey
	}
	if patchHas(patch, "use_ssl") {
		cur.UseSSL = next.UseSSL
	}
	b, _ := json.Marshal(cur)
	return b
}

func patchHas(raw json.RawMessage, key string) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	_, ok := m[key]
	return ok
}

func (s *Server) encodeStorageConfig(typ string, raw json.RawMessage) ([]byte, error) {
	switch typ {
	case "local", "managed":
		var c struct {
			Root string `json:"root"`
		}
		_ = json.Unmarshal(raw, &c)
		if strings.TrimSpace(c.Root) == "" {
			c.Root = s.Cfg.ManagedDir
		}
		if s.Box != nil {
			return s.Box.Encrypt([]byte(c.Root))
		}
		return []byte(c.Root), nil
	default:
		plain := raw
		if len(plain) == 0 {
			plain = []byte("{}")
		}
		if s.Box != nil {
			return s.Box.Encrypt(plain)
		}
		return plain, nil
	}
}

func (s *Server) storagePublic(ctx context.Context, id uuid.UUID, name, typ string, cfg []byte, libCount int, libsJSON []byte) map[string]any {
	plain := cfg
	if s.Box != nil && len(cfg) > 0 {
		if p, err := s.Box.Decrypt(cfg); err == nil {
			plain = p
		}
	}
	label, hint := storageTypeInfo(typ)
	item := map[string]any{
		"id":          id,
		"name":        name,
		"type":        typ,
		"type_label":  label,
		"description": hint,
		"libraries":   json.RawMessage(libsJSON),
		"lib_count":   libCount,
		"can_delete":  libCount == 0,
		"health":      "unknown",
	}
	root := ""
	switch typ {
	case "local", "managed":
		root = strings.TrimSpace(string(plain))
		if root == "" {
			root = s.Cfg.ManagedDir
		}
		item["root"] = root
		used, files := storage.DirUsed(root)
		item["used_bytes"] = used
		item["sounddock_bytes"] = used
		item["file_count"] = files
		if total, free, err := storage.DiskSpace(root); err == nil {
			item["total_bytes"] = total
			item["free_bytes"] = free
		}
		if _, err := os.Stat(root); err != nil {
			item["health"] = "fail"
		} else {
			item["health"] = "ok"
		}
	case "s3":
		var sc storage.S3Config
		_ = json.Unmarshal(plain, &sc)
		item["endpoint"] = sc.Endpoint
		item["region"] = sc.Region
		item["bucket"] = sc.Bucket
		item["prefix"] = sc.Prefix
		item["use_ssl"] = sc.UseSSL
		item["secret_set"] = sc.SecretKey != ""
		item["access_key_set"] = sc.AccessKey != ""
		if used, err := s3Used(ctx, sc); err == nil {
			item["used_bytes"] = used
		}
	}
	return item
}

func storageTypeInfo(typ string) (label, hint string) {
	switch typ {
	case "managed":
		return "SoundDock managed disk", "Files SoundDock downloaded or imported. Lives under the managed media folder on this machine."
	case "local":
		return "Local folder", "A folder on this machine that SoundDock can read and write."
	case "s3":
		return "S3 / R2", "Object storage (Cloudflare R2, Amazon S3, or another S3-compatible endpoint)."
	default:
		return typ, ""
	}
}

func (s *Server) dedupeManagedStorage(ctx context.Context) {
	if s.Pool == nil {
		return
	}
	var keep uuid.UUID
	err := s.Pool.QueryRow(ctx, `
		SELECT id FROM storage_providers
		WHERE type IN ('managed','local')
		ORDER BY CASE WHEN type='managed' THEN 0 ELSE 1 END, created_at
		LIMIT 1`).Scan(&keep)
	if err != nil {
		return
	}
	_, _ = s.Pool.Exec(ctx, `
		DELETE FROM storage_providers sp
		WHERE sp.name='Managed' AND sp.type IN ('managed','local') AND sp.id<>$1
		  AND NOT EXISTS (SELECT 1 FROM libraries l WHERE l.storage_provider_id=sp.id)`, keep)
}

func s3Used(ctx context.Context, cfg storage.S3Config) (int64, error) {
	if strings.TrimSpace(cfg.Bucket) == "" || strings.TrimSpace(cfg.Endpoint) == "" {
		return 0, nil
	}
	cli, err := storage.NewS3("usage", cfg)
	if err != nil {
		return 0, err
	}
	it, err := cli.List(ctx, "")
	if err != nil {
		return 0, err
	}
	defer it.Close()
	var n int64
	for it.Next() {
		n += it.Entry().Size
	}
	return n, it.Err()
}
