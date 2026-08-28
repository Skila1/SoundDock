package retention

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

func applyLogPolicies(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `SELECT key, days FROM retention_policies`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var days int
		if err := rows.Scan(&key, &days); err != nil {
			continue
		}
		if days <= 0 {
			continue
		}
		switch key {
		case "listen_history":
			_, _ = pool.Exec(ctx, `DELETE FROM listen_history WHERE played_at < now() - make_interval(days => $1)`, days)
		case "failed_jobs":
			_, _ = pool.Exec(ctx, `DELETE FROM jobs WHERE status='failed' AND updated_at < now() - make_interval(days => $1)`, days)
		case "discord_playback_errors":
			_, _ = pool.Exec(ctx, `DELETE FROM discord_playback_errors WHERE created_at < now() - make_interval(days => $1)`, days)
		case "api_usage":
			_, _ = pool.Exec(ctx, `DELETE FROM api_usage_aggregates WHERE bucket < now() - make_interval(days => $1)`, days)
		case "audit_events":
			_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE created_at < now() - make_interval(days => $1)`, days)
		case "operational_logs":
			// Seeded policy key with no table and no delete target.
		}
	}
	_, _ = pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`)
	_, _ = pool.Exec(ctx, `DELETE FROM identity_link_challenges WHERE expires_at < now()`)
	return nil
}
