package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
	"github.com/sounddock/sounddock/internal/radio"
	"github.com/sounddock/sounddock/internal/scapex"
)

func (s *Server) searchYouTube(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	song := scapex.SongQuery(q)
	if song == "" || s.ScapeX == nil {
		writeJSON(w, 200, map[string]any{"query": q, "results": []any{}})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 8
	}
	if limit > 16 {
		limit = 16
	}
	hits, err := s.YouTube().Search(r.Context(), song, limit+8)
	if err != nil || len(hits) == 0 {
		writeJSON(w, 200, map[string]any{"query": q, "results": []any{}})
		return
	}
	hits = scapex.RankHits(song, hits)
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		out = append(out, map[string]any{
			"type":        "youtube",
			"id":          h.ID,
			"title":       h.Title,
			"artist":      h.Artist,
			"album":       h.Album,
			"duration_ms": h.DurationMS,
			"source":      "youtube",
			"artwork_url": h.ArtworkURL,
			"stream_url":  h.StreamURL,
		})
	}
	writeJSON(w, 200, map[string]any{"query": q, "results": out})
}

func (s *Server) resolveQueueTracks(ctx context.Context, refs []string) ([]uuid.UUID, error) {
	tracks, youtube := scapex.ParseTrackRefs(refs)
	if err := s.reacquireMissing(ctx, tracks); err != nil {
		return nil, err
	}
	if len(youtube) == 0 {
		return tracks, nil
	}
	got, err := s.fetchYouTube(ctx, youtube)
	if err != nil {
		return nil, err
	}
	return append(tracks, got...), nil
}

func (s *Server) reacquireMissing(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 || s.ScapeX == nil {
		return nil
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT t.id, t.acquisition_ref, t.library_id
		FROM tracks t
		WHERE t.id = ANY($1)
		  AND coalesce(t.acquisition_ref,'') <> ''
		  AND t.acquisition IN ('youtube', 'scapex', '')
		  AND (
		    t.media_unavailable_at IS NOT NULL
		    OR NOT EXISTS (
		      SELECT 1 FROM track_files tf
		      WHERE tf.track_id=t.id AND tf.quality='original' AND tf.deleted_at IS NULL
		    )
		  )`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	type missing struct {
		id  uuid.UUID
		ref string
		lib uuid.UUID
	}
	var rowsMissing []missing
	seen := map[string]bool{}
	for rows.Next() {
		var m missing
		if rows.Scan(&m.id, &m.ref, &m.lib) != nil {
			continue
		}
		m.ref = strings.TrimSpace(m.ref)
		if m.ref == "" {
			continue
		}
		key := m.id.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		rowsMissing = append(rowsMissing, m)
	}
	for _, m := range rowsMissing {
		in := s.intentFromCtx(ctx)
		in.TrackID = m.id
		in.SourceRef = m.ref
		in.DestLibraryID = m.lib
		if _, err := s.enqueueAcquisition(ctx, in); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) fetchYouTube(ctx context.Context, refs []string) ([]uuid.UUID, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	return s.enqueueYouTubeRefs(ctx, refs)
}

func (s *Server) enqueueYouTubeRefs(ctx context.Context, refs []string) ([]uuid.UUID, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	if s.Jobs == nil || !s.Jobs.Started() {
		if s.ScapeX == nil {
			return nil, errScapeXDown
		}
		return s.ScapeX.Fetch(ctx, refs)
	}
	base := s.intentFromCtx(ctx)
	if base.DestLibraryID == uuid.Nil && s.ScapeX != nil {
		if lib, err := s.ScapeX.DestLibrary(ctx); err == nil {
			base.DestLibraryID = lib
		}
	}
	if base.DestLibraryID == uuid.Nil {
		lib, err := s.resolveLibraryID(ctx, uuid.Nil)
		if err != nil {
			return nil, err
		}
		base.DestLibraryID = lib
	}
	var ids []uuid.UUID
	for _, ref := range refs {
		in := base
		in.SourceRef = ref
		id, err := s.enqueueAcquisition(ctx, in)
		if err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *Server) enqueueAcquisition(ctx context.Context, in scapex.IntentInput) (uuid.UUID, error) {
	ref := scapex.CanonicalSourceRef(in.SourceRef)
	if ref == "" {
		return uuid.Nil, errString("not an allowlisted YouTube watch URL or video id")
	}
	in.SourceRef = ref
	in.Provider = scapex.NormalizeProvider(in.Provider)
	in.MediaPolicyID = scapex.NormalizePolicy(in.MediaPolicyID)
	in.Intent = scapex.NormalizeIntent(in.Intent)
	if in.DestLibraryID == uuid.Nil {
		return uuid.Nil, errString("dest library required")
	}
	if u, ok := ctx.Value(userKey).(*auth.User); ok && u != nil && !u.IsAdmin {
		if !s.userHasLibraryAction(ctx, u, in.DestLibraryID, "write") {
			return uuid.Nil, errString("library write not granted")
		}
	}
	if in.TrackID == uuid.Nil {
		id, err := scapex.EnsureStubTrack(ctx, s.Pool, in.DestLibraryID, ref, hintForRef(ctx, ref))
		if err != nil {
			return uuid.Nil, err
		}
		in.TrackID = id
	}
	key := scapex.CoalesceKey(in.Provider, ref, in.DestLibraryID.String(), in.MediaPolicyID)
	payload := map[string]any{
		"urls":            []string{"https://www.youtube.com/watch?v=" + ref},
		"source_refs":     []string{ref},
		"coalesce_key":    key,
		"dest_library_id": in.DestLibraryID.String(),
		"media_policy_id": in.MediaPolicyID,
	}
	jobID, err := s.Jobs.EnqueueCoalesced(ctx, "scapex.fetch", key, payload)
	if err != nil {
		return uuid.Nil, err
	}
	if in.UserID != uuid.Nil {
		if _, err := scapex.InsertIntent(ctx, s.Pool, in, jobID); err != nil {
			return in.TrackID, err
		}
	}
	return in.TrackID, nil
}

type acquireKey struct{}
type trackHintsKey struct{}

// WithAcquisitionIntent lets W6-http attach play/queue snapshot fields.
func WithAcquisitionIntent(ctx context.Context, in scapex.IntentInput) context.Context {
	return context.WithValue(ctx, acquireKey{}, in)
}

func withTrackHints(ctx context.Context, hints map[string]scapex.TrackHint) context.Context {
	if len(hints) == 0 {
		return ctx
	}
	return context.WithValue(ctx, trackHintsKey{}, hints)
}

func hintForRef(ctx context.Context, ref string) scapex.TrackHint {
	m, _ := ctx.Value(trackHintsKey{}).(map[string]scapex.TrackHint)
	if len(m) == 0 {
		return scapex.TrackHint{}
	}
	if h, ok := m[ref]; ok {
		return h
	}
	if h, ok := m[scapex.CanonicalSourceRef(ref)]; ok {
		return h
	}
	return scapex.TrackHint{}
}

func (s *Server) intentFromCtx(ctx context.Context) scapex.IntentInput {
	in, _ := ctx.Value(acquireKey{}).(scapex.IntentInput)
	if in.UserID == uuid.Nil {
		in.UserID = userIDFromCtx(ctx)
	}
	if in.Intent == "" {
		in.Intent = scapex.IntentQueue
	}
	if in.Provider == "" {
		in.Provider = scapex.ProviderYouTube
	}
	if in.MediaPolicyID == "" {
		in.MediaPolicyID = scapex.DefaultMediaPolicy
	}
	if in.SessionID != uuid.Nil && (in.ExpectedStateRevision != 0 || in.ExpectedInstanceID != uuid.Nil) {
		return in
	}
	if in.SessionID == uuid.Nil && in.UserID != uuid.Nil && s.Pool != nil {
		var sid uuid.UUID
		var rev int64
		var inst *uuid.UUID
		err := s.Pool.QueryRow(ctx, `
			SELECT id, state_revision, playback_instance_id
			FROM playback_sessions
			WHERE user_id=$1
			ORDER BY updated_at DESC LIMIT 1`, in.UserID).Scan(&sid, &rev, &inst)
		if err == nil {
			if in.SessionID == uuid.Nil {
				in.SessionID = sid
			}
			if in.ExpectedStateRevision == 0 {
				in.ExpectedStateRevision = rev
			}
			if in.ExpectedInstanceID == uuid.Nil && inst != nil {
				in.ExpectedInstanceID = *inst
			}
		}
	}
	return in
}

func (s *Server) similarYouTubeHits(ctx context.Context, seed uuid.UUID, need int, have []uuid.UUID) []scapex.Hit {
	if need < 1 || s.ScapeX == nil {
		return nil
	}
	need = radio.ClampFill(need)
	meta, err := radio.New(s.Pool).TrackMeta(ctx, seed)
	if err != nil {
		return nil
	}
	q := radio.SimilarQuery(meta.Title, meta.Artist, meta.Genre)
	if q == "" {
		return nil
	}
	hits, err := s.YouTube().Search(ctx, q, need+8)
	if err != nil || len(hits) == 0 {
		return nil
	}
	hits = scapex.RankHits(q, hits)
	ids := append([]uuid.UUID{seed}, have...)
	local := s.trackTitleArtist(ctx, ids)
	var out []scapex.Hit
	seen := map[string]struct{}{}
	for _, h := range hits {
		if h.ID == "" {
			continue
		}
		if _, ok := seen[h.ID]; ok {
			continue
		}
		if radio.SameSong(h.Title, meta.Title) {
			continue
		}
		if scapex.AlreadyInLibrary(h.Title, h.Artist, local) {
			continue
		}
		seen[h.ID] = struct{}{}
		out = append(out, h)
		if len(out) >= need {
			break
		}
	}
	return out
}

func (s *Server) trackTitleArtist(ctx context.Context, ids []uuid.UUID) []map[string]any {
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT t.title, coalesce((SELECT string_agg(ar.name, ', ' ORDER BY ta.position)
		  FROM track_artists ta JOIN artists ar ON ar.id=ta.artist_id
		  WHERE ta.track_id=t.id AND ta.role='primary'),'')
		FROM tracks t WHERE t.id = ANY($1)`, ids)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var title, artist string
		if err := rows.Scan(&title, &artist); err != nil {
			continue
		}
		out = append(out, map[string]any{"type": "track", "title": title, "artist": artist})
	}
	return out
}

var errScapeXDown = errString("ScapeX is not running")

type errString string

func (e errString) Error() string { return string(e) }
