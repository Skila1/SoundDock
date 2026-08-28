package external

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func trackPlayable(ctx context.Context, q execer, trackID uuid.UUID) bool {
	if trackID == uuid.Nil {
		return false
	}
	var n int
	_ = q.QueryRow(ctx, `
		SELECT 1 FROM track_files
		WHERE track_id=$1 AND quality='original' AND deleted_at IS NULL
		LIMIT 1`, trackID).Scan(&n)
	return n == 1
}

func mappedPlayable(ctx context.Context, q execer, provider, providerTrackID string) uuid.UUID {
	var id uuid.UUID
	_ = q.QueryRow(ctx, `
		SELECT m.sounddock_track_id
		FROM external_track_mappings m
		JOIN track_files tf ON tf.track_id=m.sounddock_track_id AND tf.quality='original' AND tf.deleted_at IS NULL
		WHERE m.provider=$1 AND m.provider_track_id=$2
		LIMIT 1`, provider, providerTrackID).Scan(&id)
	return id
}

func snapshotUnchanged(prevSnap, prevStatus, snap string) bool {
	return snap != "" && prevSnap == snap && prevStatus == "ok"
}

func retainedSnapshot(prev, incoming string) string {
	if incoming != "" {
		return incoming
	}
	return prev
}

func keepMembership(id uuid.UUID, playable bool) (uuid.UUID, bool) {
	if id == uuid.Nil || !playable {
		return uuid.Nil, false
	}
	return id, true
}

func sameUUIDs(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func loadPlaylistTrackIDs(ctx context.Context, q execer, playlistID uuid.UUID) ([]uuid.UUID, []uuid.UUID, error) {
	rows, err := q.Query(ctx, `
		SELECT id, track_id FROM playlist_entries WHERE playlist_id=$1 ORDER BY position, added_at`, playlistID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var rowIDs, tracks []uuid.UUID
	for rows.Next() {
		var rid, tid uuid.UUID
		if err := rows.Scan(&rid, &tid); err != nil {
			return nil, nil, err
		}
		rowIDs = append(rowIDs, rid)
		tracks = append(tracks, tid)
	}
	return rowIDs, tracks, rows.Err()
}

// reconcilePlaylistEntries updates membership without a full delete/reinsert when
// the ordered track list is unchanged. Never touches track_files / media.
func reconcilePlaylistEntries(ctx context.Context, q execer, playlistID uuid.UUID, desired []uuid.UUID, removal string) error {
	_, current, err := loadPlaylistTrackIDs(ctx, q, playlistID)
	if err != nil {
		return err
	}
	if removal != "mirror" && removal != "once" {
		var max int
		_ = q.QueryRow(ctx, `SELECT coalesce(max(position),-1) FROM playlist_entries WHERE playlist_id=$1`, playlistID).Scan(&max)
		seen := map[uuid.UUID]struct{}{}
		for _, id := range current {
			seen[id] = struct{}{}
		}
		for _, tid := range desired {
			if tid == uuid.Nil {
				continue
			}
			if _, ok := seen[tid]; ok {
				continue
			}
			max++
			if _, err := q.Exec(ctx, `INSERT INTO playlist_entries (playlist_id, track_id, position) VALUES ($1,$2,$3)`, playlistID, tid, max); err != nil {
				return err
			}
			seen[tid] = struct{}{}
		}
		return nil
	}
	if sameUUIDs(current, desired) {
		return nil
	}
	keep := 0
	for keep < len(current) && keep < len(desired) && current[keep] == desired[keep] {
		keep++
	}
	if _, err := q.Exec(ctx, `DELETE FROM playlist_entries WHERE playlist_id=$1 AND position>=$2`, playlistID, keep); err != nil {
		return err
	}
	for i := keep; i < len(desired); i++ {
		if desired[i] == uuid.Nil {
			continue
		}
		if _, err := q.Exec(ctx, `INSERT INTO playlist_entries (playlist_id, track_id, position) VALUES ($1,$2,$3)`, playlistID, desired[i], i); err != nil {
			return err
		}
	}
	return nil
}
