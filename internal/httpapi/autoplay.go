package httpapi

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
	"github.com/sounddock/sounddock/internal/playback"
	"github.com/sounddock/sounddock/internal/radio"
)

// WirePlayback attaches server-side autoplay fill. Safe to call more than once.
func (s *Server) WirePlayback() {
	if s == nil || s.Play == nil {
		return
	}
	s.Play.SetAutoplayFiller(s.ReplenishAutoplay)
}

// ReplenishAutoplay appends radio (and YouTube fill when someone is listening).
// Safe to call without the session mutex; it uses Add.
func (s *Server) ReplenishAutoplay(ctx context.Context, sid uuid.UUID) error {
	if s == nil || s.Play == nil || s.Pool == nil || sid == uuid.Nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	fillCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
	defer cancel()

	var autoplay, stopAfter bool
	var idx int
	var seed, userID uuid.UUID
	err := s.Pool.QueryRow(fillCtx, `
		SELECT autoplay, stop_after_current, current_index,
			coalesce(current_track_id, '00000000-0000-0000-0000-000000000000'),
			coalesce(user_id, '00000000-0000-0000-0000-000000000000')
		FROM playback_sessions WHERE id=$1`, sid).Scan(&autoplay, &stopAfter, &idx, &seed, &userID)
	if err != nil || seed == uuid.Nil {
		return err
	}
	var n int
	if err := s.Pool.QueryRow(fillCtx, `SELECT count(*) FROM playback_queue_items WHERE session_id=$1`, sid).Scan(&n); err != nil {
		return err
	}
	if !playback.ShouldReplenishAutoplay(autoplay, stopAfter, n-idx) {
		return nil
	}

	have, err := sessionQueueTrackIDs(fillCtx, s, sid)
	if err != nil {
		return err
	}
	exclude := append([]uuid.UUID{}, have...)
	exclude = append(exclude, seed)

	extra := s.autoplayLibraryTracks(fillCtx, userID, seed, exclude)
	if len(extra) > 0 {
		if err := s.addAutoplayTracks(fillCtx, sid, userID, extra); err != nil {
			return err
		}
		have = append(have, extra...)
	}

	n = len(have)
	if n == 0 {
		n = 1
	}
	if !playback.ShouldReplenishAutoplay(autoplay, stopAfter, n-idx) {
		return nil
	}
	ok, err := s.Play.HasAudioListener(fillCtx, sid)
	if err != nil || !ok {
		return nil
	}
	need := radio.ClampFill(8)
	hits := s.youtubeFillHits(fillCtx, seed, need, have)
	var refs []string
	seen := map[string]struct{}{}
	for _, h := range hits {
		if h.ID == "" {
			continue
		}
		if _, dup := seen[h.ID]; dup {
			continue
		}
		seen[h.ID] = struct{}{}
		refs = append(refs, h.ID)
	}
	if len(refs) == 0 {
		return nil
	}
	ids, err := s.resolvePlayTracks(fillCtx, refs)
	if err != nil || len(ids) == 0 {
		return err
	}
	var add []uuid.UUID
	known := map[uuid.UUID]struct{}{}
	for _, id := range have {
		known[id] = struct{}{}
	}
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		if _, dup := known[id]; dup {
			continue
		}
		known[id] = struct{}{}
		add = append(add, id)
	}
	return s.addAutoplayTracks(fillCtx, sid, userID, add)
}

func (s *Server) autoplayLibraryTracks(ctx context.Context, userID, seed uuid.UUID, exclude []uuid.UUID) []uuid.UUID {
	if s.autoplaySelectHook != nil {
		return s.autoplaySelectHook(ctx, seed, exclude)
	}
	u := s.autoplayUser(ctx, userID)
	libs := s.libraryIDs(ctx, u)
	if len(libs) == 0 && seed != uuid.Nil {
		var lib uuid.UUID
		_ = s.Pool.QueryRow(ctx, `SELECT library_id FROM tracks WHERE id=$1`, seed).Scan(&lib)
		if lib != uuid.Nil {
			libs = []uuid.UUID{lib}
		}
	}
	res, err := radio.New(s.Pool).Select(ctx, radio.Request{
		Kind:    "track",
		SeedID:  seed,
		Limit:   8,
		UserID:  userID,
		Libs:    libs,
		Exclude: exclude,
		Recent:  40,
	})
	if err != nil {
		return nil
	}
	var extra []uuid.UUID
	seen := map[uuid.UUID]struct{}{}
	for _, id := range exclude {
		seen[id] = struct{}{}
	}
	for _, id := range res.TrackIDs {
		if id == uuid.Nil {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		extra = append(extra, id)
	}
	return extra
}

func (s *Server) autoplayUser(ctx context.Context, userID uuid.UUID) *auth.User {
	u := &auth.User{ID: userID}
	if s.Auth != nil && userID != uuid.Nil {
		if got, err := s.Auth.GetUser(ctx, userID); err == nil && got != nil {
			return got
		}
	}
	return u
}

func (s *Server) addAutoplayTracks(ctx context.Context, sid, userID uuid.UUID, tracks []uuid.UUID) error {
	if len(tracks) == 0 {
		return nil
	}
	addCtx := playback.WithOrigin(ctx, playback.OriginAutoplay)
	if userID != uuid.Nil {
		addCtx = playback.WithRequester(addCtx, userID, "")
	}
	return s.Play.Add(addCtx, sid, tracks, false)
}

func sessionQueueTrackIDs(ctx context.Context, s *Server, sid uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.Pool.Query(ctx, `SELECT track_id FROM playback_queue_items WHERE session_id=$1 ORDER BY position`, sid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
