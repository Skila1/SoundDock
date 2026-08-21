package identity

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
)

type Service struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

func (s *Service) Challenge(ctx context.Context, userID uuid.UUID, provider string) (string, error) {
	tok, err := cryptox.RandomToken(24)
	if err != nil {
		return "", err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO identity_link_challenges (token_hash, provider, user_id, expires_at) VALUES ($1,$2,$3,$4)`,
		cryptox.HashToken(tok), provider, userID, time.Now().Add(10*time.Minute))
	return tok, err
}
