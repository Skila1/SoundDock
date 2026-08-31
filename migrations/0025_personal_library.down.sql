DROP TABLE IF EXISTS personal_library_entries;
DROP TABLE IF EXISTS personal_library_owners;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_personal_library_visibility_chk;
ALTER TABLE users DROP COLUMN IF EXISTS personal_library_visibility;
