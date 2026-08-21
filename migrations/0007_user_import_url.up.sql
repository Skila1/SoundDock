-- Allow regular users to remote-import audio the same way they can upload.

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.name = 'library.import_url'
WHERE r.name = 'User'
ON CONFLICT DO NOTHING;
