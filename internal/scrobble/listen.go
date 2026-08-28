package scrobble

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/listen"
)

func (s *Service) HandleListen(ctx context.Context, userID uuid.UUID, ev Event) error {
	_ = EnsureSchema(ctx, s.pool)
	if ev.Source == "" {
		ev.Source = "web"
	}
	if ev.Source == "import" {
		return nil
	}
	if ev.DurationMS <= 0 && ev.TrackID != uuid.Nil {
		_ = s.pool.QueryRow(ctx, `SELECT duration_ms FROM tracks WHERE id=$1`, ev.TrackID).Scan(&ev.DurationMS)
	}
	prev := s.loadState(ctx, userID, ev.Source)
	next, out := Eval(prev, ev)
	_, _ = s.pool.Exec(ctx, `
		INSERT INTO scrobble_listen_state (user_id, source, track_id, counted, max_position_ms, updated_at)
		VALUES ($1,$2,$3,$4,$5,now())
		ON CONFLICT (user_id, source) DO UPDATE SET
			track_id=EXCLUDED.track_id, counted=EXCLUDED.counted, max_position_ms=EXCLUDED.max_position_ms, updated_at=now()`,
		userID, ev.Source, next.TrackID, next.Counted, next.MaxPositionMS)
	_ = listen.ApplyShadow(ctx, s.pool, userID, listen.Checkpoint{
		TrackID: ev.TrackID, PositionMS: ev.PositionMS, DurationMS: ev.DurationMS,
		Source: ev.Source, Kind: ev.Kind, StopAfter: ev.StopAfter,
		PlaybackInstanceID: ev.PlaybackInstanceID, PlayheadSequence: ev.PlayheadSequence,
		ClientID: ev.ClientID, DeviceID: ev.DeviceID, Status: ev.Status,
		PlaybackRate: ev.PlaybackRate, RendererKind: ev.RendererKind, RendererID: ev.RendererID,
		AudioListener: ev.AudioListener, At: ev.At,
	})
	if out.CountSkip {
		_, _ = s.pool.Exec(ctx, `
			INSERT INTO play_counts (user_id, track_id, count, skip_count) VALUES ($1,$2,0,1)
			ON CONFLICT (user_id, track_id) DO UPDATE SET skip_count=play_counts.skip_count+1`,
			userID, ev.TrackID)
		return nil
	}
	if out.CountPlay && out.InsertHistory {
		if err := s.InsertHistory(ctx, userID, ev.TrackID, ev.DurationMS, ev.Source); err != nil {
			return err
		}
		s.scrobbleNow(ctx, userID, ev)
	} else if ev.Kind == "progress" && ev.PositionMS > 0 {
		s.updateNowPlaying(ctx, userID, ev)
	}
	return nil
}

func (s *Service) loadState(ctx context.Context, userID uuid.UUID, source string) State {
	var st State
	_ = s.pool.QueryRow(ctx, `SELECT track_id, counted, max_position_ms FROM scrobble_listen_state WHERE user_id=$1 AND source=$2`,
		userID, source).Scan(&st.TrackID, &st.Counted, &st.MaxPositionMS)
	return st
}

func (s *Service) InsertHistory(ctx context.Context, userID, trackID uuid.UUID, durationMS int, source string) error {
	if source != "web" && source != "discord" && source != "import" {
		source = "web"
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO listen_history (user_id, track_id, played_at, duration_ms, source)
		VALUES ($1,$2,now(),$3,$4)`, userID, trackID, durationMS, source)
	if err != nil {
		return err
	}
	if source == "import" {
		return nil
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO play_counts (user_id, track_id, count, last_played_at) VALUES ($1,$2,1,now())
		ON CONFLICT (user_id, track_id) DO UPDATE SET count=play_counts.count+1, last_played_at=now()`,
		userID, trackID)
	return err
}

func (s *Service) InsertImported(ctx context.Context, userID, trackID uuid.UUID, playedAt time.Time, durationMS int) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO listen_history (user_id, track_id, played_at, duration_ms, source)
		VALUES ($1,$2,$3,$4,'import')`, userID, trackID, playedAt, durationMS)
	return err
}

func (s *Service) trackMeta(ctx context.Context, id uuid.UUID) (title, artist, album string) {
	_ = s.pool.QueryRow(ctx, `
		SELECT t.title,
			coalesce((SELECT string_agg(ar.name, ', ' ORDER BY ta.position)
				FROM track_artists ta JOIN artists ar ON ar.id=ta.artist_id
				WHERE ta.track_id=t.id AND ta.role='primary'),''),
			coalesce(al.title,'')
		FROM tracks t LEFT JOIN albums al ON al.id=t.album_id WHERE t.id=$1`, id).Scan(&title, &artist, &album)
	return
}

func (s *Service) scrobbleNow(ctx context.Context, userID uuid.UUID, ev Event) {
	title, artist, album := s.trackMeta(ctx, ev.TrackID)
	if title == "" {
		return
	}
	_ = s.submitLastFM(ctx, userID, title, artist, album, ev.DurationMS, true)
	_ = s.submitListenBrainz(ctx, userID, title, artist, album, time.Now().Unix(), false)
}

func (s *Service) updateNowPlaying(ctx context.Context, userID uuid.UUID, ev Event) {
	title, artist, album := s.trackMeta(ctx, ev.TrackID)
	if title == "" {
		return
	}
	_ = s.submitLastFM(ctx, userID, title, artist, album, ev.DurationMS, false)
	_ = s.submitListenBrainz(ctx, userID, title, artist, album, 0, true)
}
