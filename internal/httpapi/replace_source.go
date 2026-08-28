package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/scapex"
	"github.com/sounddock/sounddock/internal/storage"
)

const replaceSourcePerm = "tracks.replace_source"

type replaceSourcePayload struct {
	TrackID       uuid.UUID `json:"track_id"`
	URLs          []string  `json:"urls"`
	SourceRefs    []string  `json:"source_refs"`
	CoalesceKey   string    `json:"coalesce_key"`
	DestLibraryID string    `json:"dest_library_id"`
	MediaPolicyID string    `json:"media_policy_id"`
	Provider      string    `json:"provider"`
	ActorID       uuid.UUID `json:"actor_id"`
}

// MountReplaceSource registers POST /tracks/{id}/replace-source on the given
// router (typically the authenticated /api/v1 group). W8-http calls this.
func (s *Server) MountReplaceSource(r chi.Router) {
	if r == nil {
		return
	}
	r.Post("/tracks/{id}/replace-source", s.replaceSource)
}

// RegisterReplaceJobs registers tracks.replace_source. W8-http calls this
// from RegisterJobs / server wiring.
func (s *Server) RegisterReplaceJobs() {
	if s == nil || s.Jobs == nil {
		return
	}
	s.Jobs.Register(scapex.JobTypeReplaceSource, s.jobReplaceSource)
}

func (s *Server) requireReplaceSource(w http.ResponseWriter, r *http.Request) bool {
	if !auth.HasPerm(currentUser(r), replaceSourcePerm) {
		writeErr(w, http.StatusForbidden, "forbidden", "replace source not permitted")
		return false
	}
	s.ensurePerm(r.Context(), replaceSourcePerm)
	return true
}

func (s *Server) replaceSource(w http.ResponseWriter, r *http.Request) {
	if !s.requireReplaceSource(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid", "invalid track id")
		return
	}
	var body struct {
		URL       string `json:"url"`
		SourceRef string `json:"source_ref"`
		Provider  string `json:"provider"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid", "invalid body")
		return
	}
	raw := strings.TrimSpace(body.SourceRef)
	if raw == "" {
		raw = strings.TrimSpace(body.URL)
	}
	ref := scapex.CanonicalSourceRef(raw)
	if ref == "" || scapex.VideoID(ref) == "" {
		writeErr(w, http.StatusBadRequest, "invalid", "not an allowlisted YouTube watch URL or video id")
		return
	}
	provider := scapex.NormalizeProvider(body.Provider)
	if s.Pool == nil {
		writeErr(w, http.StatusServiceUnavailable, "db", "database unavailable")
		return
	}
	var lib uuid.UUID
	if err := s.Pool.QueryRow(r.Context(), `SELECT library_id FROM tracks WHERE id=$1`, id).Scan(&lib); err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "track not found")
		return
	}
	if !s.requireLibraryWrite(w, r, lib) {
		return
	}
	if s.Jobs == nil {
		writeErr(w, http.StatusServiceUnavailable, "jobs", "worker runner is not available")
		return
	}
	policy := s.loadAcquisitionPolicy(r.Context()).MediaPolicyID
	if policy == "" {
		policy = scapex.DefaultMediaPolicy
	}
	policy = scapex.NormalizePolicy(policy)
	key := scapex.ReplaceCoalesceKey(id, provider, ref, lib.String(), policy)
	payload := replaceSourcePayload{
		TrackID:       id,
		URLs:          []string{"https://www.youtube.com/watch?v=" + ref},
		SourceRefs:    []string{ref},
		CoalesceKey:   key,
		DestLibraryID: lib.String(),
		MediaPolicyID: policy,
		Provider:      provider,
	}
	if u := currentUser(r); u != nil {
		payload.ActorID = u.ID
	}
	jid, err := s.Jobs.EnqueueCoalesced(r.Context(), scapex.JobTypeReplaceSource, key, payload)
	if err != nil {
		s.writeJobErr(w, err)
		return
	}
	if u := currentUser(r); u != nil && s.Audit != nil {
		s.Audit.Event(r.Context(), &u.ID, "tracks.replace_source", id.String(), r.RemoteAddr, map[string]any{"job_id": jid})
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"job_id":       jid,
		"track_id":     id,
		"coalesce_key": key,
	})
}

func (s *Server) jobReplaceSource(ctx context.Context, job jobs.Job) error {
	var p replaceSourcePayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return err
	}
	if s.ScapeX == nil {
		return errScapeXDown
	}
	trackID := p.TrackID
	if trackID == uuid.Nil {
		return errString("track_id required")
	}
	refs := p.URLs
	if len(refs) == 0 {
		refs = p.SourceRefs
	}
	lib := uuid.Nil
	if p.DestLibraryID != "" {
		lib, _ = uuid.Parse(p.DestLibraryID)
	}
	if lib == uuid.Nil && s.Pool != nil {
		_ = s.Pool.QueryRow(ctx, `SELECT library_id FROM tracks WHERE id=$1`, trackID).Scan(&lib)
	}
	policy := p.MediaPolicyID
	if policy == "" {
		policy = scapex.DefaultMediaPolicy
	}
	locals, err := s.ScapeX.RunReplaceAcquire(ctx, scapex.ReplaceOpts{
		JobID:       job.ID,
		TrackID:     trackID,
		URLs:        refs,
		DestLibrary: lib,
		Policy:      policy,
		UserID:      p.ActorID,
		Quota:       s.CheckQuota,
	})
	if work := s.ScapeX.JobWork(job.ID); work != "" {
		defer os.RemoveAll(work)
	}
	if err != nil {
		return err
	}
	retired, newKey, err := s.commitReplaceLocals(ctx, job.ID, trackID, lib, p.Provider, firstSourceRef(p.SourceRefs, refs), locals)
	if err != nil {
		return err
	}
	s.maybeDeleteRetiredReplaceFiles(ctx, job.ID, trackID, retired)
	if s.Jobs != nil {
		s.Jobs.SetResult(ctx, job.ID, map[string]any{
			"track_id":    trackID,
			"storage_key": newKey,
		})
	}
	return nil
}

func firstSourceRef(refs, urls []string) string {
	for _, r := range refs {
		if id := scapex.CanonicalSourceRef(r); id != "" {
			return id
		}
	}
	for _, r := range urls {
		if id := scapex.CanonicalSourceRef(r); id != "" {
			return id
		}
	}
	return ""
}

func (s *Server) commitReplaceLocals(ctx context.Context, jobID, trackID, lib uuid.UUID, provider, sourceRef string, locals []scapex.LocalTrack) ([]scapex.RetiredFile, string, error) {
	if len(locals) == 0 {
		return nil, "", errString("yt-dlp produced no audio")
	}
	loc := locals[0]
	st, err := os.Stat(loc.Path)
	if err != nil {
		return nil, "", err
	}
	if err := s.CheckQuota(ctx, uuid.Nil, lib, st.Size()); err != nil {
		return nil, "", err
	}
	hash, err := scapex.FileSHA256(loc.Path)
	if err != nil {
		return nil, "", err
	}
	ext := filepath.Ext(loc.Path)
	newKey := scapex.ReplaceStorageKey(trackID, jobID, ext)
	if err := s.writeReplaceObject(ctx, lib, newKey, loc.Path, st.Size()); err != nil {
		return nil, "", err
	}
	codec, container := scapex.CodecFromExt(ext)
	ref := sourceRef
	if ref == "" {
		ref = loc.VideoID
	}
	retired, err := scapex.CommitReplace(ctx, s.Pool, scapex.CommitReplaceInput{
		TrackID:     trackID,
		LibraryID:   lib,
		StorageKey:  newKey,
		SizeBytes:   st.Size(),
		ContentHash: hash,
		Codec:       codec,
		Container:   container,
		DurationMS:  loc.DurationMS,
		Provider:    provider,
		SourceRef:   ref,
		JobID:       jobID,
	})
	if err != nil {
		return nil, "", err
	}
	return retired, newKey, nil
}

func (s *Server) writeReplaceObject(ctx context.Context, lib uuid.UUID, key, src string, size int64) error {
	if s.Pool == nil {
		return errString("database unavailable")
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	prov, _, _, err := s.ProviderFor(ctx, lib)
	if err != nil {
		return err
	}
	return prov.Write(ctx, key, f, storage.WriteInfo{Size: size})
}

func (s *Server) maybeDeleteRetiredReplaceFiles(ctx context.Context, jobID, trackID uuid.UUID, retired []scapex.RetiredFile) {
	if len(retired) == 0 || s.Pool == nil {
		return
	}
	busy, err := scapex.ReplaceMediaBusy(ctx, s.Pool, trackID, jobID)
	if err != nil || busy {
		return
	}
	for _, f := range retired {
		typ := s.libraryStorageType(ctx, f.LibraryID)
		if !managedStorage(typ) {
			continue
		}
		var live int
		_ = s.Pool.QueryRow(ctx, `
			SELECT count(*) FROM track_files
			WHERE storage_key=$1 AND deleted_at IS NULL`, f.StorageKey).Scan(&live)
		if live > 0 {
			continue
		}
		prov, _, _, err := s.ProviderFor(ctx, f.LibraryID)
		if err != nil {
			continue
		}
		_ = prov.Delete(ctx, f.StorageKey)
	}
}
