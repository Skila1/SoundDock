package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/testdb"
)

func TestDiscordProfileNames(t *testing.T) {
	p := DiscordProfile{ID: "288559247741157386", Username: "skila", Global: "Skila"}
	if DiscordDisplayName(p) != "Skila" {
		t.Fatalf("display %q", DiscordDisplayName(p))
	}
	if DiscordAccountUsername(p) != "skila" {
		t.Fatalf("username %q", DiscordAccountUsername(p))
	}
	if !isDiscordStubUsername("discord_288559247741157386", p.ID) {
		t.Fatal("expected stub")
	}
	if !isDiscordStubUsername(p.ID, p.ID) {
		t.Fatal("raw discord id is a stub")
	}
	if isDiscordStubUsername("Skila", p.ID) {
		t.Fatal("local admin is not a stub")
	}
	empty := DiscordProfile{ID: "288559247741157386"}
	if DiscordAccountUsername(empty) != "288559247741157386" || DiscordDisplayName(empty) != "288559247741157386" {
		t.Fatal("empty profile fallback")
	}
}

func TestIsAdminID(t *testing.T) {
	ids := []string{"123", "456"}
	if !IsAdminDiscordID("123", ids) {
		t.Fatal("expected admin")
	}
	if IsAdminDiscordID("999", ids) {
		t.Fatal("not admin")
	}
}

func TestDiscordLoginScope(t *testing.T) {
	if got := DiscordLoginScope(DiscordRegistration{}); got != "identify" {
		t.Fatalf("got %q", got)
	}
	servers := []DiscordRegistrationGuild{{GuildID: "1"}, {GuildID: "2"}}
	if got := DiscordLoginScope(DiscordRegistration{Enabled: true, Guilds: servers}); got != "identify guilds" {
		t.Fatalf("got %q", got)
	}
	servers[1].RoleIDs = []string{"20"}
	if got := DiscordLoginScope(DiscordRegistration{Enabled: true, Guilds: servers}); got != "identify guilds guilds.members.read" {
		t.Fatalf("got %q", got)
	}
	if got := DiscordLoginScope(DiscordRegistration{Guilds: servers}); got != "identify" {
		t.Fatalf("whitelist off still asked for guild scopes: %q", got)
	}
}

type fakeMembership struct {
	guilds  []string
	roles   map[string][]string
	roleErr map[string]error
	asked   []string
}

func (f *fakeMembership) GuildIDs(context.Context) ([]string, error) { return f.guilds, nil }

func (f *fakeMembership) MemberRoles(_ context.Context, guildID string) ([]string, error) {
	f.asked = append(f.asked, guildID)
	if err := f.roleErr[guildID]; err != nil {
		return nil, err
	}
	return f.roles[guildID], nil
}

func TestCheckDiscordRegistrationOff(t *testing.T) {
	if err := CheckDiscordRegistration(context.Background(), "", DiscordRegistration{}); err != nil {
		t.Fatal(err)
	}
	if err := CheckDiscordRegistration(context.Background(), "", DiscordRegistration{Enabled: true}); !errors.Is(err, ErrNotInServer) {
		t.Fatalf("empty whitelist should deny, got %v", err)
	}
}

func TestCheckDiscordRegistrationMultipleServers(t *testing.T) {
	ctx := context.Background()
	reg := DiscordRegistration{Enabled: true, Guilds: []DiscordRegistrationGuild{
		{GuildID: "100", RoleIDs: []string{"101", "102"}},
		{GuildID: "200"},
		{GuildID: "300", RoleIDs: []string{"301"}},
	}}
	cases := []struct {
		name string
		m    *fakeMembership
		want error
	}{
		{"in no listed server", &fakeMembership{guilds: []string{"999"}}, ErrNotInServer},
		{"open server needs no role", &fakeMembership{guilds: []string{"200"}}, nil},
		{"any of the server roles", &fakeMembership{guilds: []string{"100"}, roles: map[string][]string{"100": {"5", "102"}}}, nil},
		{"role from another server does not count", &fakeMembership{guilds: []string{"100"}, roles: map[string][]string{"100": {"301"}}}, ErrMissingRole},
		{"second role server passes", &fakeMembership{guilds: []string{"100", "300"}, roles: map[string][]string{"100": {"7"}, "300": {"301"}}}, nil},
		{"member lookup 404 counts as missing role", &fakeMembership{guilds: []string{"300"}, roleErr: map[string]error{"300": ErrMissingRole}}, ErrMissingRole},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := checkRegistration(ctx, reg, tc.m); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCheckDiscordRegistrationLookupFailureIsNotADenial(t *testing.T) {
	reg := DiscordRegistration{Enabled: true, Guilds: []DiscordRegistrationGuild{{GuildID: "100", RoleIDs: []string{"101"}}}}
	boom := errors.New("discord member: 502 Bad Gateway")
	err := checkRegistration(context.Background(), reg, &fakeMembership{guilds: []string{"100"}, roleErr: map[string]error{"100": boom}})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}

func TestCheckDiscordRegistrationSkipsRoleLookupWhenOpenServerMatches(t *testing.T) {
	reg := DiscordRegistration{Enabled: true, Guilds: []DiscordRegistrationGuild{{GuildID: "200"}, {GuildID: "100", RoleIDs: []string{"101"}}}}
	m := &fakeMembership{guilds: []string{"100", "200"}}
	if err := checkRegistration(context.Background(), reg, m); err != nil {
		t.Fatal(err)
	}
	if len(m.asked) != 0 {
		t.Fatalf("asked Discord for roles in %v", m.asked)
	}
}

func TestNormalizeDiscordRegistrationGuilds(t *testing.T) {
	got, err := NormalizeDiscordRegistrationGuilds([]DiscordRegistrationGuild{
		{GuildID: " 100 ", Label: "  Main  ", RoleIDs: []string{" 1 ", "", "1", "2"}},
		{GuildID: "200"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].GuildID != "100" || got[0].Label != "Main" || len(got[0].RoleIDs) != 2 {
		t.Fatalf("got %#v", got)
	}
	if got[1].RoleIDs == nil {
		t.Fatal("role IDs must be an empty slice so the column stays NOT NULL")
	}
	bad := [][]DiscordRegistrationGuild{
		{{GuildID: "abc"}},
		{{GuildID: ""}},
		{{GuildID: "100"}, {GuildID: "100"}},
		{{GuildID: "100", RoleIDs: []string{"x1"}}},
	}
	for _, in := range bad {
		if _, err := NormalizeDiscordRegistrationGuilds(in); err == nil {
			t.Fatalf("expected error for %#v", in)
		}
	}
}

func TestDiscordRegistrationRoundTrip(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	prev, err := LoadDiscordRegistration(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = SaveDiscordRegistration(context.Background(), pool, prev) })
	want := DiscordRegistration{Enabled: true, Guilds: []DiscordRegistrationGuild{
		{GuildID: "300", Label: "Second", RoleIDs: []string{"301", "302"}},
		{GuildID: "100", Label: "First", RoleIDs: []string{}},
	}}
	if err := SaveDiscordRegistration(ctx, pool, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadDiscordRegistration(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || len(got.Guilds) != 2 || got.Guilds[0].GuildID != "300" || len(got.Guilds[0].RoleIDs) != 2 || got.Guilds[1].Label != "First" {
		t.Fatalf("got %#v", got)
	}
}

func TestNormalizeAdminDiscordIDs(t *testing.T) {
	got, err := NormalizeAdminDiscordIDs([]string{" 123 ", "456,123", "789"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "123" || got[1] != "456" || got[2] != "789" {
		t.Fatalf("got %#v", got)
	}
	if _, err := NormalizeAdminDiscordIDs([]string{"abc"}); err == nil {
		t.Fatal("expected invalid")
	}
}

func TestUpsertDiscordUserDoesNotStealAdmin(t *testing.T) {
	pool := testdb.Open(t)
	svc := New(pool)
	ctx := context.Background()
	adminID := uuid.New()
	uname := "adm-" + adminID.String()[:8]
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`, adminID, uname); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE name='Administrator'`, adminID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM user_identities WHERE user_id IN (SELECT id FROM users WHERE username=$1 OR username LIKE $2)`, uname, "friend-%")
		_, _ = pool.Exec(c, `DELETE FROM user_roles WHERE user_id=$1`, adminID)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id=$1 OR username LIKE $2`, adminID, "friend-%")
	})
	did := "9" + adminID.String()[:17]
	friend, err := svc.UpsertDiscordUser(ctx, DiscordProfile{ID: did, Username: "friend-" + adminID.String()[:8], Global: "Friend"})
	if err != nil {
		t.Fatal(err)
	}
	if friend.ID == adminID {
		t.Fatal("second Discord login attached to the existing administrator")
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_identities WHERE user_id=$1 AND provider='discord'`, adminID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("admin gained a Discord identity from someone else's login")
	}
	again, err := svc.UpsertDiscordUser(ctx, DiscordProfile{ID: did, Username: "friend-" + adminID.String()[:8], Global: "Friend"})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != friend.ID {
		t.Fatalf("same Discord user created a second local account %s %s", again.ID, friend.ID)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM user_identities WHERE user_id=$1`, friend.ID)
		_, _ = pool.Exec(c, `DELETE FROM user_roles WHERE user_id=$1`, friend.ID)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id=$1`, friend.ID)
	})
}
