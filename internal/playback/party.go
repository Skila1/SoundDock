package playback

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func partyActive(enabled bool, expires *time.Time, now time.Time) bool {
	if !enabled {
		return false
	}
	if expires != nil && !expires.After(now) {
		return false
	}
	return true
}

type PartyState struct {
	SessionID  uuid.UUID        `json:"session_id"`
	Enabled    bool             `json:"enabled"`
	HostUserID *uuid.UUID       `json:"host_user_id"`
	ExpiresAt  *time.Time       `json:"expires_at"`
	Members    []map[string]any `json:"members"`
	Votes      []map[string]any `json:"votes"`
}

func (e *Engine) GetParty(ctx context.Context, sid uuid.UUID) (PartyState, error) {
	st := PartyState{SessionID: sid, Members: []map[string]any{}, Votes: []map[string]any{}}
	var enabled bool
	var host *uuid.UUID
	var exp *time.Time
	err := e.pool.QueryRow(ctx, `SELECT party_enabled, party_host_user_id, party_expires_at FROM playback_sessions WHERE id=$1`, sid).
		Scan(&enabled, &host, &exp)
	if err != nil {
		return st, err
	}
	if !partyActive(enabled, exp, time.Now()) {
		if enabled {
			_ = e.ExpireParty(ctx, sid)
		}
		st.Enabled = false
		st.ExpiresAt = exp
		return st, nil
	}
	st.Enabled = true
	st.HostUserID = host
	st.ExpiresAt = exp
	mrows, err := e.pool.Query(ctx, `SELECT user_id, role FROM party_members WHERE session_id=$1 ORDER BY joined_at`, sid)
	if err != nil {
		return st, err
	}
	defer mrows.Close()
	for mrows.Next() {
		var uid uuid.UUID
		var role string
		if err := mrows.Scan(&uid, &role); err != nil {
			return st, err
		}
		st.Members = append(st.Members, map[string]any{"user_id": uid, "role": role})
	}
	vrows, err := e.pool.Query(ctx, `SELECT user_id, track_id, created_at FROM party_votes WHERE session_id=$1 ORDER BY created_at`, sid)
	if err != nil {
		return st, err
	}
	defer vrows.Close()
	for vrows.Next() {
		var uid, tid uuid.UUID
		var at time.Time
		if err := vrows.Scan(&uid, &tid, &at); err != nil {
			return st, err
		}
		st.Votes = append(st.Votes, map[string]any{"user_id": uid, "track_id": tid, "created_at": at})
	}
	if st.Members == nil {
		st.Members = []map[string]any{}
	}
	if st.Votes == nil {
		st.Votes = []map[string]any{}
	}
	return st, nil
}

func (e *Engine) sessionUser(ctx context.Context, sid uuid.UUID) (*uuid.UUID, error) {
	var uid *uuid.UUID
	err := e.pool.QueryRow(ctx, `SELECT user_id FROM playback_sessions WHERE id=$1`, sid).Scan(&uid)
	return uid, err
}

func (e *Engine) EnableParty(ctx context.Context, sid, host uuid.UUID, expiresIn time.Duration) (time.Time, error) {
	m := e.lock(sid.String())
	m.Lock()
	defer m.Unlock()
	owner, err := e.sessionUser(ctx, sid)
	if err != nil {
		return time.Time{}, err
	}
	if owner == nil || *owner != host {
		return time.Time{}, fmt.Errorf("forbidden")
	}
	if expiresIn <= 0 {
		expiresIn = time.Hour
	}
	exp := time.Now().Add(expiresIn)
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `
		UPDATE playback_sessions
		SET party_enabled=true, party_expires_at=$2, party_host_user_id=$3, updated_at=now()
		WHERE id=$1`, sid, exp, host)
	if err != nil {
		return time.Time{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM party_votes WHERE session_id=$1`, sid); err != nil {
		return time.Time{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO party_members (session_id, user_id, role) VALUES ($1,$2,'host')
		ON CONFLICT (session_id, user_id) DO UPDATE SET role='host'`, sid, host); err != nil {
		return time.Time{}, err
	}
	return exp, tx.Commit(ctx)
}

func (e *Engine) DisableParty(ctx context.Context, sid, actor uuid.UUID) error {
	m := e.lock(sid.String())
	m.Lock()
	defer m.Unlock()
	var host *uuid.UUID
	if err := e.pool.QueryRow(ctx, `SELECT party_host_user_id FROM playback_sessions WHERE id=$1`, sid).Scan(&host); err != nil {
		return err
	}
	if host == nil || *host != actor {
		return fmt.Errorf("forbidden")
	}
	return e.clearParty(ctx, sid)
}

func (e *Engine) LeaveParty(ctx context.Context, sid, actor uuid.UUID) error {
	m := e.lock(sid.String())
	m.Lock()
	defer m.Unlock()
	var host *uuid.UUID
	_ = e.pool.QueryRow(ctx, `SELECT party_host_user_id FROM playback_sessions WHERE id=$1`, sid).Scan(&host)
	if host != nil && *host == actor {
		return e.clearParty(ctx, sid)
	}
	_, err := e.pool.Exec(ctx, `DELETE FROM party_members WHERE session_id=$1 AND user_id=$2`, sid, actor)
	if err != nil {
		return err
	}
	_, err = e.pool.Exec(ctx, `DELETE FROM party_votes WHERE session_id=$1 AND user_id=$2`, sid, actor)
	return err
}

func (e *Engine) JoinParty(ctx context.Context, sid, actor uuid.UUID) error {
	m := e.lock(sid.String())
	m.Lock()
	defer m.Unlock()
	var enabled bool
	var exp *time.Time
	var host *uuid.UUID
	if err := e.pool.QueryRow(ctx, `SELECT party_enabled, party_expires_at, party_host_user_id FROM playback_sessions WHERE id=$1`, sid).
		Scan(&enabled, &exp, &host); err != nil {
		return err
	}
	if !partyActive(enabled, exp, time.Now()) {
		return fmt.Errorf("party inactive")
	}
	role := "guest"
	if host != nil && *host == actor {
		role = "host"
	}
	_, err := e.pool.Exec(ctx, `
		INSERT INTO party_members (session_id, user_id, role) VALUES ($1,$2,$3)
		ON CONFLICT (session_id, user_id) DO UPDATE SET role=EXCLUDED.role`, sid, actor, role)
	return err
}

func (e *Engine) Vote(ctx context.Context, sid, actor, track uuid.UUID) error {
	m := e.lock(sid.String())
	m.Lock()
	defer m.Unlock()
	var enabled bool
	var exp *time.Time
	if err := e.pool.QueryRow(ctx, `SELECT party_enabled, party_expires_at FROM playback_sessions WHERE id=$1`, sid).
		Scan(&enabled, &exp); err != nil {
		return err
	}
	if !partyActive(enabled, exp, time.Now()) {
		return fmt.Errorf("party inactive")
	}
	var n int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM party_members WHERE session_id=$1 AND user_id=$2`, sid, actor).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := e.pool.Exec(ctx, `
			INSERT INTO party_members (session_id, user_id, role) VALUES ($1,$2,'guest')
			ON CONFLICT (session_id, user_id) DO NOTHING`, sid, actor); err != nil {
			return err
		}
	}
	if _, err := e.pool.Exec(ctx, `DELETE FROM party_votes WHERE session_id=$1 AND user_id=$2 AND track_id=$3`, sid, actor, track); err != nil {
		return err
	}
	_, err := e.pool.Exec(ctx, `INSERT INTO party_votes (session_id, user_id, track_id) VALUES ($1,$2,$3)`, sid, actor, track)
	return err
}

func (e *Engine) ExpireParty(ctx context.Context, sid uuid.UUID) error {
	m := e.lock(sid.String())
	m.Lock()
	defer m.Unlock()
	var enabled bool
	var exp *time.Time
	err := e.pool.QueryRow(ctx, `SELECT party_enabled, party_expires_at FROM playback_sessions WHERE id=$1`, sid).Scan(&enabled, &exp)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if partyActive(enabled, exp, time.Now()) {
		return nil
	}
	return e.clearParty(ctx, sid)
}

func (e *Engine) FindPartyForUser(ctx context.Context, userID uuid.UUID) (uuid.UUID, bool, error) {
	var sid uuid.UUID
	err := e.pool.QueryRow(ctx, `
		SELECT s.id FROM playback_sessions s
		JOIN party_members m ON m.session_id=s.id
		WHERE m.user_id=$1 AND s.party_enabled=true
			AND (s.party_expires_at IS NULL OR s.party_expires_at > now())
		ORDER BY s.updated_at DESC
		LIMIT 1`, userID).Scan(&sid)
	if err == pgx.ErrNoRows {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	return sid, true, nil
}

// clearParty requires the session lock to already be held.
func (e *Engine) clearParty(ctx context.Context, sid uuid.UUID) error {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM party_votes WHERE session_id=$1`, sid); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM party_members WHERE session_id=$1`, sid); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE playback_sessions
		SET party_enabled=false, party_host_user_id=NULL, updated_at=now()
		WHERE id=$1`, sid); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
