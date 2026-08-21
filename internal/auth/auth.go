package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
	"golang.org/x/crypto/argon2"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrDisabled           = errors.New("account disabled")
	ErrSetupComplete      = errors.New("setup already complete")
)

type User struct {
	ID               uuid.UUID `json:"id"`
	Username         string    `json:"username"`
	Email            *string   `json:"email,omitempty"`
	DisplayName      string    `json:"display_name"`
	Disabled         bool      `json:"disabled"`
	ReplayGainMode   string    `json:"replaygain_mode"`
	CrossfadeSeconds int       `json:"crossfade_seconds"`
	TargetLUFS       float64   `json:"target_lufs"`
	Permissions      []string  `json:"permissions"`
	IsAdmin          bool      `json:"is_admin"`
}

type Session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	ExpiresAt time.Time
}

type Service struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return fmt.Sprintf("argon2id$v=19$m=65536,t=3,p=2$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(want, got) == 1
}

func (s *Service) SetupNeeded(ctx context.Context) (bool, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return false, err
	}
	return n == 0, nil
}

func (s *Service) CreateAdmin(ctx context.Context, username, password, email string) (*User, error) {
	needed, err := s.SetupNeeded(ctx)
	if err != nil {
		return nil, err
	}
	if !needed {
		return nil, ErrSetupComplete
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	err = s.pool.QueryRow(ctx, `
		INSERT INTO users (username, email, password_hash, display_name)
		VALUES ($1, NULLIF($2,''), $3, $1) RETURNING id`, username, email, hash).Scan(&id)
	if err != nil {
		return nil, err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO user_roles (user_id, role_id)
		SELECT $1, id FROM roles WHERE name='Administrator'`, id)
	if err != nil {
		return nil, err
	}
	_, _ = s.pool.Exec(ctx, `UPDATE server_settings SET value='true'::jsonb WHERE key='setup_complete'`)
	return s.GetUser(ctx, id)
}

func (s *Service) Authenticate(ctx context.Context, identifier, password string) (*User, error) {
	var id uuid.UUID
	var hash string
	var disabled bool
	err := s.pool.QueryRow(ctx, `
		SELECT id, password_hash, disabled FROM users
		WHERE lower(username)=lower($1) OR lower(coalesce(email,''))=lower($1)`, identifier).
		Scan(&id, &hash, &disabled)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if disabled {
		return nil, ErrDisabled
	}
	if !VerifyPassword(hash, password) {
		return nil, ErrInvalidCredentials
	}
	return s.GetUser(ctx, id)
}

func (s *Service) GetUser(ctx context.Context, id uuid.UUID) (*User, error) {
	u := &User{ID: id}
	err := s.pool.QueryRow(ctx, `
		SELECT username, email, display_name, disabled, replaygain_mode, crossfade_seconds, target_lufs
		FROM users WHERE id=$1`, id).
		Scan(&u.Username, &u.Email, &u.DisplayName, &u.Disabled, &u.ReplayGainMode, &u.CrossfadeSeconds, &u.TargetLUFS)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT p.name FROM permissions p
		JOIN role_permissions rp ON rp.permission_id=p.id
		JOIN user_roles ur ON ur.role_id=rp.role_id
		WHERE ur.user_id=$1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		u.Permissions = append(u.Permissions, n)
		if n == "admin" {
			u.IsAdmin = true
		}
	}
	return u, rows.Err()
}

func (s *Service) CreateSession(ctx context.Context, userID uuid.UUID, ua, ip string, ttl time.Duration) (plain string, sess Session, err error) {
	plain, err = cryptox.RandomToken(32)
	if err != nil {
		return "", Session{}, err
	}
	hash := cryptox.HashToken(plain)
	exp := time.Now().Add(ttl)
	err = s.pool.QueryRow(ctx, `
		INSERT INTO sessions (user_id, token_hash, user_agent, ip, expires_at)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`, userID, hash, ua, ip, exp).Scan(&sess.ID)
	if err != nil {
		return "", Session{}, err
	}
	sess.UserID = userID
	sess.ExpiresAt = exp
	return plain, sess, nil
}

func (s *Service) SessionUser(ctx context.Context, token string) (*User, uuid.UUID, error) {
	hash := cryptox.HashToken(token)
	var uid, sid uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id FROM sessions WHERE token_hash=$1 AND expires_at>now()`, hash).Scan(&sid, &uid)
	if err != nil {
		return nil, uuid.Nil, err
	}
	_, _ = s.pool.Exec(ctx, `UPDATE sessions SET last_seen_at=now() WHERE id=$1`, sid)
	u, err := s.GetUser(ctx, uid)
	return u, sid, err
}

func (s *Service) DeleteSession(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE id=$1`, id)
	return err
}

func (s *Service) DeleteUserSessions(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, userID)
	return err
}

func (s *Service) ListSessions(ctx context.Context, userID uuid.UUID) ([]map[string]any, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, user_agent, ip, created_at, last_seen_at, expires_at FROM sessions WHERE user_id=$1 ORDER BY last_seen_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var ua, ip *string
		var created, seen, exp time.Time
		if err := rows.Scan(&id, &ua, &ip, &created, &seen, &exp); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "user_agent": ua, "ip": ip, "created_at": created, "last_seen_at": seen, "expires_at": exp})
	}
	return out, rows.Err()
}

func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, current, next string) error {
	var hash string
	if err := s.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, userID).Scan(&hash); err != nil {
		return err
	}
	if !VerifyPassword(hash, current) {
		return ErrInvalidCredentials
	}
	nh, err := HashPassword(next)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE users SET password_hash=$1, updated_at=now() WHERE id=$2`, nh, userID)
	return err
}

func (s *Service) UpdatePrefs(ctx context.Context, userID uuid.UUID, rg string, crossfade int, lufs float64) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET replaygain_mode=$1, crossfade_seconds=$2, target_lufs=$3, updated_at=now() WHERE id=$4`,
		rg, crossfade, lufs, userID)
	return err
}

func HasPerm(u *User, name string) bool {
	if u == nil {
		return false
	}
	if u.IsAdmin {
		return true
	}
	for _, p := range u.Permissions {
		if p == name {
			return true
		}
	}
	return false
}
