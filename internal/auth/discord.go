package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
)

type DiscordProfile struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Global   string `json:"global_name"`
	Avatar   string `json:"avatar"`
}

type DiscordOAuth struct {
	Profile     DiscordProfile
	AccessToken string
}

type DiscordRegistration struct {
	GuildEnabled bool
	GuildID      string
	RoleEnabled  bool
	RoleID       string
}

func DiscordUserExists(ctx context.Context, pool *pgxpool.Pool, discordID string) (bool, error) {
	var n int
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_identities WHERE provider='discord' AND provider_user_id=$1`, discordID).Scan(&n)
	return n > 0, err
}

func LoadDiscordRegistration(ctx context.Context, pool *pgxpool.Pool) (DiscordRegistration, error) {
	var r DiscordRegistration
	err := pool.QueryRow(ctx, `
		SELECT registration_guild_enabled, registration_guild_id, registration_role_enabled, registration_role_id
		FROM discord_settings WHERE id=1`).Scan(&r.GuildEnabled, &r.GuildID, &r.RoleEnabled, &r.RoleID)
	return r, err
}

func (r DiscordRegistration) NeedGuildsScope() bool {
	return r.GuildEnabled || r.RoleEnabled
}

func (r DiscordRegistration) NeedRoleScope() bool {
	return r.RoleEnabled
}

func guildIDsContain(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func (s *Service) UpsertDiscordUser(ctx context.Context, p DiscordProfile, adminIDs []string) (*User, error) {
	display := p.Global
	if display == "" {
		display = p.Username
	}
	uname := "discord_" + p.ID
	var uid uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT user_id FROM user_identities WHERE provider='discord' AND provider_user_id=$1`, p.ID).Scan(&uid)
	if err != nil {
		hash, herr := HashPassword(uuid.NewString() + uuid.NewString())
		if herr != nil {
			return nil, herr
		}
		err = s.pool.QueryRow(ctx, `
			INSERT INTO users (username, password_hash, display_name)
			VALUES ($1,$2,$3)
			ON CONFLICT (username) DO UPDATE SET display_name=EXCLUDED.display_name
			RETURNING id`, uname, hash, display).Scan(&uid)
		if err != nil {
			return nil, err
		}
		_, _ = s.pool.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE name='User' ON CONFLICT DO NOTHING`, uid)
		_, _ = s.pool.Exec(ctx, `INSERT INTO user_identities (user_id, provider, provider_user_id, provider_username) VALUES ($1,'discord',$2,$3)
			ON CONFLICT (provider, provider_user_id) DO UPDATE SET user_id=EXCLUDED.user_id, provider_username=EXCLUDED.provider_username`, uid, p.ID, p.Username)
	} else {
		_, _ = s.pool.Exec(ctx, `UPDATE users SET display_name=$2, updated_at=now() WHERE id=$1 AND display_name=''`, uid, display)
		_, _ = s.pool.Exec(ctx, `UPDATE user_identities SET provider_username=$2 WHERE provider='discord' AND provider_user_id=$1`, p.ID, p.Username)
	}
	if IsAdminDiscordID(p.ID, adminIDs) {
		_, _ = s.pool.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE name='Administrator' ON CONFLICT DO NOTHING`, uid)
	}
	return s.GetUser(ctx, uid)
}

func ApplyAdminDiscordIDs(ctx context.Context, pool *pgxpool.Pool, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO user_roles (user_id, role_id)
		SELECT i.user_id, r.id
		FROM user_identities i
		CROSS JOIN roles r
		WHERE i.provider='discord' AND i.provider_user_id = ANY($1) AND r.name='Administrator'
		ON CONFLICT DO NOTHING`, ids)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		DELETE FROM user_roles ur
		USING roles r
		WHERE ur.role_id = r.id AND r.name='Administrator'
		  AND NOT EXISTS (
		    SELECT 1 FROM user_identities i
		    WHERE i.user_id = ur.user_id AND i.provider='discord' AND i.provider_user_id = ANY($1)
		  )`, ids)
	return err
}

func IsAdminDiscordID(id string, ids []string) bool {
	for _, a := range ids {
		if a == id {
			return true
		}
	}
	return false
}

func DiscordAuthURL(clientID, redirect, state, challenge, scope string) string {
	if scope == "" {
		scope = "identify"
	}
	q := url.Values{
		"client_id":             {clientID},
		"response_type":         {"code"},
		"redirect_uri":          {redirect},
		"scope":                 {scope},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"prompt":                {"consent"},
	}
	return "https://discord.com/oauth2/authorize?" + q.Encode()
}

func DiscordLoginScope(reg DiscordRegistration) string {
	parts := []string{"identify"}
	if reg.NeedGuildsScope() {
		parts = append(parts, "guilds")
	}
	if reg.NeedRoleScope() {
		parts = append(parts, "guilds.members.read")
	}
	return strings.Join(parts, " ")
}

func ExchangeDiscordCode(ctx context.Context, clientID, secret, redirect, code, verifier string) (DiscordOAuth, error) {
	var out DiscordOAuth
	form := url.Values{
		"client_id": {clientID}, "client_secret": {secret}, "grant_type": {"authorization_code"},
		"code": {code}, "redirect_uri": {redirect}, "code_verifier": {verifier},
	}
	cli := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://discord.com/api/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := cli.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("discord token: %s", truncate(string(b), 180))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(b, &tok); err != nil || tok.AccessToken == "" {
		return out, fmt.Errorf("discord token missing")
	}
	out.AccessToken = tok.AccessToken
	ureq, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://discord.com/api/users/@me", nil)
	if err != nil {
		return out, err
	}
	ureq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	uresp, err := cli.Do(ureq)
	if err != nil {
		return out, err
	}
	defer uresp.Body.Close()
	ub, _ := io.ReadAll(io.LimitReader(uresp.Body, 1<<20))
	if uresp.StatusCode >= 400 {
		return out, fmt.Errorf("discord profile: %s", uresp.Status)
	}
	if err := json.Unmarshal(ub, &out.Profile); err != nil {
		return out, err
	}
	if out.Profile.ID == "" {
		return out, fmt.Errorf("discord profile missing id")
	}
	return out, nil
}

func CheckDiscordRegistration(ctx context.Context, accessToken string, reg DiscordRegistration) error {
	if !reg.GuildEnabled && !reg.RoleEnabled {
		return nil
	}
	cli := &http.Client{Timeout: 20 * time.Second}
	if reg.GuildEnabled {
		if strings.TrimSpace(reg.GuildID) == "" {
			return fmt.Errorf("not_in_server")
		}
		ids, err := discordUserGuildIDs(ctx, cli, accessToken)
		if err != nil {
			return err
		}
		if !guildIDsContain(ids, strings.TrimSpace(reg.GuildID)) {
			return fmt.Errorf("not_in_server")
		}
	}
	if reg.RoleEnabled {
		if strings.TrimSpace(reg.GuildID) == "" || strings.TrimSpace(reg.RoleID) == "" {
			return fmt.Errorf("missing_role")
		}
		roles, err := discordMemberRoles(ctx, cli, accessToken, strings.TrimSpace(reg.GuildID))
		if err != nil {
			return err
		}
		if !guildIDsContain(roles, strings.TrimSpace(reg.RoleID)) {
			return fmt.Errorf("missing_role")
		}
	}
	return nil
}

func discordUserGuildIDs(ctx context.Context, cli *http.Client, token string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://discord.com/api/users/@me/guilds", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("not_in_server")
	}
	var guilds []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(b, &guilds); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(guilds))
	for _, g := range guilds {
		ids = append(ids, g.ID)
	}
	return ids, nil
}

func discordMemberRoles(ctx context.Context, cli *http.Client, token, guildID string) ([]string, error) {
	u := "https://discord.com/api/users/@me/guilds/" + url.PathEscape(guildID) + "/member"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("missing_role")
	}
	var mem struct {
		Roles []string `json:"roles"`
	}
	if err := json.Unmarshal(b, &mem); err != nil {
		return nil, err
	}
	return mem.Roles, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func SyncDiscordEnv(ctx context.Context, pool *pgxpool.Pool, box *cryptox.Box, clientID, secret, botToken string) {
	if clientID == "" && botToken == "" {
		return
	}
	var encBot, encSec []byte
	if box != nil && botToken != "" {
		encBot, _ = box.Encrypt([]byte(botToken))
	}
	if box != nil && secret != "" {
		encSec, _ = box.Encrypt([]byte(secret))
	}
	enabled := botToken != ""
	_, _ = pool.Exec(ctx, `
		UPDATE discord_settings SET
			client_id=COALESCE(NULLIF($1,''), client_id),
			client_secret_enc=COALESCE($2, client_secret_enc),
			bot_token_enc=COALESCE($3, bot_token_enc),
			application_id=COALESCE(NULLIF($1,''), application_id),
			enabled=CASE WHEN $4 THEN true ELSE enabled END,
			updated_at=now()
		WHERE id=1`, clientID, encSec, encBot, enabled)
}

func StoreLoginState(ctx context.Context, pool *pgxpool.Pool, box *cryptox.Box, state, verifier string) error {
	var enc []byte
	if box != nil {
		enc, _ = box.Encrypt([]byte(verifier))
	}
	_, err := pool.Exec(ctx, `INSERT INTO login_states (state, provider, code_verifier_enc, expires_at) VALUES ($1,'discord',$2,now()+interval '15 minutes')`, state, enc)
	return err
}

func TakeLoginState(ctx context.Context, pool *pgxpool.Pool, box *cryptox.Box, state string) (string, error) {
	var enc []byte
	err := pool.QueryRow(ctx, `DELETE FROM login_states WHERE state=$1 AND expires_at>now() RETURNING code_verifier_enc`, state).Scan(&enc)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", fmt.Errorf("invalid state")
		}
		return "", err
	}
	if box != nil && len(enc) > 0 {
		p, e := box.Decrypt(enc)
		if e != nil {
			return "", e
		}
		return string(p), nil
	}
	return "", nil
}

func DiscordCallbackURL(public string) string {
	return strings.TrimRight(public, "/") + "/api/v1/auth/discord/callback"
}
