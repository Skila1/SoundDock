package integrity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/storage"
)

var (
	ErrNotTrashed = errors.New("file is not in trash")
	ErrConflict   = errors.New("original storage key is not free")
)

func TrashKey(fileID uuid.UUID, originalKey string) string {
	originalKey = strings.TrimPrefix(strings.ReplaceAll(originalKey, "\\", "/"), "/")
	return "trash/" + fileID.String() + "/" + originalKey
}

func OriginalKeyFromTrash(trashKey string, fileID uuid.UUID) (string, error) {
	prefix := "trash/" + fileID.String() + "/"
	k := strings.ReplaceAll(trashKey, "\\", "/")
	if !strings.HasPrefix(k, prefix) {
		return "", fmt.Errorf("not a trash key for this file")
	}
	orig := strings.TrimPrefix(k, prefix)
	if orig == "" {
		return "", fmt.Errorf("empty original key")
	}
	return orig, nil
}

func (s *Service) Trash(ctx context.Context, fileID uuid.UUID) (string, error) {
	var key string
	var lib uuid.UUID
	var deleted any
	err := s.pool.QueryRow(ctx, `SELECT storage_key, library_id, deleted_at FROM track_files WHERE id=$1`, fileID).Scan(&key, &lib, &deleted)
	if err != nil {
		return "", err
	}
	if deleted != nil {
		return key, nil
	}
	if strings.HasPrefix(strings.ReplaceAll(key, "\\", "/"), "trash/") {
		_, _ = s.pool.Exec(ctx, `UPDATE track_files SET deleted_at=now() WHERE id=$1`, fileID)
		return key, nil
	}
	prov, _, _, err := s.getProv(ctx, lib)
	if err != nil {
		return "", err
	}
	dest := TrashKey(fileID, key)
	if err := MoveObject(ctx, prov, key, dest); err != nil && !storage.IsNotExist(err) {
		return "", err
	}
	_, err = s.pool.Exec(ctx, `UPDATE track_files SET storage_key=$2, deleted_at=now() WHERE id=$1`, fileID, dest)
	return dest, err
}

func (s *Service) Restore(ctx context.Context, fileID uuid.UUID) (string, error) {
	var key string
	var lib uuid.UUID
	var deleted any
	err := s.pool.QueryRow(ctx, `SELECT storage_key, library_id, deleted_at FROM track_files WHERE id=$1`, fileID).Scan(&key, &lib, &deleted)
	if err != nil {
		return "", err
	}
	orig, err := OriginalKeyFromTrash(key, fileID)
	if err != nil {
		if deleted == nil {
			return key, nil
		}
		return "", ErrNotTrashed
	}
	var taken bool
	_ = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM track_files WHERE library_id=$1 AND storage_key=$2 AND id<>$3)`, lib, orig, fileID).Scan(&taken)
	if taken {
		return "", ErrConflict
	}
	prov, _, _, err := s.getProv(ctx, lib)
	if err != nil {
		return "", err
	}
	if _, err := prov.Stat(ctx, orig); err == nil {
		return "", ErrConflict
	}
	if err := MoveObject(ctx, prov, key, orig); err != nil && !storage.IsNotExist(err) {
		return "", err
	}
	_, err = s.pool.Exec(ctx, `UPDATE track_files SET storage_key=$2, deleted_at=NULL WHERE id=$1`, fileID, orig)
	return orig, err
}

func (s *Service) FilesRemoved(ctx context.Context, libID uuid.UUID, limit int) ([]map[string]any, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id::text, track_id::text, storage_key, size_bytes, deleted_at
		FROM track_files WHERE deleted_at IS NOT NULL`
	args := []any{}
	if libID != uuid.Nil {
		q += ` AND library_id=$1`
		args = append(args, libID)
	}
	q += ` ORDER BY deleted_at DESC LIMIT $` + itoa(len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, trackID, key string
		var size int64
		var at any
		if err := rows.Scan(&id, &trackID, &key, &size, &at); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "track_id": trackID, "storage_key": key, "size_bytes": size, "deleted_at": at,
		})
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, nil
}

func itoa(n int) string {
	if n < 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	if n == 0 {
		return "0"
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
