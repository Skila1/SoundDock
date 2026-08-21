package httpapi

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Server) ensureDefaultLibrary(ctx context.Context) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.Pool.QueryRow(ctx, `
		SELECT l.id
		FROM libraries l
		JOIN storage_providers sp ON sp.id = l.storage_provider_id
		WHERE l.read_only = false AND sp.type IN ('managed', 'local')
		ORDER BY CASE WHEN lower(l.name) = 'music' THEN 0 ELSE 1 END, l.created_at
		LIMIT 1`).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != nil && err != pgx.ErrNoRows {
		return uuid.Nil, err
	}

	var sid uuid.UUID
	err = s.Pool.QueryRow(ctx, `
		SELECT id FROM storage_providers
		WHERE type IN ('managed', 'local')
		ORDER BY CASE WHEN type = 'managed' THEN 0 ELSE 1 END, created_at
		LIMIT 1`).Scan(&sid)
	if err != nil {
		root := s.Cfg.ManagedDir
		enc := []byte(root)
		if s.Box != nil {
			if b, e := s.Box.Encrypt([]byte(root)); e == nil {
				enc = b
			}
		}
		if err := s.Pool.QueryRow(ctx, `
			INSERT INTO storage_providers (name, type, config_enc)
			VALUES ('Managed', 'managed', $1) RETURNING id`, enc).Scan(&sid); err != nil {
			return uuid.Nil, err
		}
	}

	if err := s.Pool.QueryRow(ctx, `
		INSERT INTO libraries (name, kind, storage_provider_id, root_prefix, read_only, organisation_mode)
		VALUES ('Music', 'music', $1, '', false, 'virtual') RETURNING id`, sid).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	_, _ = s.Pool.Exec(ctx, `INSERT INTO library_grants (library_id, role_id, actions)
		SELECT $1, id, ARRAY['read','stream','write','admin'] FROM roles WHERE name='Administrator'`, id)
	_, _ = s.Pool.Exec(ctx, `INSERT INTO library_grants (library_id, role_id, actions)
		SELECT $1, id, ARRAY['read','stream','write'] FROM roles WHERE name='User'`, id)
	return id, nil
}

func (s *Server) resolveLibraryID(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	if id != uuid.Nil {
		var ok bool
		if err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM libraries WHERE id=$1 AND read_only=false)`, id).Scan(&ok); err == nil && ok {
			return id, nil
		}
	}
	return s.ensureDefaultLibrary(ctx)
}
