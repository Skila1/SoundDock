package discordx

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

func ensureRuntimeSchema(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS discord_user_voice (
			discord_user_id TEXT NOT NULL,
			guild_id TEXT NOT NULL,
			channel_id TEXT,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (discord_user_id, guild_id)
		)`)
	return err
}
