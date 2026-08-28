package external

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

const (
	StatusConnected      = "connected"
	StatusNeedsReconnect = "needs_reconnect"
	refreshSkew          = 2 * time.Minute
)

var (
	ErrNeedsReconnect = errors.New("needs reconnect")
	ErrTemporary      = errors.New("temporary error")

	spotifyTokenURL    = "https://accounts.spotify.com/api/token"
	youtubeTokenURL    = "https://oauth2.googleapis.com/token"
	soundcloudTokenURL = "https://secure.soundcloud.com/oauth/token"
	refreshFlight      flightGroup
)

type Settings struct {
	Provider        string
	Enabled         bool
	UsersMayConnect bool
	PublicImport    bool
	ClientID        string
	ClientSecret    string
	Extra           map[string]string // apple developer_token, youtube api_key
	DefaultInterval string
	MinInterval     string
}

func LoadSettings(ctx context.Context, pool *pgxpool.Pool, box *cryptox.Box, provider string) (Settings, error) {
	s := Settings{Provider: provider, Extra: map[string]string{}}
	var sec, extra []byte
	err := pool.QueryRow(ctx, `SELECT enabled, users_may_connect, public_import, client_id, client_secret_enc, extra_enc, default_sync_interval, min_sync_interval FROM external_provider_settings WHERE provider=$1`, provider).
		Scan(&s.Enabled, &s.UsersMayConnect, &s.PublicImport, &s.ClientID, &sec, &extra, &s.DefaultInterval, &s.MinInterval)
	if err != nil {
		return s, err
	}
	if box != nil && len(sec) > 0 {
		if p, e := box.Decrypt(sec); e == nil {
			s.ClientSecret = string(p)
		}
	}
	if box != nil && len(extra) > 0 {
		if p, e := box.Decrypt(extra); e == nil {
			_ = json.Unmarshal(p, &s.Extra)
		}
	}
	if s.Extra == nil {
		s.Extra = map[string]string{}
	}
	return s, nil
}

func SaveSettings(ctx context.Context, pool *pgxpool.Pool, box *cryptox.Box, s Settings) error {
	var sec, extra []byte
	if box != nil && s.ClientSecret != "" {
		sec, _ = box.Encrypt([]byte(s.ClientSecret))
	}
	if box != nil && len(s.Extra) > 0 {
		b, _ := json.Marshal(s.Extra)
		extra, _ = box.Encrypt(b)
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO external_provider_settings (provider, enabled, users_may_connect, public_import, client_id, client_secret_enc, extra_enc, default_sync_interval, min_sync_interval, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,now())
		ON CONFLICT (provider) DO UPDATE SET
			enabled=EXCLUDED.enabled, users_may_connect=EXCLUDED.users_may_connect, public_import=EXCLUDED.public_import,
			client_id=COALESCE(NULLIF(EXCLUDED.client_id,''), external_provider_settings.client_id),
			client_secret_enc=COALESCE(EXCLUDED.client_secret_enc, external_provider_settings.client_secret_enc),
			extra_enc=COALESCE(EXCLUDED.extra_enc, external_provider_settings.extra_enc),
			default_sync_interval=EXCLUDED.default_sync_interval, min_sync_interval=EXCLUDED.min_sync_interval, updated_at=now()`,
		s.Provider, s.Enabled, s.UsersMayConnect, s.PublicImport, s.ClientID, sec, extra, s.DefaultInterval, s.MinInterval)
	return err
}

func PKCE() (verifier, challenge string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return
}

func httpJSON(ctx context.Context, method, rawURL, bearer string, form url.Values, out any) error {
	var body io.Reader
	ct := ""
	if form != nil {
		body = strings.NewReader(form.Encode())
		ct = "application/x-www-form-urlencoded"
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s: %s", resp.Status, truncate(string(b), 200))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func AuthURL(provider, clientID, redirect, state, challenge string) string {
	switch provider {
	case "spotify":
		q := url.Values{
			"client_id": {clientID}, "response_type": {"code"}, "redirect_uri": {redirect},
			"state": {state}, "code_challenge_method": {"S256"}, "code_challenge": {challenge},
			"scope": {"playlist-read-private playlist-read-collaborative"},
		}
		return "https://accounts.spotify.com/authorize?" + q.Encode()
	case "youtube":
		q := url.Values{
			"client_id": {clientID}, "response_type": {"code"}, "redirect_uri": {redirect},
			"state": {state}, "access_type": {"offline"}, "prompt": {"consent"},
			"scope":          {"https://www.googleapis.com/auth/youtube.readonly"},
			"code_challenge": {challenge}, "code_challenge_method": {"S256"},
		}
		return "https://accounts.google.com/o/oauth2/v2/auth?" + q.Encode()
	case "soundcloud":
		q := url.Values{
			"client_id": {clientID}, "response_type": {"code"}, "redirect_uri": {redirect},
			"state": {state},
		}
		return "https://secure.soundcloud.com/authorize?" + q.Encode()
	default:
		return ""
	}
}

func ExchangeCode(ctx context.Context, provider, clientID, secret, redirect, code, verifier string) (Token, error) {
	var tok Token
	switch provider {
	case "spotify":
		form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}, "client_id": {clientID}, "code_verifier": {verifier}}
		var raw map[string]any
		if err := httpJSON(ctx, http.MethodPost, "https://accounts.spotify.com/api/token", "", form, &raw); err != nil {
			return tok, err
		}
		tok.Access, _ = raw["access_token"].(string)
		tok.Refresh, _ = raw["refresh_token"].(string)
		if n, _ := raw["expires_in"].(float64); n > 0 {
			tok.Expiry = time.Now().Add(time.Duration(n) * time.Second)
		}
		var me map[string]any
		_ = httpJSON(ctx, http.MethodGet, "https://api.spotify.com/v1/me", tok.Access, nil, &me)
		tok.AccountID, _ = me["id"].(string)
		tok.Name, _ = me["display_name"].(string)
		tok.Scopes = []string{"playlist-read-private"}
	case "youtube":
		form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}, "client_id": {clientID}, "client_secret": {secret}, "code_verifier": {verifier}}
		var raw map[string]any
		if err := httpJSON(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", "", form, &raw); err != nil {
			return tok, err
		}
		tok.Access, _ = raw["access_token"].(string)
		tok.Refresh, _ = raw["refresh_token"].(string)
		if n, _ := raw["expires_in"].(float64); n > 0 {
			tok.Expiry = time.Now().Add(time.Duration(n) * time.Second)
		}
		tok.Name = "YouTube"
		tok.AccountID = "youtube"
	case "soundcloud":
		form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}, "client_id": {clientID}, "client_secret": {secret}}
		var raw map[string]any
		if err := httpJSON(ctx, http.MethodPost, "https://secure.soundcloud.com/oauth/token", "", form, &raw); err != nil {
			return tok, err
		}
		tok.Access, _ = raw["access_token"].(string)
		tok.Refresh, _ = raw["refresh_token"].(string)
		tok.Name = "SoundCloud"
	default:
		return tok, fmt.Errorf("unsupported provider")
	}
	if tok.Access == "" {
		return tok, fmt.Errorf("no access token")
	}
	return tok, nil
}

func CallbackURL(public, provider string) string {
	return strings.TrimRight(public, "/") + "/api/v1/oauth/" + provider + "/callback"
}

func ClientCredentials(ctx context.Context, provider, clientID, secret string) (string, error) {
	switch provider {
	case "spotify":
		form := url.Values{"grant_type": {"client_credentials"}, "client_id": {clientID}, "client_secret": {secret}}
		var raw map[string]any
		if err := httpJSON(ctx, http.MethodPost, "https://accounts.spotify.com/api/token", "", form, &raw); err != nil {
			return "", err
		}
		s, _ := raw["access_token"].(string)
		return s, nil
	default:
		return "", fmt.Errorf("no client credentials for %s", provider)
	}
}

func publicAccess(ctx context.Context, st Settings) (access, extra string) {
	if st.Extra == nil {
		st.Extra = map[string]string{}
	}
	extra = st.Extra["developer_token"]
	if extra == "" {
		extra = st.Extra["api_key"]
	}
	if st.Provider == "spotify" && st.ClientID != "" && st.ClientSecret != "" {
		access, _ = ClientCredentials(ctx, st.Provider, st.ClientID, st.ClientSecret)
	}
	return
}

func NewState() string {
	s, _ := cryptox.RandomToken(24)
	return s
}

func StoreAccount(ctx context.Context, pool *pgxpool.Pool, box *cryptox.Box, userID uuid.UUID, provider string, tok Token) error {
	var acc, ref []byte
	if box != nil {
		acc, _ = box.Encrypt([]byte(tok.Access))
		if tok.Refresh != "" {
			ref, _ = box.Encrypt([]byte(tok.Refresh))
		}
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO external_provider_accounts (user_id, provider, provider_account_id, display_name, access_token_enc, refresh_token_enc, token_expiry, scopes, status, last_error, connected_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'connected','',now())
		ON CONFLICT (user_id, provider) DO UPDATE SET
			provider_account_id=EXCLUDED.provider_account_id, display_name=EXCLUDED.display_name,
			access_token_enc=EXCLUDED.access_token_enc, refresh_token_enc=COALESCE(EXCLUDED.refresh_token_enc, external_provider_accounts.refresh_token_enc),
			token_expiry=EXCLUDED.token_expiry, scopes=EXCLUDED.scopes, status='connected', last_error='', connected_at=now()`,
		userID, provider, tok.AccountID, tok.Name, acc, ref, tok.Expiry, tok.Scopes)
	return err
}

func extraFrom(st Settings) string {
	if st.Extra == nil {
		return ""
	}
	extra := st.Extra["developer_token"]
	if extra == "" {
		extra = st.Extra["api_key"]
	}
	return extra
}

func decryptString(box *cryptox.Box, enc []byte) string {
	if box == nil || len(enc) == 0 {
		return ""
	}
	p, err := box.Decrypt(enc)
	if err != nil {
		return ""
	}
	return string(p)
}

func tokenFresh(expiry time.Time) bool {
	if expiry.IsZero() {
		return false
	}
	return time.Until(expiry) > refreshSkew
}

func tokenEndpoint(provider string) string {
	switch provider {
	case "spotify":
		return spotifyTokenURL
	case "youtube":
		return youtubeTokenURL
	case "soundcloud":
		return soundcloudTokenURL
	default:
		return ""
	}
}

func providerLabel(provider string) string {
	switch provider {
	case "spotify":
		return "Spotify"
	case "youtube":
		return "YouTube"
	case "soundcloud":
		return "SoundCloud"
	default:
		return provider
	}
}

func reconnectMessage(provider string) string {
	return "Reconnect required. " + providerLabel(provider) + " revoked or expired this login."
}

func temporaryMessage(provider string) string {
	return providerLabel(provider) + " is temporarily unavailable. Try again shortly."
}

func tokenFromRaw(raw map[string]any) Token {
	var tok Token
	tok.Access, _ = raw["access_token"].(string)
	tok.Refresh, _ = raw["refresh_token"].(string)
	if n, _ := raw["expires_in"].(float64); n > 0 {
		tok.Expiry = time.Now().Add(time.Duration(n) * time.Second)
	}
	return tok
}

type flightGroup struct {
	mu sync.Mutex
	m  map[string]*flightCall
}

type flightCall struct {
	wg  sync.WaitGroup
	tok Token
	err error
}

func (g *flightGroup) do(key string, fn func() (Token, error)) (Token, error) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*flightCall)
	}
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.tok, c.err
	}
	c := &flightCall{}
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	c.tok, c.err = fn()
	c.wg.Done()

	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()
	return c.tok, c.err
}

func classifyTokenStatus(status int, body []byte) error {
	var raw struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &raw)
	if strings.EqualFold(raw.Error, "invalid_grant") {
		msg := raw.ErrorDescription
		if msg == "" {
			msg = "invalid_grant"
		}
		return fmt.Errorf("%w: %s", ErrNeedsReconnect, msg)
	}
	if status >= 500 {
		return fmt.Errorf("%w: %s", ErrTemporary, truncate(string(body), 200))
	}
	if status >= 400 {
		return fmt.Errorf("token refresh: %s %s", http.StatusText(status), truncate(string(body), 200))
	}
	return nil
}

// RefreshToken exchanges a refresh token for a new access token.
// A rotated refresh_token is returned when the provider sends one; otherwise Refresh is empty.
func RefreshToken(ctx context.Context, provider, clientID, secret, refresh string) (Token, error) {
	var tok Token
	ep := tokenEndpoint(provider)
	if ep == "" || refresh == "" {
		return tok, fmt.Errorf("refresh not supported for %s", provider)
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {clientID}}
	if secret != "" {
		form.Set("client_secret", secret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep, strings.NewReader(form.Encode()))
	if err != nil {
		return tok, fmt.Errorf("%w: %v", ErrTemporary, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return tok, fmt.Errorf("%w: %v", ErrTemporary, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err := classifyTokenStatus(resp.StatusCode, b); err != nil {
		return tok, err
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return tok, fmt.Errorf("%w: %v", ErrTemporary, err)
	}
	tok = tokenFromRaw(raw)
	if tok.Access == "" {
		return tok, fmt.Errorf("%w: no access token", ErrTemporary)
	}
	return tok, nil
}

func persistRotatedTokens(ctx context.Context, pool *pgxpool.Pool, box *cryptox.Box, accountID uuid.UUID, tok Token) error {
	var acc, ref []byte
	if box != nil {
		var err error
		acc, err = box.Encrypt([]byte(tok.Access))
		if err != nil {
			return err
		}
		if tok.Refresh != "" {
			ref, err = box.Encrypt([]byte(tok.Refresh))
			if err != nil {
				return err
			}
		}
	}
	_, err := pool.Exec(ctx, `
		UPDATE external_provider_accounts SET
			access_token_enc=$2,
			refresh_token_enc=COALESCE($3, refresh_token_enc),
			token_expiry=$4,
			status='connected',
			last_error=''
		WHERE id=$1`, accountID, acc, ref, tok.Expiry)
	return err
}

func markRefreshFailure(ctx context.Context, pool *pgxpool.Pool, accountID uuid.UUID, provider string, err error) {
	if errors.Is(err, ErrNeedsReconnect) {
		_, _ = pool.Exec(ctx, `UPDATE external_provider_accounts SET status=$2, last_error=$3 WHERE id=$1`,
			accountID, StatusNeedsReconnect, reconnectMessage(provider))
		return
	}
	if errors.Is(err, ErrTemporary) {
		_, _ = pool.Exec(ctx, `UPDATE external_provider_accounts SET last_error=$2 WHERE id=$1`,
			accountID, temporaryMessage(provider))
	}
}

func refreshAccount(ctx context.Context, pool *pgxpool.Pool, box *cryptox.Box, accountID uuid.UUID, provider string, st Settings, refresh string) (Token, error) {
	tok, err := RefreshToken(ctx, provider, st.ClientID, st.ClientSecret, refresh)
	if err != nil {
		markRefreshFailure(ctx, pool, accountID, provider, err)
		return Token{}, err
	}
	if err := persistRotatedTokens(ctx, pool, box, accountID, tok); err != nil {
		return Token{}, err
	}
	return tok, nil
}

// EnsureAccess returns a usable user access token, refreshing via per-account singleflight when expired.
// invalid_grant marks the account needs_reconnect. Provider 5xx is a temporary error and does not revoke the link.
func EnsureAccess(ctx context.Context, pool *pgxpool.Pool, box *cryptox.Box, userID uuid.UUID, provider string, st Settings) (access, extra string, err error) {
	extra = extraFrom(st)
	var (
		id             uuid.UUID
		accEnc, refEnc []byte
		expiry         *time.Time
		status         string
	)
	err = pool.QueryRow(ctx, `
		SELECT id, access_token_enc, refresh_token_enc, token_expiry, status
		FROM external_provider_accounts WHERE user_id=$1 AND provider=$2`, userID, provider).
		Scan(&id, &accEnc, &refEnc, &expiry, &status)
	if err != nil {
		return "", extra, err
	}
	access = decryptString(box, accEnc)
	refresh := decryptString(box, refEnc)
	var exp time.Time
	if expiry != nil {
		exp = *expiry
	}
	if status == StatusNeedsReconnect {
		return "", extra, fmt.Errorf("%w: %s", ErrNeedsReconnect, reconnectMessage(provider))
	}
	if tokenFresh(exp) && access != "" {
		return access, extra, nil
	}
	if refresh == "" {
		if access != "" {
			return access, extra, nil
		}
		return "", extra, fmt.Errorf("no access token")
	}

	tok, err := refreshFlight.do(id.String(), func() (Token, error) {
		var (
			acc2, ref2 []byte
			exp2       *time.Time
			st2        string
		)
		if e := pool.QueryRow(ctx, `
			SELECT access_token_enc, refresh_token_enc, token_expiry, status
			FROM external_provider_accounts WHERE id=$1`, id).
			Scan(&acc2, &ref2, &exp2, &st2); e != nil {
			return Token{}, e
		}
		if st2 == StatusNeedsReconnect {
			return Token{}, fmt.Errorf("%w: %s", ErrNeedsReconnect, reconnectMessage(provider))
		}
		a := decryptString(box, acc2)
		r := decryptString(box, ref2)
		var innerExp time.Time
		if exp2 != nil {
			innerExp = *exp2
		}
		if tokenFresh(innerExp) && a != "" {
			return Token{Access: a, Refresh: r, Expiry: innerExp}, nil
		}
		if r == "" {
			if a != "" {
				return Token{Access: a}, nil
			}
			return Token{}, fmt.Errorf("no access token")
		}
		return refreshAccount(ctx, pool, box, id, provider, st, r)
	})
	if err != nil {
		if errors.Is(err, ErrTemporary) && access != "" && !exp.IsZero() && time.Now().Before(exp) {
			return access, extra, nil
		}
		return "", extra, err
	}
	if tok.Access == "" {
		return "", extra, fmt.Errorf("no access token")
	}
	return tok.Access, extra, nil
}
