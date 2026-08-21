package scrobble

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
	"github.com/sounddock/sounddock/internal/search"
)

type Service struct {
	pool   *pgxpool.Pool
	box    *cryptox.Box
	search *search.Engine
}

func New(pool *pgxpool.Pool, box *cryptox.Box, se *search.Engine) *Service {
	return &Service{pool: pool, box: box, search: se}
}

func EnsureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS scrobble_accounts (
			user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			lastfm_username TEXT NOT NULL DEFAULT '',
			lastfm_session_enc BYTEA,
			listenbrainz_username TEXT NOT NULL DEFAULT '',
			listenbrainz_token_enc BYTEA,
			presence_enabled BOOLEAN NOT NULL DEFAULT FALSE,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE IF NOT EXISTS scrobble_listen_state (
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			source TEXT NOT NULL,
			track_id UUID NOT NULL,
			counted BOOLEAN NOT NULL DEFAULT FALSE,
			max_position_ms INT NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (user_id, source)
		)`)
	return err
}

type Account struct {
	LastFMUsername        string
	ListenBrainzUsername  string
	PresenceEnabled       bool
	LastFMConnected       bool
	ListenBrainzConnected bool
}

func (s *Service) Account(ctx context.Context, userID uuid.UUID) (Account, error) {
	_ = EnsureSchema(ctx, s.pool)
	var a Account
	var lfSess, lbTok []byte
	err := s.pool.QueryRow(ctx, `
		SELECT lastfm_username, lastfm_session_enc, listenbrainz_username, listenbrainz_token_enc, presence_enabled
		FROM scrobble_accounts WHERE user_id=$1`, userID).
		Scan(&a.LastFMUsername, &lfSess, &a.ListenBrainzUsername, &lbTok, &a.PresenceEnabled)
	if err != nil {
		return Account{}, nil
	}
	a.LastFMConnected = len(lfSess) > 0 && a.LastFMUsername != ""
	a.ListenBrainzConnected = len(lbTok) > 0
	return a, nil
}
