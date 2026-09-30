package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
	"github.com/sounddock/sounddock/internal/minilib"
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

// DiscordRegistrationGuild is one whitelisted server. With RoleIDs set, a new
// user must hold at least one of those roles in that server.
type DiscordRegistrationGuild struct {
	GuildID string   `json:"guild_id"`
	Label   string   `json:"label"`
	RoleIDs []string `json:"role_ids"`
}

// DiscordRegistration gates new Discord accounts: when Enabled, the user must
// pass at least one entry in Guilds.
type DiscordRegistration struct {
	Enabled bool
	Guilds  []DiscordRegistrationGuild
}

var (
	ErrNotInServer = errors.New("not_in_server")
	ErrMissingRole = errors.New("missing_role")
)

const (
	maxRegistrationGuilds = 50
	maxRegistrationRoles  = 25
	maxRegistrationLabel  = 100
)

func DiscordUserExists(ctx context.Context, pool *pgxpool.Pool, discordID string) (bool, error) {
	var n int
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_identities WHERE provider='discord' AND provider_user_id=$1`, discordID).Scan(&n)
	return n > 0, err
}

func LoadDiscordRegistration(ctx context.Context, pool *pgxpool.Pool) (DiscordRegistration, error) {
	var r DiscordRegistration
	if err := pool.QueryRow(ctx, `SELECT registration_guild_enabled FROM discord_settings WHERE id=1`).Scan(&r.Enabled); err != nil {
		return r, err
	}
	rows, err := pool.Query(ctx, `SELECT guild_id, label, role_ids FROM discord_registration_guilds ORDER BY position, guild_id`)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	r.Guilds = []DiscordRegistrationGuild{}
	for rows.Next() {
		var g DiscordRegistrationGuild
		if err := rows.Scan(&g.GuildID, &g.Label, &g.RoleIDs); err != nil {
			return r, err
		}
		if g.RoleIDs == nil {
			g.RoleIDs = []string{}
		}
		r.Guilds = append(r.Guilds, g)
	}
	return r, rows.Err()
}

// SaveDiscordRegistration replaces the whitelist. Pass guilds through
// NormalizeDiscordRegistrationGuilds first.
func SaveDiscordRegistration(ctx context.Context, pool *pgxpool.Pool, reg DiscordRegistration) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE discord_settings SET registration_guild_enabled=$1, updated_at=now() WHERE id=1`, reg.Enabled); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM discord_registration_guilds`); err != nil {
		return err
	}
	for i, g := range reg.Guilds {
		roles := g.RoleIDs
		if roles == nil {
			roles = []string{}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO discord_registration_guilds (guild_id, label, role_ids, position) VALUES ($1,$2,$3,$4)`, g.GuildID, g.Label, roles, i); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// NormalizeDiscordRegistrationGuilds trims and validates whitelist entries and
// drops duplicate role IDs. A server listed twice is an error.
func NormalizeDiscordRegistrationGuilds(in []DiscordRegistrationGuild) ([]DiscordRegistrationGuild, error) {
	if len(in) > maxRegistrationGuilds {
		return nil, fmt.Errorf("at most %d servers", maxRegistrationGuilds)
	}
	seen := map[string]struct{}{}
	out := make([]DiscordRegistrationGuild, 0, len(in))
	for _, g := range in {
		id := strings.TrimSpace(g.GuildID)
		if !isSnowflake(id) {
			return nil, fmt.Errorf("server ID %q must be a numeric Discord snowflake", g.GuildID)
		}
		if _, dup := seen[id]; dup {
			return nil, fmt.Errorf("server %s is listed twice", id)
		}
		seen[id] = struct{}{}
		label := strings.TrimSpace(g.Label)
		if r := []rune(label); len(r) > maxRegistrationLabel {
			label = string(r[:maxRegistrationLabel])
		}
		roles := []string{}
		roleSeen := map[string]struct{}{}
		for _, rid := range g.RoleIDs {
			rid = strings.TrimSpace(rid)
			if rid == "" {
				continue
			}
			if !isSnowflake(rid) {
				return nil, fmt.Errorf("role ID %q must be a numeric Discord snowflake", rid)
			}
			if _, dup := roleSeen[rid]; dup {
				continue
			}
			roleSeen[rid] = struct{}{}
			roles = append(roles, rid)
		}
		if len(roles) > maxRegistrationRoles {
			return nil, fmt.Errorf("at most %d roles per server", maxRegistrationRoles)
		}
		out = append(out, DiscordRegistrationGuild{GuildID: id, Label: label, RoleIDs: roles})
	}
	return out, nil
}

func isSnowflake(s string) bool {
	if s == "" || len(s) > 20 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (r DiscordRegistration) NeedGuildsScope() bool {
	return r.Enabled
}

func (r DiscordRegistration) NeedRoleScope() bool {
	if !r.Enabled {
		return false
	}
	for _, g := range r.Guilds {
		if len(g.RoleIDs) > 0 {
			return true
		}
	}
	return false
}

func idsIntersect(have, want []string) bool {
	for _, w := range want {
		for _, h := range have {
			if h == w {
				return true
			}
		}
	}
	return false
}

func (s *Service) AdministratorCount(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE r.name='Administrator'`).Scan(&n)
	return n, err
}

func LoadAdminDiscordIDs(ctx context.Context, pool *pgxpool.Pool) []string {
	var raw string
	_ = pool.QueryRow(ctx, `SELECT coalesce(admin_discord_ids,'') FROM discord_settings WHERE id=1`).Scan(&raw)
	return splitIDs(raw)
}

func RecordAdminDiscordID(ctx context.Context, pool *pgxpool.Pool, id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	ids := LoadAdminDiscordIDs(ctx, pool)
	if IsAdminDiscordID(id, ids) {
		return
	}
	ids = append(ids, id)
	_, _ = pool.Exec(ctx, `UPDATE discord_settings SET admin_discord_ids=$1, updated_at=now() WHERE id=1`, strings.Join(ids, ","))
}

func RemoveAdminDiscordID(ctx context.Context, pool *pgxpool.Pool, id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	ids := LoadAdminDiscordIDs(ctx, pool)
	out := make([]string, 0, len(ids))
	for _, a := range ids {
		if a != id {
			out = append(out, a)
		}
	}
	if len(out) == len(ids) {
		return
	}
	_, _ = pool.Exec(ctx, `UPDATE discord_settings SET admin_discord_ids=$1, updated_at=now() WHERE id=1`, strings.Join(out, ","))
}

func splitIDs(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

type DiscordOAuthConfig struct {
	LoginEnabled bool
	ClientID     string
	Secret       string
}

func LoadDiscordOAuth(ctx context.Context, pool *pgxpool.Pool, box *cryptox.Box) DiscordOAuthConfig {
	var login bool
	var clientID *string
	var enc []byte
	_ = pool.QueryRow(ctx, `SELECT login_enabled, client_id, client_secret_enc FROM discord_settings WHERE id=1`).Scan(&login, &clientID, &enc)
	out := DiscordOAuthConfig{LoginEnabled: login}
	if clientID != nil {
		out.ClientID = strings.TrimSpace(*clientID)
	}
	if box != nil && len(enc) > 0 {
		if p, err := box.Decrypt(enc); err == nil {
			out.Secret = string(p)
		}
	}
	return out
}

func (c DiscordOAuthConfig) Ready() bool {
	return c.LoginEnabled && c.ClientID != "" && c.Secret != ""
}

func DiscordAvatarURL(discordUserID, avatarHash string) string {
	discordUserID = strings.TrimSpace(discordUserID)
	if discordUserID == "" {
		return ""
	}
	hash := strings.TrimSpace(avatarHash)
	if hash != "" {
		ext := "png"
		if strings.HasPrefix(hash, "a_") {
			ext = "gif"
		}
		return "https://cdn.discordapp.com/avatars/" + discordUserID + "/" + hash + "." + ext + "?size=80"
	}
	n, err := strconv.ParseUint(discordUserID, 10, 64)
	if err != nil {
		return "https://cdn.discordapp.com/embed/avatars/0.png"
	}
	return "https://cdn.discordapp.com/embed/avatars/" + strconv.FormatUint((n>>22)%6, 10) + ".png"
}

func DiscordDisplayName(p DiscordProfile) string {
	if g := strings.TrimSpace(p.Global); g != "" {
		return g
	}
	if u := strings.TrimSpace(p.Username); u != "" {
		return u
	}
	return strings.TrimSpace(p.ID)
}

func DiscordAccountUsername(p DiscordProfile) string {
	if u := strings.TrimSpace(p.Username); u != "" {
		return u
	}
	return strings.TrimSpace(p.ID)
}

func isDiscordStubUsername(username, discordID string) bool {
	username = strings.TrimSpace(username)
	discordID = strings.TrimSpace(discordID)
	return username == "discord_"+discordID || username == discordID
}

func uniqueViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

func (s *Service) attachDiscordIdentity(ctx context.Context, uid uuid.UUID, p DiscordProfile) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_identities (user_id, provider, provider_user_id, provider_username, avatar_hash)
		VALUES ($1,'discord',$2,$3,$4)
		ON CONFLICT (provider, provider_user_id) DO UPDATE
		SET user_id=EXCLUDED.user_id, provider_username=EXCLUDED.provider_username, avatar_hash=EXCLUDED.avatar_hash`,
		uid, p.ID, strings.TrimSpace(p.Username), strings.TrimSpace(p.Avatar))
	return err
}

func (s *Service) createDiscordLocalUser(ctx context.Context, p DiscordProfile) (uuid.UUID, error) {
	display := DiscordDisplayName(p)
	uname := DiscordAccountUsername(p)
	hash, err := HashPassword(uuid.NewString() + uuid.NewString())
	if err != nil {
		return uuid.Nil, err
	}
	var uid uuid.UUID
	err = s.pool.QueryRow(ctx, `
		INSERT INTO users (username, password_hash, display_name)
		VALUES ($1,$2,$3) RETURNING id`, uname, hash, display).Scan(&uid)
	if uniqueViolation(err) {
		uname = strings.TrimSpace(p.ID)
		err = s.pool.QueryRow(ctx, `
			INSERT INTO users (username, password_hash, display_name)
			VALUES ($1,$2,$3) RETURNING id`, uname, hash, display).Scan(&uid)
	}
	if err != nil {
		return uuid.Nil, err
	}
	_, _ = s.pool.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE name='User' ON CONFLICT DO NOTHING`, uid)
	if err := s.attachDiscordIdentity(ctx, uid, p); err != nil {
		return uuid.Nil, err
	}
	_ = minilib.Reconcile(ctx, s.pool, uid, p.ID)
	return uid, nil
}

func (s *Service) UpsertDiscordUser(ctx context.Context, p DiscordProfile) (*User, error) {
	display := DiscordDisplayName(p)

	var uid uuid.UUID
	var existingName string
	err := s.pool.QueryRow(ctx, `
		SELECT i.user_id, u.username
		FROM user_identities i
		JOIN users u ON u.id = i.user_id
		WHERE i.provider='discord' AND i.provider_user_id=$1`, p.ID).Scan(&uid, &existingName)

	switch {
	case err == nil:
		_, _ = s.pool.Exec(ctx, `UPDATE user_identities SET provider_username=$2, avatar_hash=$3 WHERE provider='discord' AND provider_user_id=$1`, p.ID, strings.TrimSpace(p.Username), strings.TrimSpace(p.Avatar))
		_ = minilib.Reconcile(ctx, s.pool, uid, p.ID)
		if isDiscordStubUsername(existingName, p.ID) {
			newName := DiscordAccountUsername(p)
			if _, uerr := s.pool.Exec(ctx, `UPDATE users SET username=$2, display_name=$3, updated_at=now() WHERE id=$1`, uid, newName, display); uniqueViolation(uerr) {
				_, _ = s.pool.Exec(ctx, `UPDATE users SET username=$2, display_name=$3, updated_at=now() WHERE id=$1`, uid, p.ID, display)
			}
		} else if existingName == DiscordAccountUsername(p) || strings.HasSuffix(existingName, "_"+p.ID) {
			_, _ = s.pool.Exec(ctx, `UPDATE users SET display_name=$2, updated_at=now() WHERE id=$1`, uid, display)
		}
	case errors.Is(err, pgx.ErrNoRows):
		uid, err = s.createDiscordLocalUser(ctx, p)
		if err != nil {
			return nil, err
		}
	default:
		return nil, err
	}

	admins, _ := s.AdministratorCount(ctx)
	stored := LoadAdminDiscordIDs(ctx, s.pool)
	if admins == 0 || IsAdminDiscordID(p.ID, stored) {
		_, _ = s.pool.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE name='Administrator' ON CONFLICT DO NOTHING`, uid)
		RecordAdminDiscordID(ctx, s.pool, p.ID)
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
	return err
}

func NormalizeAdminDiscordIDs(raw []string) ([]string, error) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(raw))
	for _, id := range raw {
		for _, p := range strings.Split(id, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if !isSnowflake(p) {
				return nil, fmt.Errorf("invalid discord user id")
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	return out, nil
}

func SaveAdminDiscordIDs(ctx context.Context, pool *pgxpool.Pool, ids []string) error {
	_, err := pool.Exec(ctx, `UPDATE discord_settings SET admin_discord_ids=$1, updated_at=now() WHERE id=1`, strings.Join(ids, ","))
	if err != nil {
		return err
	}
	return ApplyAdminDiscordIDs(ctx, pool, ids)
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

// discordMembership answers guild and role questions for the signing-in user.
type discordMembership interface {
	GuildIDs(ctx context.Context) ([]string, error)
	MemberRoles(ctx context.Context, guildID string) ([]string, error)
}

// CheckDiscordRegistration returns nil when the whitelist is off or the user
// passes at least one listed server. It returns ErrNotInServer or
// ErrMissingRole on a clean denial, and another error when Discord could not
// be asked.
func CheckDiscordRegistration(ctx context.Context, accessToken string, reg DiscordRegistration) error {
	if !reg.Enabled {
		return nil
	}
	return checkRegistration(ctx, reg, httpMembership{cli: &http.Client{Timeout: 20 * time.Second}, token: accessToken})
}

func checkRegistration(ctx context.Context, reg DiscordRegistration, m discordMembership) error {
	if !reg.Enabled {
		return nil
	}
	if len(reg.Guilds) == 0 {
		return ErrNotInServer
	}
	ids, err := m.GuildIDs(ctx)
	if err != nil {
		return err
	}
	member := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		member[id] = struct{}{}
	}
	inAny := false
	var lookupErr error
	for _, g := range reg.Guilds {
		if _, ok := member[g.GuildID]; !ok {
			continue
		}
		inAny = true
		if len(g.RoleIDs) == 0 {
			return nil
		}
		roles, err := m.MemberRoles(ctx, g.GuildID)
		if err != nil {
			if !errors.Is(err, ErrMissingRole) && lookupErr == nil {
				lookupErr = err
			}
			continue
		}
		if idsIntersect(roles, g.RoleIDs) {
			return nil
		}
	}
	if !inAny {
		return ErrNotInServer
	}
	if lookupErr != nil {
		return lookupErr
	}
	return ErrMissingRole
}

type httpMembership struct {
	cli   *http.Client
	token string
}

// GuildIDs pages through /users/@me/guilds; Discord caps each page at 200.
func (h httpMembership) GuildIDs(ctx context.Context) ([]string, error) {
	var ids []string
	after := ""
	for page := 0; page < 10; page++ {
		u := "https://discord.com/api/users/@me/guilds?limit=200"
		if after != "" {
			u += "&after=" + url.QueryEscape(after)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+h.token)
		resp, err := h.cli.Do(req)
		if err != nil {
			return nil, err
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("discord guilds: %s", resp.Status)
		}
		var guilds []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(b, &guilds); err != nil {
			return nil, err
		}
		for _, g := range guilds {
			ids = append(ids, g.ID)
		}
		if len(guilds) < 200 {
			break
		}
		after = guilds[len(guilds)-1].ID
	}
	return ids, nil
}

// MemberRoles returns ErrMissingRole when Discord says the user is not a member.
func (h httpMembership) MemberRoles(ctx context.Context, guildID string) ([]string, error) {
	u := "https://discord.com/api/users/@me/guilds/" + url.PathEscape(guildID) + "/member"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	resp, err := h.cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return nil, ErrMissingRole
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("discord member: %s", resp.Status)
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
