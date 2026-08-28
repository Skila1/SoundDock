DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE name IN ('roles.manage', 'library.delete', 'tracks.delete'));
DELETE FROM permissions WHERE name IN ('roles.manage', 'library.delete', 'tracks.delete');
DROP TABLE IF EXISTS role_discord_links;
DROP INDEX IF EXISTS libraries_one_default;
ALTER TABLE libraries DROP COLUMN IF EXISTS is_default;
