package playback

import (
	"context"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// BindResult is returned by BindDiscordRenderer / UnbindDiscordRenderer / BindGuildSession.
type BindResult struct {
	BindingRevision int64     `json:"binding_revision"`
	SessionID       uuid.UUID `json:"session_id"`
	StateRevision   int64     `json:"state_revision"`
}

func sameVoiceChannel(prev *string, want string) bool {
	if prev == nil {
		return want == ""
	}
	return *prev == want
}

func sessionStateRevision(ctx context.Context, q db, sessionID uuid.UUID) int64 {
	var rev int64
	_ = q.QueryRow(ctx, `SELECT state_revision FROM playback_sessions WHERE id=$1`, sessionID).Scan(&rev)
	return rev
}

// BindGuildSession binds a guild voice runtime row to sessionID and channel.
// It does not grant a renderer lease. Same session+channel is a no-op and
// does not bump binding_revision or state_revision.
func (e *Engine) BindGuildSession(ctx context.Context, guildID string, sessionID uuid.UUID, voiceChannelID string, expectedBindingRevision int64) (BindResult, error) {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return BindResult{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `INSERT INTO discord_guilds (id) VALUES ($1) ON CONFLICT DO NOTHING`, guildID); err != nil {
		return BindResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO discord_voice_runtime (guild_id)
		VALUES ($1) ON CONFLICT (guild_id) DO NOTHING`, guildID); err != nil {
		return BindResult{}, err
	}

	var curRev int64
	var prevSession *uuid.UUID
	var prevCh *string
	err = tx.QueryRow(ctx, `
		SELECT binding_revision, session_id, voice_channel_id
		FROM discord_voice_runtime WHERE guild_id=$1 FOR UPDATE`, guildID).
		Scan(&curRev, &prevSession, &prevCh)
	if err != nil {
		return BindResult{}, err
	}
	if expectedBindingRevision != 0 && expectedBindingRevision != curRev {
		return BindResult{}, ErrBindConflict
	}

	sameSession := prevSession != nil && *prevSession == sessionID
	if sameSession && sameVoiceChannel(prevCh, voiceChannelID) {
		if err := tx.Commit(ctx); err != nil {
			return BindResult{}, err
		}
		return BindResult{BindingRevision: curRev, SessionID: sessionID, StateRevision: sessionStateRevision(ctx, e.pool, sessionID)}, nil
	}

	oldID := uuid.Nil
	if prevSession != nil {
		oldID = *prevSession
	}
	unlock := e.lockSessions(sessionID, oldID)
	defer unlock()
	if err := lockSessionRows(ctx, tx, sessionID, oldID); err != nil {
		return BindResult{}, err
	}

	newRev := curRev + 1
	if _, err := tx.Exec(ctx, `
		UPDATE discord_voice_runtime
		SET binding_revision=$2, session_id=$3, voice_channel_id=$4
		WHERE guild_id=$1`, guildID, newRev, sessionID, voiceChannelID); err != nil {
		return BindResult{}, err
	}

	if oldID != uuid.Nil && oldID != sessionID {
		released, err := casReleaseDiscordIfHeld(ctx, tx, oldID, true)
		if err != nil {
			return BindResult{}, err
		}
		if released {
			if err := bumpRevision(ctx, tx, oldID); err != nil {
				return BindResult{}, err
			}
		}
	}

	stateRev := sessionStateRevision(ctx, tx, sessionID)
	if err := tx.Commit(ctx); err != nil {
		return BindResult{}, err
	}
	return BindResult{BindingRevision: newRev, SessionID: sessionID, StateRevision: stateRev}, nil
}

// BindDiscordRenderer atomically binds a guild voice runtime row to sessionID
// and CAS-grants the Discord renderer lease on that session.
func (e *Engine) BindDiscordRenderer(ctx context.Context, guildID string, sessionID uuid.UUID, voiceChannelID string, expectedBindingRevision int64, discordRendererID string, discordGeneration int64) (BindResult, error) {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return BindResult{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `INSERT INTO discord_guilds (id) VALUES ($1) ON CONFLICT DO NOTHING`, guildID); err != nil {
		return BindResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO discord_voice_runtime (guild_id, voice_channel_id, session_id, connected, last_disconnect_reason)
		VALUES ($1,$2,$3,false,'')
		ON CONFLICT (guild_id) DO NOTHING`, guildID, voiceChannelID, sessionID); err != nil {
		return BindResult{}, err
	}

	var curRev int64
	var prevSession *uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT binding_revision, session_id
		FROM discord_voice_runtime WHERE guild_id=$1 FOR UPDATE`, guildID).
		Scan(&curRev, &prevSession)
	if err != nil {
		return BindResult{}, err
	}
	if expectedBindingRevision != 0 && expectedBindingRevision != curRev {
		return BindResult{}, ErrBindConflict
	}

	oldID := uuid.Nil
	if prevSession != nil {
		oldID = *prevSession
	}
	unlock := e.lockSessions(sessionID, oldID)
	defer unlock()
	if err := lockSessionRows(ctx, tx, sessionID, oldID); err != nil {
		return BindResult{}, err
	}

	newRev := curRev + 1
	if _, err := tx.Exec(ctx, `
		UPDATE discord_voice_runtime
		SET binding_revision=$2, session_id=$3, voice_channel_id=$4
		WHERE guild_id=$1`, guildID, newRev, sessionID, voiceChannelID); err != nil {
		return BindResult{}, err
	}

	if oldID != uuid.Nil && oldID != sessionID {
		released, err := casReleaseDiscordIfHeld(ctx, tx, oldID, true)
		if err != nil {
			return BindResult{}, err
		}
		if released {
			if err := bumpRevision(ctx, tx, oldID); err != nil {
				return BindResult{}, err
			}
		}
	}

	if err := casGrantDiscord(ctx, tx, sessionID, discordRendererID, discordGeneration); err != nil {
		return BindResult{}, err
	}
	if err := bumpRevision(ctx, tx, sessionID); err != nil {
		return BindResult{}, err
	}

	var stateRev int64
	if err := tx.QueryRow(ctx, `SELECT state_revision FROM playback_sessions WHERE id=$1`, sessionID).Scan(&stateRev); err != nil {
		return BindResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return BindResult{}, err
	}
	return BindResult{BindingRevision: newRev, SessionID: sessionID, StateRevision: stateRev}, nil
}

// UnbindDiscordRenderer increments binding_revision, CAS-releases the Discord
// lease if this runtime's session still holds it, and clears session_id/connected.
func (e *Engine) UnbindDiscordRenderer(ctx context.Context, guildID string, expectedBindingRevision int64, discordRendererID string, discordGeneration int64) (BindResult, error) {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return BindResult{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `INSERT INTO discord_guilds (id) VALUES ($1) ON CONFLICT DO NOTHING`, guildID); err != nil {
		return BindResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO discord_voice_runtime (guild_id)
		VALUES ($1) ON CONFLICT (guild_id) DO NOTHING`, guildID); err != nil {
		return BindResult{}, err
	}

	var curRev int64
	var bound *uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT binding_revision, session_id FROM discord_voice_runtime WHERE guild_id=$1 FOR UPDATE`, guildID).
		Scan(&curRev, &bound)
	if err != nil {
		return BindResult{}, err
	}
	if expectedBindingRevision != 0 && expectedBindingRevision != curRev {
		return BindResult{}, ErrBindConflict
	}

	sid := uuid.Nil
	if bound != nil {
		sid = *bound
	}
	unlock := e.lockSessions(sid)
	defer unlock()
	if sid != uuid.Nil {
		if err := lockSessionRow(ctx, tx, sid); err != nil && err != pgx.ErrNoRows {
			return BindResult{}, err
		}
	}

	newRev := curRev + 1
	if _, err := tx.Exec(ctx, `
		UPDATE discord_voice_runtime
		SET binding_revision=$2, session_id=NULL, connected=false, voice_channel_id=NULL, last_disconnect_reason='unbind'
		WHERE guild_id=$1`, guildID, newRev); err != nil {
		return BindResult{}, err
	}

	var stateRev int64
	if sid != uuid.Nil {
		released := false
		if discordRendererID != "" && discordGeneration != 0 {
			ok, err := casRelease(ctx, tx, sid, RendererDiscord, discordRendererID, discordGeneration)
			if err != nil {
				return BindResult{}, err
			}
			released = ok
			if ok {
				_, _ = tx.Exec(ctx, `
					UPDATE playback_sessions
					SET output_pref=CASE WHEN output_pref='discord' THEN 'browser' ELSE output_pref END, updated_at=now()
					WHERE id=$1`, sid)
				if err := bumpRevision(ctx, tx, sid); err != nil {
					return BindResult{}, err
				}
			}
		} else {
			ok, err := casReleaseDiscordIfHeld(ctx, tx, sid, true)
			if err != nil {
				return BindResult{}, err
			}
			released = ok
			if released {
				if err := bumpRevision(ctx, tx, sid); err != nil {
					return BindResult{}, err
				}
			}
		}
		_ = tx.QueryRow(ctx, `SELECT state_revision FROM playback_sessions WHERE id=$1`, sid).Scan(&stateRev)
	}

	if err := tx.Commit(ctx); err != nil {
		return BindResult{}, err
	}
	return BindResult{BindingRevision: newRev, SessionID: sid, StateRevision: stateRev}, nil
}

// SwitchRendererToBrowser CAS-releases a Discord lease held by this session,
// CAS-acquires a browser lease, and sets output_pref=browser.
// It does not touch binding_revision or discord_voice_runtime.session_id.
func (e *Engine) SwitchRendererToBrowser(ctx context.Context, sessionID uuid.UUID, clientRendererID string, generation int64) error {
	unlock := e.lockSessions(sessionID)
	defer unlock()
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := lockSessionRow(ctx, tx, sessionID); err != nil {
		return err
	}
	if _, err := casReleaseDiscordIfHeld(ctx, tx, sessionID, false); err != nil {
		return err
	}
	var gen int64
	if generation != 0 {
		err = tx.QueryRow(ctx, `
			UPDATE playback_sessions
			SET renderer_kind=$2, renderer_id=$3, renderer_generation=$4,
				renderer_heartbeat_at=now(), output_pref=$5, updated_at=now()
			WHERE id=$1 AND renderer_kind IN ('none','browser','discord')
			RETURNING renderer_generation`, sessionID, RendererBrowser, clientRendererID, generation, OutputBrowser).Scan(&gen)
	} else {
		gen, err = casAcquireBrowser(ctx, tx, sessionID, clientRendererID, 0, true)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE playback_sessions SET output_pref=$2, updated_at=now() WHERE id=$1`, sessionID, OutputBrowser)
		}
	}
	if err != nil {
		return ErrLeaseConflict
	}
	_ = gen
	if err := bumpRevision(ctx, tx, sessionID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func lockSessionRows(ctx context.Context, q db, ids ...uuid.UUID) error {
	seen := map[uuid.UUID]struct{}{}
	var uniq []uuid.UUID
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	sort.Slice(uniq, func(i, j int) bool { return uniq[i].String() < uniq[j].String() })
	for _, id := range uniq {
		if err := lockSessionRow(ctx, q, id); err != nil {
			return err
		}
	}
	return nil
}
