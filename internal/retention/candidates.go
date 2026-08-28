package retention

import (
	"context"
	"time"

	"github.com/google/uuid"
)

const candidateSQL = `
WITH plays AS (
  SELECT track_id, SUM(count)::int AS plays, MAX(last_played_at) AS last_played
  FROM play_counts GROUP BY track_id
), hist AS (
  SELECT track_id, MAX(played_at) AS last_hist FROM listen_history GROUP BY track_id
), sizes AS (
  SELECT track_id, coalesce(sum(size_bytes),0) AS size_bytes
  FROM track_files
  WHERE deleted_at IS NULL
  GROUP BY track_id
)
SELECT
  t.id, t.title,
  coalesce((
    SELECT string_agg(a.name, ', ' ORDER BY ta.position)
    FROM track_artists ta JOIN artists a ON a.id = ta.artist_id
    WHERE ta.track_id = t.id AND ta.role = 'primary'
  ), ''),
  s.size_bytes AS size_bytes,
  coalesce(p.plays, 0),
  GREATEST(p.last_played, h.last_hist) AS last_activity,
  t.acquisition, t.acquisition_ref, t.created_at,
  (
    $6::int > 0 AND (
      CASE
        WHEN coalesce(p.plays, 0) = 0 AND GREATEST(p.last_played, h.last_hist) IS NULL
          THEN t.created_at < now() - make_interval(days => $6)
        ELSE coalesce(GREATEST(p.last_played, h.last_hist), t.created_at) < now() - make_interval(days => $6)
      END
    )
  ) AS age_eligible
FROM tracks t
JOIN libraries l ON l.id = t.library_id
JOIN storage_providers sp ON sp.id = l.storage_provider_id
JOIN sizes s ON s.track_id = t.id
LEFT JOIN plays p ON p.track_id = t.id
LEFT JOIN hist h ON h.track_id = t.id
WHERE t.media_unavailable_at IS NULL
  AND t.keep_forever = FALSE
  AND s.size_bytes > 0
  AND (
    l.retention_opt_in
    OR (sp.type = 'managed' AND NOT l.read_only AND t.acquisition IN ('youtube', 'url', 'scapex'))
  )
  AND NOT EXISTS (SELECT 1 FROM favourites f WHERE f.entity_type='track' AND f.entity_id=t.id)
  AND NOT EXISTS (SELECT 1 FROM favourites f WHERE f.entity_type='album' AND t.album_id IS NOT NULL AND f.entity_id=t.album_id)
  AND NOT EXISTS (
    SELECT 1 FROM favourites f
    JOIN track_artists ta ON ta.artist_id = f.entity_id
    WHERE f.entity_type='artist' AND ta.track_id=t.id
  )
  AND NOT EXISTS (
    SELECT 1 FROM playlist_entries pe
    JOIN playlists pl ON pl.id = pe.playlist_id
    WHERE pe.track_id = t.id
      AND NOT EXISTS (SELECT 1 FROM external_playlists ep WHERE ep.sounddock_playlist_id = pl.id)
      AND NOT EXISTS (SELECT 1 FROM smart_playlist_rules sm WHERE sm.playlist_id = pl.id)
  )
  AND NOT EXISTS (SELECT 1 FROM retention_exclusions e WHERE e.kind='track' AND e.target_id=t.id)
  AND NOT EXISTS (SELECT 1 FROM retention_exclusions e WHERE e.kind='album' AND t.album_id IS NOT NULL AND e.target_id=t.album_id)
  AND NOT EXISTS (SELECT 1 FROM retention_exclusions e WHERE e.kind='library' AND e.target_id=t.library_id)
  AND NOT EXISTS (
    SELECT 1 FROM retention_exclusions e
    JOIN track_artists ta ON ta.artist_id = e.target_id
    WHERE e.kind='artist' AND ta.track_id=t.id
  )
  AND NOT EXISTS (
    SELECT 1 FROM retention_exclusions e
    JOIN playlist_entries pe ON pe.playlist_id = e.target_id
    WHERE e.kind='playlist' AND pe.track_id=t.id
  )
  AND t.id <> ALL($1::uuid[])
  AND t.library_id <> ALL($2::uuid[])
  AND t.id NOT IN (SELECT track_id FROM playback_queue_items)
  AND t.id NOT IN (SELECT current_track_id FROM playback_sessions WHERE current_track_id IS NOT NULL)
  AND ($3::int = 0 OR GREATEST(p.last_played, h.last_hist) IS NULL OR GREATEST(p.last_played, h.last_hist) < now() - make_interval(days => $3))
  AND ($4::int = 0 OR coalesce(p.plays, 0) < $4)
  AND (
    NOT $5::bool
    OR (
      $6::int > 0 AND (
        CASE
          WHEN coalesce(p.plays, 0) = 0 AND GREATEST(p.last_played, h.last_hist) IS NULL
            THEN t.created_at < now() - make_interval(days => $6)
          ELSE coalesce(GREATEST(p.last_played, h.last_hist), t.created_at) < now() - make_interval(days => $6)
        END
      )
    )
  )
`

func (e *Engine) pressure(ctx context.Context, policy Policy) (storage, freeSpace bool) {
	if policy.UsesStorage() {
		managed, _ := e.managedBytes(ctx)
		storage = managed >= policy.HighWater()
	}
	if policy.UsesFreeSpace() {
		_, free, _ := e.disk()
		freeSpace = free > 0 && free < policy.MinFreeBytes
	}
	return storage, freeSpace
}

func (e *Engine) requireAge(ctx context.Context, policy Policy) bool {
	st, fs := e.pressure(ctx, policy)
	switch policy.Mode {
	case ModeStorage, ModeFreeSpace:
		return false
	case ModeHybrid:
		return !st && !fs
	default:
		return policy.AgeDays > 0
	}
}

func (e *Engine) skipRun(ctx context.Context, policy Policy) bool {
	st, fs := e.pressure(ctx, policy)
	switch policy.Mode {
	case ModeStorage:
		return !st
	case ModeFreeSpace:
		return !fs
	default:
		return false
	}
}

func (e *Engine) candidateArgs(ctx context.Context, policy Policy, requireAge bool) []any {
	tracks, libs := e.busy(ctx)
	return []any{
		emptyUUID(tracks),
		emptyUUID(libs),
		policy.RecentPlayDays,
		policy.MinPlayCountProtect,
		requireAge,
		policy.AgeDays,
	}
}

func (e *Engine) candidates(ctx context.Context, policy Policy, limit int) ([]Candidate, error) {
	if e.skipRun(ctx, policy) {
		return nil, nil
	}
	if limit <= 0 {
		limit = policy.BatchSize
	}
	args := e.candidateArgs(ctx, policy, e.requireAge(ctx, policy))
	q := candidateSQL + `
ORDER BY
  CASE WHEN coalesce(p.plays, 0) = 0 AND GREATEST(p.last_played, h.last_hist) IS NULL THEN 0 ELSE 1 END,
  coalesce(p.plays, 0),
  COALESCE(GREATEST(p.last_played, h.last_hist), TIMESTAMPTZ 'epoch'),
  t.created_at
LIMIT $7`
	args = append(args, limit)
	rows, err := e.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var c Candidate
		if err := rows.Scan(
			&c.ID, &c.Title, &c.Artist, &c.SizeBytes, &c.PlayCount, &c.LastPlayed,
			&c.Acquisition, &c.AcquisitionRef, &c.CreatedAt, &c.AgeEligible,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (e *Engine) RecentEvents(ctx context.Context, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 40
	}
	rows, err := e.pool.Query(ctx, `
		SELECT e.id, e.track_id, e.title, e.artist, e.size_bytes, e.reason,
		       e.last_played_at, e.play_count, e.acquisition, e.acquisition_ref,
		       e.dry_run, e.created_at
		FROM retention_events e
		ORDER BY e.created_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var (
			id, trackID                                     uuid.UUID
			title, artist, reason, acq, ref                 string
			size                                            int64
			plays                                           int
			last                                            *time.Time
			dry                                             bool
			at                                              time.Time
		)
		if err := rows.Scan(&id, &trackID, &title, &artist, &size, &reason, &last, &plays, &acq, &ref, &dry, &at); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "track_id": trackID, "title": title, "artist": artist,
			"size_bytes": size, "reason": reason, "last_played_at": last,
			"play_count": plays, "acquisition": acq, "acquisition_ref": ref,
			"dry_run": dry, "created_at": at,
		})
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, nil
}
