package scrobble

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

func (s *Service) ConnectListenBrainz(ctx context.Context, userID uuid.UUID, token, username string) error {
	_ = EnsureSchema(ctx, s.pool)
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("token required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.listenbrainz.org/1/validate-token", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		Valid    bool   `json:"valid"`
		UserName string `json:"user_name"`
		Message  string `json:"message"`
	}
	_ = json.Unmarshal(raw, &out)
	if !out.Valid {
		if out.Message != "" {
			return fmt.Errorf("%s", out.Message)
		}
		return fmt.Errorf("ListenBrainz token invalid")
	}
	if username == "" {
		username = out.UserName
	}
	var enc []byte
	if s.box != nil {
		enc, _ = s.box.Encrypt([]byte(token))
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO scrobble_accounts (user_id, listenbrainz_username, listenbrainz_token_enc, updated_at)
		VALUES ($1,$2,$3,now())
		ON CONFLICT (user_id) DO UPDATE SET listenbrainz_username=EXCLUDED.listenbrainz_username, listenbrainz_token_enc=EXCLUDED.listenbrainz_token_enc, updated_at=now()`,
		userID, username, enc)
	return err
}

func (s *Service) DisconnectListenBrainz(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE scrobble_accounts SET listenbrainz_username='', listenbrainz_token_enc=NULL, updated_at=now() WHERE user_id=$1`, userID)
	return err
}

func (s *Service) SetPresence(ctx context.Context, userID uuid.UUID, on bool) error {
	_ = EnsureSchema(ctx, s.pool)
	_, err := s.pool.Exec(ctx, `
		INSERT INTO scrobble_accounts (user_id, presence_enabled, updated_at) VALUES ($1,$2,now())
		ON CONFLICT (user_id) DO UPDATE SET presence_enabled=EXCLUDED.presence_enabled, updated_at=now()`,
		userID, on)
	return err
}

func (s *Service) listenBrainzToken(ctx context.Context, userID uuid.UUID) (user, token string) {
	var enc []byte
	_ = s.pool.QueryRow(ctx, `SELECT listenbrainz_username, listenbrainz_token_enc FROM scrobble_accounts WHERE user_id=$1`, userID).Scan(&user, &enc)
	if len(enc) == 0 || s.box == nil {
		return user, ""
	}
	plain, err := s.box.Decrypt(enc)
	if err != nil {
		return user, ""
	}
	return user, string(plain)
}

func (s *Service) submitListenBrainz(ctx context.Context, userID uuid.UUID, title, artist, album string, listenedAt int64, nowPlaying bool) error {
	_, token := s.listenBrainzToken(ctx, userID)
	if token == "" {
		return nil
	}
	track := map[string]any{
		"track_metadata": map[string]any{
			"artist_name":  artist,
			"track_name":   title,
			"release_name": album,
		},
	}
	kind := "single"
	if nowPlaying {
		kind = "playing_now"
	} else {
		track["listened_at"] = listenedAt
	}
	body, _ := json.Marshal(map[string]any{"listen_type": kind, "payload": []any{track}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.listenbrainz.org/1/submit-listens", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return nil
}
