package scrobble

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (s *Service) LastFMConfigured(ctx context.Context) (bool, bool) {
	k, sec := s.lastFMKeys(ctx)
	return k != "", sec != ""
}

func (s *Service) lastFMKeys(ctx context.Context) (key, secret string) {
	key = os.Getenv("SD_LASTFM_API_KEY")
	secret = os.Getenv("SD_LASTFM_API_SECRET")
	if key != "" && secret != "" {
		return key, secret
	}
	var raw []byte
	_ = s.pool.QueryRow(ctx, `SELECT value FROM server_settings WHERE key='scrobble.lastfm'`).Scan(&raw)
	var m struct {
		APIKey string `json:"api_key"`
		Secret string `json:"api_secret"`
	}
	_ = json.Unmarshal(raw, &m)
	if key == "" {
		key = m.APIKey
	}
	if secret == "" {
		secret = m.Secret
	}
	return key, secret
}

func (s *Service) SaveLastFMKeys(ctx context.Context, apiKey, secret string) error {
	b, _ := json.Marshal(map[string]string{"api_key": apiKey, "api_secret": secret})
	_, err := s.pool.Exec(ctx, `INSERT INTO server_settings (key, value) VALUES ('scrobble.lastfm', $1::jsonb)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, b)
	return err
}

func lastFMSign(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString(params[k])
	}
	b.WriteString(secret)
	sum := md5.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func (s *Service) ConnectLastFM(ctx context.Context, userID uuid.UUID, username, password string) error {
	_ = EnsureSchema(ctx, s.pool)
	apiKey, secret := s.lastFMKeys(ctx)
	if apiKey == "" || secret == "" {
		return fmt.Errorf("Last.fm API key is not configured (set SD_LASTFM_API_KEY and SD_LASTFM_API_SECRET)")
	}
	params := map[string]string{
		"method":   "auth.getMobileSession",
		"username": username,
		"password": password,
		"api_key":  apiKey,
	}
	params["api_sig"] = lastFMSign(params, secret)
	params["format"] = "json"
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://ws.audioscrobbler.com/2.0/", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		Session struct {
			Name string `json:"name"`
			Key  string `json:"key"`
		} `json:"session"`
		Message string `json:"message"`
		Error   int    `json:"error"`
	}
	_ = json.Unmarshal(raw, &out)
	if out.Session.Key == "" {
		if out.Message != "" {
			return fmt.Errorf("%s", out.Message)
		}
		return fmt.Errorf("Last.fm login failed")
	}
	var enc []byte
	if s.box != nil {
		enc, _ = s.box.Encrypt([]byte(out.Session.Key))
	}
	name := out.Session.Name
	if name == "" {
		name = username
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO scrobble_accounts (user_id, lastfm_username, lastfm_session_enc, updated_at)
		VALUES ($1,$2,$3,now())
		ON CONFLICT (user_id) DO UPDATE SET lastfm_username=EXCLUDED.lastfm_username, lastfm_session_enc=EXCLUDED.lastfm_session_enc, updated_at=now()`,
		userID, name, enc)
	return err
}

func (s *Service) DisconnectLastFM(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE scrobble_accounts SET lastfm_username='', lastfm_session_enc=NULL, updated_at=now() WHERE user_id=$1`, userID)
	return err
}

func (s *Service) lastFMSession(ctx context.Context, userID uuid.UUID) (user, sk string) {
	var enc []byte
	_ = s.pool.QueryRow(ctx, `SELECT lastfm_username, lastfm_session_enc FROM scrobble_accounts WHERE user_id=$1`, userID).Scan(&user, &enc)
	if len(enc) == 0 || s.box == nil {
		return user, ""
	}
	plain, err := s.box.Decrypt(enc)
	if err != nil {
		return user, ""
	}
	return user, string(plain)
}

func (s *Service) submitLastFM(ctx context.Context, userID uuid.UUID, title, artist, album string, durationMS int, scrobble bool) error {
	apiKey, secret := s.lastFMKeys(ctx)
	_, sk := s.lastFMSession(ctx, userID)
	if apiKey == "" || secret == "" || sk == "" {
		return nil
	}
	method := "track.updateNowPlaying"
	if scrobble {
		method = "track.scrobble"
	}
	params := map[string]string{
		"method":  method,
		"api_key": apiKey,
		"sk":      sk,
		"artist":  artist,
		"track":   title,
	}
	if album != "" {
		params["album"] = album
	}
	if durationMS > 0 {
		params["duration"] = fmt.Sprintf("%d", durationMS/1000)
	}
	if scrobble {
		params["timestamp"] = fmt.Sprintf("%d", time.Now().Unix())
	}
	params["api_sig"] = lastFMSign(params, secret)
	params["format"] = "json"
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://ws.audioscrobbler.com/2.0/", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return nil
}
