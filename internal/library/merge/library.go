package merge

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LibraryInto moves src into dest. Same-storage only. Content-hash duplicates
// are merged with Tracks (dest track wins) so listen_history is remapped
// instead of cascading away.
func LibraryInto(ctx context.Context, pool *pgxpool.Pool, src, dest uuid.UUID) (int, error) {
	if pool == nil {
		return 0, errors.New("merge: no database")
	}
	if src == dest {
		return 0, nil
	}

	var srcProv, destProv uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT storage_provider_id FROM libraries WHERE id=$1`, src).Scan(&srcProv); err != nil {
		return 0, err
	}
	if err := pool.QueryRow(ctx, `SELECT storage_provider_id FROM libraries WHERE id=$1`, dest).Scan(&destProv); err != nil {
		return 0, err
	}
	if srcProv != destProv {
		return 0, ErrStorageMismatch
	}

	pairs, err := hashDupePairs(ctx, pool, src, dest)
	if err != nil {
		return 0, err
	}
	for _, p := range pairs {
		inUse, err := trackInUse(ctx, pool, p.loser)
		if err != nil {
			return 0, err
		}
		if inUse {
			return 0, ErrTrackInUse
		}
	}
	for _, p := range pairs {
		if err := Tracks(ctx, pool, p.winner, p.loser); err != nil {
			return 0, err
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `UPDATE tracks SET library_id=$2 WHERE library_id=$1`, src, dest)
	if err != nil {
		return 0, err
	}
	moved := int(tag.RowsAffected())
	if _, err := tx.Exec(ctx, `
		UPDATE track_files tf SET library_id=$2
		WHERE library_id=$1
		  AND NOT EXISTS (
			SELECT 1 FROM track_files d WHERE d.library_id=$2 AND d.storage_key=tf.storage_key
		  )`, src, dest); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM track_files WHERE library_id=$1`, src); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE albums SET library_id=$2 WHERE library_id=$1`, src, dest); err != nil {
		return 0, err
	}

	var srcDefault bool
	_ = tx.QueryRow(ctx, `SELECT is_default FROM libraries WHERE id=$1`, src).Scan(&srcDefault)
	if _, err := tx.Exec(ctx, `DELETE FROM libraries WHERE id=$1`, src); err != nil {
		return 0, err
	}
	if srcDefault {
		if _, err := tx.Exec(ctx, `UPDATE libraries SET is_default=FALSE WHERE is_default`); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `UPDATE libraries SET is_default=TRUE WHERE id=$1`, dest); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return moved, nil
}

type hashPair struct {
	winner uuid.UUID
	loser  uuid.UUID
}

func hashDupePairs(ctx context.Context, pool *pgxpool.Pool, src, dest uuid.UUID) ([]hashPair, error) {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT ON (s.track_id) d.track_id, s.track_id
		FROM track_files s
		JOIN track_files d ON d.content_hash = s.content_hash
			AND d.library_id=$2 AND d.quality='original'
		WHERE s.library_id=$1 AND s.quality='original'
		  AND s.content_hash IS NOT NULL AND s.content_hash <> ''
		  AND d.track_id <> s.track_id
		ORDER BY s.track_id, d.track_id`, src, dest)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []hashPair
	for rows.Next() {
		var p hashPair
		if err := rows.Scan(&p.winner, &p.loser); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
