package playback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// PartyPermissions is what guests may do on the host's playback. DJs and the
// host may always do all of it.
type PartyPermissions struct {
	Add   bool `json:"add"`
	Vote  bool `json:"vote"`
	Skip  bool `json:"skip"`
	Pause bool `json:"pause"`
}

func DefaultPartyPermissions() PartyPermissions {
	return PartyPermissions{Add: true, Vote: true}
}

var ErrPartyForbidden = errors.New("the party host has not allowed this")

func (e *Engine) partyPermissions(ctx context.Context, sid uuid.UUID) PartyPermissions {
	p := DefaultPartyPermissions()
	var raw []byte
	if err := e.pool.QueryRow(ctx, `SELECT party_permissions FROM playback_sessions WHERE id=$1`, sid).Scan(&raw); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}
	return p
}

// SetPartyPermissions lets the host change what guests may do.
func (e *Engine) SetPartyPermissions(ctx context.Context, sid, actor uuid.UUID, p PartyPermissions) error {
	if !e.isPartyHost(ctx, sid, actor) {
		return fmt.Errorf("only the party host can change permissions")
	}
	b, _ := json.Marshal(p)
	_, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET party_permissions=$2::jsonb, updated_at=now() WHERE id=$1`, sid, b)
	return err
}

// SetPartyMemberRole promotes a guest to DJ or back. Hosts cannot be changed.
func (e *Engine) SetPartyMemberRole(ctx context.Context, sid, actor, member uuid.UUID, role string) error {
	if !e.isPartyHost(ctx, sid, actor) {
		return fmt.Errorf("only the party host can change roles")
	}
	if role != "guest" && role != "dj" {
		return fmt.Errorf("role must be guest or dj")
	}
	tag, err := e.pool.Exec(ctx, `UPDATE party_members SET role=$3 WHERE session_id=$1 AND user_id=$2 AND role <> 'host'`, sid, member, role)
	if err == nil && tag.RowsAffected() == 0 {
		return fmt.Errorf("member not found")
	}
	return err
}

// RemovePartyMember removes someone from the party (host only).
func (e *Engine) RemovePartyMember(ctx context.Context, sid, actor, member uuid.UUID) error {
	if !e.isPartyHost(ctx, sid, actor) {
		return fmt.Errorf("only the party host can remove people")
	}
	_, err := e.pool.Exec(ctx, `DELETE FROM party_members WHERE session_id=$1 AND user_id=$2 AND role <> 'host'`, sid, member)
	if err == nil {
		_, _ = e.pool.Exec(ctx, `DELETE FROM party_votes WHERE session_id=$1 AND user_id=$2`, sid, member)
	}
	return err
}

func (e *Engine) isPartyHost(ctx context.Context, sid, actor uuid.UUID) bool {
	var host *uuid.UUID
	var enabled bool
	var exp *time.Time
	if err := e.pool.QueryRow(ctx, `SELECT party_host_user_id, party_enabled, party_expires_at FROM playback_sessions WHERE id=$1`, sid).Scan(&host, &enabled, &exp); err != nil {
		return false
	}
	return host != nil && *host == actor && partyActive(enabled, exp, time.Now())
}

// PartyAllowed reports whether actor may do action ("add", "vote", "skip",
// "pause") on the party session sid.
func (e *Engine) PartyAllowed(ctx context.Context, sid, actor uuid.UUID, action string) error {
	var enabled bool
	var exp *time.Time
	var host *uuid.UUID
	if err := e.pool.QueryRow(ctx, `SELECT party_enabled, party_expires_at, party_host_user_id FROM playback_sessions WHERE id=$1`, sid).Scan(&enabled, &exp, &host); err != nil {
		return err
	}
	if !partyActive(enabled, exp, time.Now()) {
		return fmt.Errorf("party inactive")
	}
	if host != nil && *host == actor {
		return nil
	}
	var role string
	if err := e.pool.QueryRow(ctx, `SELECT role FROM party_members WHERE session_id=$1 AND user_id=$2`, sid, actor).Scan(&role); err != nil {
		return fmt.Errorf("join the party first")
	}
	if role == "dj" || role == "host" {
		return nil
	}
	p := e.partyPermissions(ctx, sid)
	allowed := map[string]bool{"add": p.Add, "vote": p.Vote, "skip": p.Skip, "pause": p.Pause}[action]
	if !allowed {
		return ErrPartyForbidden
	}
	return nil
}
