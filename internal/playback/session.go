package playback

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func WebOwnerKey(userID uuid.UUID, deviceID string) string {
	return userID.String() + ":" + strings.TrimSpace(deviceID)
}

func (e *Engine) Session(ctx context.Context, kind, owner string, userID *uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := e.pool.QueryRow(ctx, `SELECT id FROM playback_sessions WHERE kind=$1 AND owner_key=$2`, kind, owner).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != pgx.ErrNoRows {
		return uuid.Nil, err
	}
	err = e.pool.QueryRow(ctx, `INSERT INTO playback_sessions (kind, owner_key, user_id) VALUES ($1,$2,$3) RETURNING id`, kind, owner, userID).Scan(&id)
	return id, err
}

func (e *Engine) knownUserID(ctx context.Context, userID uuid.UUID) any {
	if userID == uuid.Nil {
		return nil
	}
	var ok bool
	if err := e.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, userID).Scan(&ok); err != nil || !ok {
		return nil
	}
	return userID
}

func (e *Engine) WebSession(ctx context.Context, userID uuid.UUID, deviceID string) (uuid.UUID, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		deviceID = "web"
	}
	key := WebOwnerKey(userID, deviceID)
	uid := e.knownUserID(ctx, userID)
	m := e.lock("web-migrate:" + userID.String())
	m.Lock()
	defer m.Unlock()

	var id uuid.UUID
	err := e.pool.QueryRow(ctx, `SELECT id FROM playback_sessions WHERE kind='web_device' AND owner_key=$1`, key).Scan(&id)
	if err == nil {
		_, _ = e.pool.Exec(ctx, `UPDATE playback_sessions SET device_id=$2, last_device=$2, user_id=coalesce($3, user_id), updated_at=now() WHERE id=$1 AND kind='web_device'`, id, deviceID, uid)
		return id, nil
	}
	if err != pgx.ErrNoRows {
		return uuid.Nil, err
	}

	legacy := userID.String()
	err = e.pool.QueryRow(ctx, `SELECT id FROM playback_sessions WHERE kind='web_device' AND owner_key=$1`, legacy).Scan(&id)
	if err == nil {
		_, err = e.pool.Exec(ctx, `
			UPDATE playback_sessions
			SET owner_key=$2, device_id=$3, last_device=$3, user_id=coalesce($4, user_id), updated_at=now()
			WHERE id=$1 AND kind='web_device' AND owner_key=$5`,
			id, key, deviceID, uid, legacy)
		return id, err
	}
	if err != pgx.ErrNoRows {
		return uuid.Nil, err
	}

	err = e.pool.QueryRow(ctx, `
		INSERT INTO playback_sessions (kind, owner_key, user_id, device_id, last_device)
		VALUES ('web_device',$1,$2,$3,$3) RETURNING id`, key, uid, deviceID).Scan(&id)
	return id, err
}
