package httpapi

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/library/merge"
)

type libraryMergePayload struct {
	Dest      uuid.UUID   `json:"dest"`
	SourceIDs []uuid.UUID `json:"source_ids"`
	ActorID   uuid.UUID   `json:"actor_id"`
}

type libraryDeletePayload struct {
	ID          uuid.UUID `json:"id"`
	DeleteFiles bool      `json:"delete_files"`
	ActorID     uuid.UUID `json:"actor_id"`
}

type tracksDeletePayload struct {
	IDs         []uuid.UUID `json:"ids"`
	All         bool        `json:"all"`
	LibraryID   uuid.UUID   `json:"library_id"`
	DeleteFiles bool        `json:"delete_files"`
	ActorID     uuid.UUID   `json:"actor_id"`
}

type tracksMetaPayload struct {
	IDs         []uuid.UUID `json:"ids"`
	Genre       *string     `json:"genre"`
	Year        *int        `json:"year"`
	Explicit    *bool       `json:"explicit"`
	DiscNumber  *int        `json:"disc_number"`
	TrackNumber *int        `json:"track_number"`
	WriteBack   bool        `json:"write_back"`
	ActorID     uuid.UUID   `json:"actor_id"`
}

func (s *Server) jobLibraryMerge(ctx context.Context, job jobs.Job) error {
	var p libraryMergePayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return err
	}
	moved := 0
	for _, src := range p.SourceIDs {
		if src == p.Dest {
			continue
		}
		n, err := merge.LibraryInto(ctx, s.Pool, src, p.Dest)
		if err != nil {
			return err
		}
		moved += n
		s.Jobs.SetProgress(ctx, job.ID, 10+80*moved/(len(p.SourceIDs)*100+1))
	}
	s.Jobs.SetResult(ctx, job.ID, map[string]any{"moved_tracks": moved})
	if p.ActorID != uuid.Nil {
		s.Audit.Event(ctx, &p.ActorID, "library.merge", p.Dest.String(), "", nil)
	}
	return nil
}

func (s *Server) jobLibraryDelete(ctx context.Context, job jobs.Job) error {
	var p libraryDeletePayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return err
	}
	return s.deleteLibrary(ctx, p.ID, p.DeleteFiles, p.ActorID)
}

func (s *Server) deleteLibrary(ctx context.Context, id uuid.UUID, deleteFiles bool, actor uuid.UUID) error {
	var n int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM libraries`).Scan(&n)
	if n <= 1 {
		return errString("cannot delete the last library")
	}
	var exists, isDefault bool
	if err := s.Pool.QueryRow(ctx, `SELECT true, is_default FROM libraries WHERE id=$1`, id).Scan(&exists, &isDefault); err != nil {
		return err
	}
	typ := s.libraryStorageType(ctx, id)
	if deleteFiles && !managedStorage(typ) {
		return errString("physical delete is only offered for SoundDock-managed libraries")
	}
	var files []storedFile
	if deleteFiles {
		rows, err := s.Pool.Query(ctx, `SELECT id FROM tracks WHERE library_id=$1`, id)
		if err == nil {
			defer rows.Close()
			var ids []uuid.UUID
			for rows.Next() {
				var tid uuid.UUID
				if rows.Scan(&tid) == nil {
					ids = append(ids, tid)
				}
			}
			files = s.collectManagedFiles(ctx, ids)
		}
	}
	if isDefault {
		if err := s.reassignDefault(ctx, id); err != nil {
			return err
		}
	}
	if _, err := s.Pool.Exec(ctx, `DELETE FROM tracks WHERE library_id=$1`, id); err != nil {
		return err
	}
	if deleteFiles {
		s.deleteManagedFiles(ctx, files)
	}
	if _, err := s.Pool.Exec(ctx, `DELETE FROM libraries WHERE id=$1`, id); err != nil {
		return err
	}
	if actor != uuid.Nil {
		s.Audit.Event(ctx, &actor, "library.delete", id.String(), "", nil)
	}
	return nil
}

func (s *Server) jobTracksDelete(ctx context.Context, job jobs.Job) error {
	var p tracksDeletePayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return err
	}
	n, skipped, err := s.deleteTrackIDs(ctx, p.IDs, p.All, p.LibraryID, p.DeleteFiles)
	if err != nil {
		return err
	}
	s.Jobs.SetResult(ctx, job.ID, map[string]any{"deleted": n, "deleted_files": p.DeleteFiles, "skipped": skipped})
	if p.ActorID != uuid.Nil {
		s.Audit.Event(ctx, &p.ActorID, "tracks.delete", "", "", nil)
	}
	return nil
}

func (s *Server) deleteTrackIDs(ctx context.Context, ids []uuid.UUID, all bool, lib uuid.UUID, deleteFiles bool) (int64, []map[string]any, error) {
	if all {
		var err error
		ids, err = s.collectDeleteIDs(ctx, lib)
		if err != nil {
			return 0, nil, err
		}
	}
	skipped := []map[string]any{}
	if len(ids) == 0 {
		return 0, skipped, nil
	}
	if deleteFiles {
		skipped = s.nonManagedTrackSkips(ctx, ids)
	}
	files := []storedFile{}
	if deleteFiles {
		files = s.collectManagedFiles(ctx, ids)
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM tracks WHERE id = ANY($1)`, ids)
	if err != nil {
		return 0, skipped, err
	}
	if deleteFiles {
		s.deleteManagedFiles(ctx, files)
	}
	return tag.RowsAffected(), skipped, nil
}

func (s *Server) collectDeleteIDs(ctx context.Context, lib uuid.UUID) ([]uuid.UUID, error) {
	q := `SELECT id FROM tracks`
	args := []any{}
	if lib != uuid.Nil {
		q += ` WHERE library_id=$1`
		args = append(args, lib)
	}
	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (s *Server) jobTracksMetadata(ctx context.Context, job jobs.Job) error {
	var p tracksMetaPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return err
	}
	meta := trackMetaBody{Genre: p.Genre, Year: p.Year, Explicit: p.Explicit, DiscNumber: p.DiscNumber, TrackNumber: p.TrackNumber, WriteBack: p.WriteBack}
	n := 0
	for i, id := range p.IDs {
		if s.Jobs.Cancelled(ctx, job.ID) {
			return context.Canceled
		}
		if s.trackGloballyLocked(ctx, id) {
			continue
		}
		s.applyTrackMeta(ctx, id, meta, p.ActorID)
		n++
		if len(p.IDs) > 0 {
			s.Jobs.SetProgress(ctx, job.ID, (i+1)*100/len(p.IDs))
		}
	}
	s.Jobs.SetResult(ctx, job.ID, map[string]any{"updated": n, "write_back": p.WriteBack})
	return nil
}
