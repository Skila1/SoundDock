package discordx

import (
	"context"
	"errors"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/oplog"
)

// ServerScope is what a Discord server is allowed to use.
type ServerScope struct {
	Banned      bool
	SharedQueue bool
	// Restricted is true when only Libraries may be searched and played.
	Restricted bool
	Libraries  []uuid.UUID
}

// LoadServerScope reads a server's queue mode, library limit and ban. An
// unknown server is unrestricted with personal queues, as before this existed.
func LoadServerScope(ctx context.Context, pool *pgxpool.Pool, guildID string) ServerScope {
	var sc ServerScope
	if pool == nil || guildID == "" {
		return sc
	}
	var queueMode, libMode string
	var banned *time.Time
	if err := pool.QueryRow(ctx, `SELECT queue_mode, library_mode, banned_at FROM discord_guilds WHERE id=$1`, guildID).
		Scan(&queueMode, &libMode, &banned); err != nil {
		return sc
	}
	sc.Banned = banned != nil
	sc.SharedQueue = queueMode == "shared"
	if libMode != "selected" {
		return sc
	}
	sc.Restricted = true
	rows, err := pool.Query(ctx, `SELECT library_id FROM discord_guild_libraries WHERE guild_id=$1`, guildID)
	if err != nil {
		return sc
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			sc.Libraries = append(sc.Libraries, id)
		}
	}
	return sc
}

// SearchLibraries is the library filter for search and play in this server:
// nil means every library. A server limited to no libraries gets a filter
// that matches nothing, since an empty list means "all" to the search engine.
func (sc ServerScope) SearchLibraries() []uuid.UUID {
	if !sc.Restricted {
		return nil
	}
	if len(sc.Libraries) == 0 {
		return []uuid.UUID{uuid.Nil}
	}
	return sc.Libraries
}

// Allows reports whether a song from library may play in this server.
func (sc ServerScope) Allows(library uuid.UUID) bool {
	if !sc.Restricted {
		return true
	}
	for _, id := range sc.Libraries {
		if id == library {
			return true
		}
	}
	return false
}

// ServerInfo is what the bot can see about a server it is in.
type ServerInfo struct {
	ID          string
	Name        string
	Icon        string
	OwnerID     string
	OwnerName   string
	MemberCount int
	Permissions int64
}

var ErrBotOffline = errors.New("the Discord bot is not connected")

// Online is true while the bot has a gateway session.
func (b *Bot) Online() bool { return b != nil && b.session() != nil }

// FetchServer asks Discord for a server's owner, member count and the bot's
// permissions there.
func (b *Bot) FetchServer(ctx context.Context, guildID string) (ServerInfo, error) {
	s := b.session()
	if s == nil {
		return ServerInfo{}, ErrBotOffline
	}
	g, err := s.GuildWithCounts(guildID, discordgo.WithContext(ctx))
	if err != nil {
		return ServerInfo{}, err
	}
	info := ServerInfo{ID: g.ID, Name: g.Name, Icon: g.Icon, OwnerID: g.OwnerID, MemberCount: g.ApproximateMemberCount}
	if info.MemberCount == 0 {
		info.MemberCount = g.MemberCount
	}
	if g.OwnerID != "" {
		if u, err := s.User(g.OwnerID, discordgo.WithContext(ctx)); err == nil && u != nil {
			info.OwnerName = u.Username
		}
	}
	if s.State != nil && s.State.User != nil {
		if m, err := s.GuildMember(guildID, s.State.User.ID, discordgo.WithContext(ctx)); err == nil && m != nil {
			info.Permissions = guildPermissions(g, s.State.User.ID, m.Roles)
		}
	}
	return info, nil
}

// guildPermissions is the server-wide permission set of a member: the owner
// has all of them, otherwise @everyone plus each of their roles.
func guildPermissions(g *discordgo.Guild, userID string, roles []string) int64 {
	if g.OwnerID == userID {
		return discordgo.PermissionAll
	}
	var p int64
	for _, r := range g.Roles {
		if r.ID == g.ID {
			p |= r.Permissions
		}
		for _, id := range roles {
			if r.ID == id {
				p |= r.Permissions
			}
		}
	}
	if p&discordgo.PermissionAdministrator != 0 {
		return discordgo.PermissionAll
	}
	return p
}

// RefreshServer stores the latest owner, member count and permissions.
func (b *Bot) RefreshServer(ctx context.Context, guildID string) (ServerInfo, error) {
	info, err := b.FetchServer(ctx, guildID)
	if err != nil {
		return info, err
	}
	_, err = b.pool.Exec(ctx, `
		INSERT INTO discord_guilds (id, name, icon, owner_id, owner_name, member_count, bot_permissions, info_refreshed_at, joined_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,now(),now())
		ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name, icon=EXCLUDED.icon, owner_id=EXCLUDED.owner_id,
			owner_name=CASE WHEN EXCLUDED.owner_name='' THEN discord_guilds.owner_name ELSE EXCLUDED.owner_name END,
			member_count=EXCLUDED.member_count, bot_permissions=EXCLUDED.bot_permissions, info_refreshed_at=now()`,
		guildID, info.Name, info.Icon, info.OwnerID, info.OwnerName, info.MemberCount, info.Permissions)
	return info, err
}

// LeaveServer drops voice and removes the bot from the server. The bot can be
// invited back unless the server is banned.
func (b *Bot) LeaveServer(ctx context.Context, guildID string) error {
	_ = b.LeaveGuild(ctx, guildID)
	s := b.session()
	if s == nil {
		return ErrBotOffline
	}
	if err := s.GuildLeave(guildID, discordgo.WithContext(ctx)); err != nil {
		return err
	}
	_, _ = b.pool.Exec(ctx, `UPDATE discord_guilds SET left_at=now() WHERE id=$1`, guildID)
	return nil
}

func (b *Bot) serverBanned(ctx context.Context, guildID string) bool {
	var banned bool
	_ = b.pool.QueryRow(ctx, `SELECT banned_at IS NOT NULL FROM discord_guilds WHERE id=$1`, guildID).Scan(&banned)
	return banned
}

// leaveBanned removes the bot from a banned server it was invited back to.
func (b *Bot) leaveBanned(ctx context.Context, guildID, name string) {
	err := b.LeaveServer(ctx, guildID)
	res := oplog.ResultSuccess
	msg := "Left banned Discord server " + name
	if err != nil {
		res = oplog.ResultFailure
		msg = "Could not leave banned Discord server " + name
	}
	oplog.Emit(ctx, oplog.Entry{Level: levelFor(err), Category: "discord", Action: "discord.server.banned_rejoin",
		Message: msg, Result: res, Error: errText(err), Details: map[string]any{"guild_id": guildID, "guild_name": name}})
}

func levelFor(err error) string {
	if err != nil {
		return "warn"
	}
	return "info"
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
