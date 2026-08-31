package merge

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrTrackInUse is returned when the loser is the current track of an active
// session (playing/paused/interrupted) or a Discord decoder. HTTP maps this to 409.
var ErrTrackInUse = errors.New("track_in_use")

// ErrStorageMismatch is returned when two libraries do not share a storage provider.
var ErrStorageMismatch = errors.New("libraries must share the same storage to merge without reimporting")

type querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Tracks remaps loser → winner, then deletes loser. Winner keeps its identity.
// History is updated before delete so listen_history / play_counts are not lost
// to ON DELETE CASCADE.
func Tracks(ctx context.Context, pool *pgxpool.Pool, winnerID, loserID uuid.UUID) error {
	if pool == nil {
		return errors.New("merge: no database")
	}
	if winnerID == uuid.Nil || loserID == uuid.Nil {
		return errors.New("merge: winner and loser are required")
	}
	if winnerID == loserID {
		return nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := lockTracks(ctx, tx, winnerID, loserID); err != nil {
		return err
	}

	inUse, err := trackInUse(ctx, tx, loserID)
	if err != nil {
		return err
	}
	if inUse {
		return ErrTrackInUse
	}

	if err := remapLoser(ctx, tx, winnerID, loserID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM tracks WHERE id=$1`, loserID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func lockTracks(ctx context.Context, tx pgx.Tx, winner, loser uuid.UUID) error {
	a, b := winner, loser
	if bytesLess(b, a) {
		a, b = b, a
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM tracks WHERE id=$1 FOR UPDATE`, a).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("merge: track not found")
		}
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT id FROM tracks WHERE id=$1 FOR UPDATE`, b).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("merge: track not found")
		}
		return err
	}
	return nil
}

func bytesLess(a, b uuid.UUID) bool {
	for i := 0; i < 16; i++ {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return false
}

func trackInUse(ctx context.Context, q querier, loser uuid.UUID) (bool, error) {
	var used bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM playback_sessions
			WHERE current_track_id=$1::uuid
			  AND (
				status IN ('playing', 'paused', 'interrupted')
				OR renderer_kind='discord'
			  )
		)`, loser).Scan(&used)
	if err != nil {
		return false, err
	}
	if used {
		return true, nil
	}
	err = q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM discord_voice_runtime r
			JOIN playback_sessions s ON s.id = r.session_id
			WHERE s.current_track_id=$1::uuid
		)`, loser).Scan(&used)
	if err != nil {
		if missingTable(err) {
			return false, nil
		}
		return false, err
	}
	return used, nil
}

func remapLoser(ctx context.Context, q querier, winner, loser uuid.UUID) error {
	if err := remapListenHistory(ctx, q, winner, loser); err != nil {
		return err
	}
	if err := remapListenEvents(ctx, q, winner, loser); err != nil {
		return err
	}
	if err := remapPlayCounts(ctx, q, winner, loser); err != nil {
		return err
	}
	if err := remapPlaylistEntries(ctx, q, winner, loser); err != nil {
		return err
	}
	if err := remapPersonalLibraryEntries(ctx, q, winner, loser); err != nil {
		return err
	}
	if err := remapFavourites(ctx, q, winner, loser); err != nil {
		return err
	}
	if err := execRequired(ctx, q, `UPDATE playback_queue_items SET track_id=$1 WHERE track_id=$2`, winner, loser); err != nil {
		return err
	}
	if err := execRequired(ctx, q, `UPDATE playback_sessions SET current_track_id=$1 WHERE current_track_id=$2`, winner, loser); err != nil {
		return err
	}
	if err := execOptional(ctx, q, `UPDATE acquisition_intents SET track_id=$1 WHERE track_id=$2`, winner, loser); err != nil {
		return err
	}
	if err := execOptional(ctx, q, `UPDATE track_sources SET track_id=$1 WHERE track_id=$2`, winner, loser); err != nil {
		return err
	}
	if err := remapLyrics(ctx, q, winner, loser); err != nil {
		return err
	}
	if err := remapMetadataLocks(ctx, q, winner, loser); err != nil {
		return err
	}
	if err := execOptional(ctx, q, `UPDATE track_fingerprints SET track_id=$1 WHERE track_id=$2`, winner, loser); err != nil {
		return err
	}
	if err := remapRetentionExclusions(ctx, q, winner, loser); err != nil {
		return err
	}
	if err := execRequired(ctx, q, `
		UPDATE artwork_assets SET owner_id=$1 WHERE owner_type='track' AND owner_id=$2`, winner, loser); err != nil {
		return err
	}
	if err := execOptional(ctx, q, `UPDATE external_track_mappings SET sounddock_track_id=$1 WHERE sounddock_track_id=$2`, winner, loser); err != nil {
		return err
	}
	if err := execOptional(ctx, q, `UPDATE external_playlist_items SET mapped_track_id=$1 WHERE mapped_track_id=$2`, winner, loser); err != nil {
		return err
	}
	if err := remapListenInstanceState(ctx, q, winner, loser); err != nil {
		return err
	}
	if err := execOptional(ctx, q, `UPDATE party_votes SET track_id=$1 WHERE track_id=$2`, winner, loser); err != nil {
		return err
	}
	if err := execOptional(ctx, q, `UPDATE scrobble_listen_state SET track_id=$1 WHERE track_id=$2`, winner, loser); err != nil {
		return err
	}
	if err := execOptional(ctx, q, `UPDATE retention_events SET track_id=$1 WHERE track_id=$2`, winner, loser); err != nil {
		return err
	}
	return nil
}

func remapListenHistory(ctx context.Context, q querier, winner, loser uuid.UUID) error {
	return execRequired(ctx, q, `UPDATE listen_history SET track_id=$1 WHERE track_id=$2`, winner, loser)
}

func remapListenEvents(ctx context.Context, q querier, winner, loser uuid.UUID) error {
	if err := execOptional(ctx, q, `
		DELETE FROM listen_events e
		WHERE e.track_id=$2::uuid
		  AND e.playback_instance_id IS NOT NULL
		  AND EXISTS (
			SELECT 1 FROM listen_events w
			WHERE w.track_id=$1::uuid
			  AND w.playback_instance_id = e.playback_instance_id
			  AND w.user_id = e.user_id
			  AND w.kind = e.kind
		  )`, winner, loser); err != nil {
		return err
	}
	return execOptional(ctx, q, `UPDATE listen_events SET track_id=$1 WHERE track_id=$2`, winner, loser)
}

func remapPlayCounts(ctx context.Context, q querier, winner, loser uuid.UUID) error {
	if err := execRequired(ctx, q, `
		INSERT INTO play_counts (user_id, track_id, count, skip_count, last_played_at)
		SELECT user_id, $1::uuid, count, skip_count, last_played_at
		FROM play_counts WHERE track_id=$2::uuid
		ON CONFLICT (user_id, track_id) DO UPDATE SET
			count = play_counts.count + EXCLUDED.count,
			skip_count = play_counts.skip_count + EXCLUDED.skip_count,
			last_played_at = COALESCE(
				GREATEST(play_counts.last_played_at, EXCLUDED.last_played_at),
				play_counts.last_played_at,
				EXCLUDED.last_played_at
			)`, winner, loser); err != nil {
		return err
	}
	return execRequired(ctx, q, `DELETE FROM play_counts WHERE track_id=$1`, loser)
}

func remapPlaylistEntries(ctx context.Context, q querier, winner, loser uuid.UUID) error {
	if err := execRequired(ctx, q, `
		DELETE FROM playlist_entries pe
		WHERE pe.track_id=$2::uuid
		  AND EXISTS (
			SELECT 1 FROM playlist_entries x
			WHERE x.playlist_id = pe.playlist_id AND x.track_id=$1::uuid
		  )`, winner, loser); err != nil {
		return err
	}
	return execRequired(ctx, q, `UPDATE playlist_entries SET track_id=$1 WHERE track_id=$2`, winner, loser)
}

func remapPersonalLibraryEntries(ctx context.Context, q querier, winner, loser uuid.UUID) error {
	if err := execOptional(ctx, q, `
		DELETE FROM personal_library_entries pe
		WHERE pe.track_id=$2
		  AND EXISTS (
			SELECT 1 FROM personal_library_entries x
			WHERE x.owner_id = pe.owner_id AND x.track_id=$1
		  )`, winner, loser); err != nil {
		return err
	}
	return execOptional(ctx, q, `UPDATE personal_library_entries SET track_id=$1 WHERE track_id=$2`, winner, loser)
}

func remapFavourites(ctx context.Context, q querier, winner, loser uuid.UUID) error {
	if err := execRequired(ctx, q, `
		DELETE FROM favourites f
		WHERE f.entity_type='track' AND f.entity_id=$2
		  AND EXISTS (
			SELECT 1 FROM favourites x
			WHERE x.user_id = f.user_id AND x.entity_type='track' AND x.entity_id=$1::uuid
		  )`, winner, loser); err != nil {
		return err
	}
	return execRequired(ctx, q, `
		UPDATE favourites SET entity_id=$1
		WHERE entity_type='track' AND entity_id=$2`, winner, loser)
}

func remapLyrics(ctx context.Context, q querier, winner, loser uuid.UUID) error {
	if err := execRequired(ctx, q, `
		DELETE FROM lyrics l
		WHERE l.track_id=$2
		  AND EXISTS (
			SELECT 1 FROM lyrics w WHERE w.track_id=$1::uuid AND w.source = l.source
		  )`, winner, loser); err != nil {
		return err
	}
	return execRequired(ctx, q, `UPDATE lyrics SET track_id=$1 WHERE track_id=$2`, winner, loser)
}

func remapMetadataLocks(ctx context.Context, q querier, winner, loser uuid.UUID) error {
	if err := execRequired(ctx, q, `
		DELETE FROM metadata_locks m
		WHERE m.entity_type='track' AND m.entity_id=$2
		  AND EXISTS (
			SELECT 1 FROM metadata_locks w
			WHERE w.entity_type='track' AND w.entity_id=$1 AND w.field = m.field
		  )`, winner, loser); err != nil {
		return err
	}
	return execRequired(ctx, q, `
		UPDATE metadata_locks SET entity_id=$1
		WHERE entity_type='track' AND entity_id=$2`, winner, loser)
}

func remapRetentionExclusions(ctx context.Context, q querier, winner, loser uuid.UUID) error {
	if err := execOptional(ctx, q, `
		DELETE FROM retention_exclusions e
		WHERE e.kind='track' AND e.target_id=$2
		  AND EXISTS (
			SELECT 1 FROM retention_exclusions w
			WHERE w.kind='track' AND w.target_id=$1
		  )`, winner, loser); err != nil {
		return err
	}
	return execOptional(ctx, q, `
		UPDATE retention_exclusions SET target_id=$1
		WHERE kind='track' AND target_id=$2`, winner, loser)
}

func remapListenInstanceState(ctx context.Context, q querier, winner, loser uuid.UUID) error {
	if err := execOptional(ctx, q, `
		DELETE FROM listen_instance_state s
		WHERE s.track_id=$2::uuid
		  AND EXISTS (
			SELECT 1 FROM listen_instance_state w
			WHERE w.playback_instance_id = s.playback_instance_id AND w.user_id = s.user_id
			  AND w.track_id=$1::uuid
		  )`, winner, loser); err != nil {
		return err
	}
	return execOptional(ctx, q, `UPDATE listen_instance_state SET track_id=$1 WHERE track_id=$2`, winner, loser)
}

func execRequired(ctx context.Context, q querier, sql string, args ...any) error {
	_, err := q.Exec(ctx, sql, args...)
	return err
}

func execOptional(ctx context.Context, q querier, sql string, args ...any) error {
	_, err := q.Exec(ctx, sql, args...)
	if err != nil && missingTable(err) {
		return nil
	}
	return err
}

func missingTable(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "42P01"
}
