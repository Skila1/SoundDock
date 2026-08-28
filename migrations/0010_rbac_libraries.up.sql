-- Default library flag, Discord role links for RBAC groups, extra permissions.

ALTER TABLE libraries ADD COLUMN IF NOT EXISTS is_default BOOLEAN NOT NULL DEFAULT FALSE;

CREATE UNIQUE INDEX IF NOT EXISTS libraries_one_default ON libraries (is_default) WHERE is_default;

UPDATE libraries SET is_default = TRUE
WHERE id = (
  SELECT l.id FROM libraries l
  ORDER BY (SELECT count(*) FROM tracks t WHERE t.library_id = l.id) DESC, l.created_at
  LIMIT 1
)
AND NOT EXISTS (SELECT 1 FROM libraries WHERE is_default);

CREATE TABLE IF NOT EXISTS role_discord_links (
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    guild_id TEXT NOT NULL DEFAULT '',
    discord_role_id TEXT NOT NULL,
    PRIMARY KEY (role_id, guild_id, discord_role_id)
);

INSERT INTO permissions (name, description) VALUES
  ('roles.manage', 'Create groups and assign permissions'),
  ('library.delete', 'Remove libraries and tracks from the catalogue'),
  ('tracks.delete', 'Remove tracks from the catalogue')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r, permissions p
WHERE r.name = 'Administrator'
  AND p.name IN ('roles.manage', 'library.delete', 'tracks.delete')
ON CONFLICT DO NOTHING;
