DELETE FROM role_permissions
WHERE role_id = (SELECT id FROM roles WHERE name = 'User')
  AND permission_id = (SELECT id FROM permissions WHERE name = 'library.import_url');
