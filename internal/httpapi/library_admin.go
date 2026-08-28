package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/library/merge"
)

func (s *Server) libraryStorageType(ctx context.Context, lib uuid.UUID) string {
	var typ string
	_ = s.Pool.QueryRow(ctx, `
		SELECT sp.type FROM libraries l
		JOIN storage_providers sp ON sp.id = l.storage_provider_id
		WHERE l.id=$1`, lib).Scan(&typ)
	return typ
}

func managedStorage(typ string) bool {
	return typ == "managed"
}

type storedFile struct {
	Lib uuid.UUID
	Key string
	Typ string
}

func (s *Server) collectManagedFiles(ctx context.Context, trackIDs []uuid.UUID) []storedFile {
	return s.collectTrackFiles(ctx, trackIDs, true)
}

func (s *Server) collectTrackFiles(ctx context.Context, trackIDs []uuid.UUID, managedOnly bool) []storedFile {
	if len(trackIDs) == 0 {
		return nil
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT tf.library_id, tf.storage_key, sp.type
		FROM track_files tf
		JOIN libraries l ON l.id = tf.library_id
		JOIN storage_providers sp ON sp.id = l.storage_provider_id
		WHERE tf.track_id = ANY($1) AND tf.deleted_at IS NULL`, trackIDs)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []storedFile
	for rows.Next() {
		var f storedFile
		if err := rows.Scan(&f.Lib, &f.Key, &f.Typ); err != nil {
			continue
		}
		if f.Key == "" {
			continue
		}
		if managedOnly && !managedStorage(f.Typ) {
			continue
		}
		out = append(out, f)
	}
	return out
}

func (s *Server) PurgeTrackMedia(ctx context.Context, trackID uuid.UUID) (int64, error) {
	var size int64
	_ = s.Pool.QueryRow(ctx, `
		SELECT coalesce(sum(tf.size_bytes),0)
		FROM track_files tf
		JOIN libraries l ON l.id = tf.library_id
		JOIN storage_providers sp ON sp.id = l.storage_provider_id
		WHERE tf.track_id=$1 AND tf.deleted_at IS NULL AND sp.type='managed'`, trackID).Scan(&size)
	// Physical delete only for managed storage. NAS/S3/local files stay on disk
	// even when libraries.retention_opt_in is true.
	files := s.collectTrackFiles(ctx, []uuid.UUID{trackID}, true)
	if _, err := s.Pool.Exec(ctx, `DELETE FROM track_files WHERE track_id=$1`, trackID); err != nil {
		return 0, err
	}
	if _, err := s.Pool.Exec(ctx, `
		UPDATE tracks SET media_unavailable_at=COALESCE(media_unavailable_at, now()), updated_at=now()
		WHERE id=$1`, trackID); err != nil {
		return 0, err
	}
	s.deleteManagedFiles(ctx, files)
	return size, nil
}

func (s *Server) deleteManagedFiles(ctx context.Context, files []storedFile) {
	for _, f := range files {
		if !managedStorage(f.Typ) {
			continue
		}
		var n int
		_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM track_files WHERE storage_key=$1`, f.Key).Scan(&n)
		if n > 0 {
			continue
		}
		prov, _, _, err := s.ProviderFor(ctx, f.Lib)
		if err != nil {
			continue
		}
		_ = prov.Delete(ctx, f.Key)
	}
}

func (s *Server) nonManagedTrackSkips(ctx context.Context, ids []uuid.UUID) []map[string]any {
	out := []map[string]any{}
	if len(ids) == 0 {
		return out
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT t.id, sp.type
		FROM tracks t
		JOIN libraries l ON l.id = t.library_id
		JOIN storage_providers sp ON sp.id = l.storage_provider_id
		WHERE t.id = ANY($1)`, ids)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var typ string
		if err := rows.Scan(&id, &typ); err != nil {
			continue
		}
		if managedStorage(typ) {
			continue
		}
		out = append(out, map[string]any{
			"track_id":     id,
			"storage_type": typ,
			"reason":       "physical delete is only offered for SoundDock-managed tracks",
		})
	}
	return out
}

func (s *Server) previewNonManagedDeletes(ctx context.Context, ids []uuid.UUID, all bool, lib uuid.UUID) []map[string]any {
	if all {
		got, err := s.collectDeleteIDs(ctx, lib)
		if err != nil {
			return []map[string]any{}
		}
		ids = got
	}
	return s.nonManagedTrackSkips(ctx, ids)
}

func (s *Server) adminDeleteLibrary(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid library id")
		return
	}
	var body struct {
		DeleteFiles bool `json:"delete_files"`
	}
	_ = decodeJSON(r, &body)

	var n int
	_ = s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM libraries`).Scan(&n)
	if n <= 1 {
		writeErr(w, 409, "last_library", "cannot delete the last library")
		return
	}
	var exists bool
	if err := s.Pool.QueryRow(r.Context(), `SELECT true FROM libraries WHERE id=$1`, id).Scan(&exists); err != nil {
		writeErr(w, 404, "not_found", "library not found")
		return
	}
	typ := s.libraryStorageType(r.Context(), id)
	if body.DeleteFiles && !managedStorage(typ) {
		writeErr(w, 400, "storage", "physical delete is only offered for SoundDock-managed libraries")
		return
	}
	if s.Jobs == nil {
		if err := s.deleteLibrary(r.Context(), id, body.DeleteFiles, currentUser(r).ID); err != nil {
			writeErr(w, 500, "library", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "deleted_files": body.DeleteFiles && managedStorage(typ)})
		return
	}
	jid, err := s.Jobs.Enqueue(r.Context(), "library.delete", libraryDeletePayload{
		ID: id, DeleteFiles: body.DeleteFiles, ActorID: currentUser(r).ID,
	})
	if err != nil {
		s.writeJobErr(w, err)
		return
	}
	writeJSON(w, 202, map[string]any{"ok": true, "queued": true, "job_id": jid, "deleted_files": body.DeleteFiles && managedStorage(typ)})
}

func (s *Server) reassignDefault(ctx context.Context, except uuid.UUID) error {
	_, _ = s.Pool.Exec(ctx, `UPDATE libraries SET is_default=FALSE WHERE is_default`)
	var next uuid.UUID
	err := s.Pool.QueryRow(ctx, `
		SELECT l.id FROM libraries l
		WHERE l.id <> $1
		ORDER BY (SELECT count(*) FROM tracks t WHERE t.library_id=l.id) DESC, l.created_at
		LIMIT 1`, except).Scan(&next)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `UPDATE libraries SET is_default=TRUE WHERE id=$1`, next)
	return err
}

func (s *Server) adminSetDefaultLibrary(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid library id")
		return
	}
	var exists bool
	if err := s.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM libraries WHERE id=$1)`, id).Scan(&exists); err != nil || !exists {
		writeErr(w, 404, "not_found", "library not found")
		return
	}
	tx, err := s.Pool.Begin(r.Context())
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `UPDATE libraries SET is_default=FALSE WHERE is_default`); err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE libraries SET is_default=TRUE WHERE id=$1`, id); err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	s.Audit.Event(r.Context(), &currentUser(r).ID, "library.default", id.String(), r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) adminMergeLibraries(w http.ResponseWriter, r *http.Request) {
	dest, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid library id")
		return
	}
	var body struct {
		SourceIDs []uuid.UUID `json:"source_ids"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	if len(body.SourceIDs) == 0 {
		writeErr(w, 400, "invalid", "source_ids required")
		return
	}
	var destProv uuid.UUID
	if err := s.Pool.QueryRow(r.Context(), `SELECT storage_provider_id FROM libraries WHERE id=$1`, dest).Scan(&destProv); err != nil {
		writeErr(w, 404, "not_found", "destination library not found")
		return
	}
	if s.Jobs == nil {
		moved := 0
		for _, src := range body.SourceIDs {
			if src == dest {
				continue
			}
			n, err := s.mergeLibraryInto(r.Context(), src, dest, destProv)
			if errors.Is(err, merge.ErrTrackInUse) {
				writeErr(w, 409, "track_in_use", "cannot merge a track that is currently playing")
				return
			}
			if err != nil {
				writeErr(w, 400, "merge", err.Error())
				return
			}
			moved += n
		}
		s.Audit.Event(r.Context(), &currentUser(r).ID, "library.merge", dest.String(), r.RemoteAddr, nil)
		writeJSON(w, 200, map[string]any{"ok": true, "moved_tracks": moved})
		return
	}
	jid, err := s.Jobs.Enqueue(r.Context(), "library.merge", libraryMergePayload{
		Dest: dest, SourceIDs: body.SourceIDs, ActorID: currentUser(r).ID,
	})
	if err != nil {
		s.writeJobErr(w, err)
		return
	}
	writeJSON(w, 202, map[string]any{"ok": true, "queued": true, "job_id": jid})
}

func (s *Server) mergeLibraryInto(ctx context.Context, src, dest, _ uuid.UUID) (int, error) {
	return merge.LibraryInto(ctx, s.Pool, src, dest)
}
