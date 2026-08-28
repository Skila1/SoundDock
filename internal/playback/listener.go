package playback

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// ListenerSnap is enough to decide autoplay YouTube fill. SSE presence is
// intentionally absent - avatars are not audio listeners.
type ListenerSnap struct {
	RendererKind  string
	Status        string
	RendererID    string
	DiscordHumans int
}

// HasAudioListener reports a real decoder: a playing browser lease, or a
// Discord lease with at least one human in the bound bot VC.
func HasAudioListener(in ListenerSnap) bool {
	kind := strings.ToLower(strings.TrimSpace(in.RendererKind))
	status := strings.ToLower(strings.TrimSpace(in.Status))
	lease := strings.TrimSpace(in.RendererID) != "" && status == "playing"
	switch kind {
	case RendererBrowser:
		return lease
	case RendererDiscord:
		return lease && in.DiscordHumans > 0
	default:
		return false
	}
}

// HasAudioListener loads the session row and bound-VC human count.
func (e *Engine) HasAudioListener(ctx context.Context, sessionID uuid.UUID) (bool, error) {
	if e == nil || e.pool == nil || sessionID == uuid.Nil {
		return false, nil
	}
	var kind, status, rid string
	err := e.pool.QueryRow(ctx, `
		SELECT coalesce(renderer_kind,'none'), coalesce(status,''), coalesce(renderer_id,'')
		FROM playback_sessions WHERE id=$1`, sessionID).Scan(&kind, &status, &rid)
	if err != nil {
		return false, err
	}
	var humans int
	_ = e.pool.QueryRow(ctx, `
		SELECT count(*) FROM discord_user_voice v
		JOIN discord_voice_runtime r ON r.guild_id = v.guild_id
		WHERE r.session_id=$1
		  AND coalesce(r.voice_channel_id,'') <> ''
		  AND v.channel_id = r.voice_channel_id`, sessionID).Scan(&humans)
	return HasAudioListener(ListenerSnap{
		RendererKind:  kind,
		Status:        status,
		RendererID:    rid,
		DiscordHumans: humans,
	}), nil
}
