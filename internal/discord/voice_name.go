package discordx

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

func memberDisplayName(m *discordgo.Member) string {
	if m == nil {
		return ""
	}
	if n := strings.TrimSpace(m.Nick); n != "" {
		return n
	}
	if m.User != nil {
		if n := strings.TrimSpace(m.User.DisplayName()); n != "" {
			return n
		}
		if n := strings.TrimSpace(m.User.Username); n != "" {
			return n
		}
	}
	return ""
}

func voiceDisplayName(vs *discordgo.VoiceState) string {
	if vs == nil {
		return ""
	}
	return memberDisplayName(vs.Member)
}

func (b *Bot) resolveVoiceName(s *discordgo.Session, guildID string, vs *discordgo.VoiceState, fetch bool) string {
	if n := voiceDisplayName(vs); n != "" {
		return n
	}
	if s == nil || vs == nil || vs.UserID == "" {
		return ""
	}
	if s.State != nil {
		if m, err := s.State.Member(guildID, vs.UserID); err == nil {
			if n := memberDisplayName(m); n != "" {
				return n
			}
		}
	}
	if !fetch {
		return ""
	}
	m, err := s.GuildMember(guildID, vs.UserID)
	if err != nil {
		return ""
	}
	return memberDisplayName(m)
}

// MemberDisplayName is the guild nick or Discord display name from gateway state.
func (b *Bot) MemberDisplayName(guildID, userID string) string {
	if b == nil || guildID == "" || userID == "" {
		return ""
	}
	s := b.session()
	if s == nil || s.State == nil {
		return ""
	}
	if m, err := s.State.Member(guildID, userID); err == nil {
		if n := memberDisplayName(m); n != "" {
			return n
		}
	}
	if g, err := s.State.Guild(guildID); err == nil && g != nil {
		for _, vs := range g.VoiceStates {
			if vs != nil && vs.UserID == userID {
				if n := voiceDisplayName(vs); n != "" {
					return n
				}
			}
		}
	}
	return ""
}
