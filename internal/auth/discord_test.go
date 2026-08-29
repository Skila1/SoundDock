package auth

import (
	"context"
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
	if got := DiscordLoginScope(DiscordRegistration{GuildEnabled: true}); got != "identify guilds" {
		t.Fatalf("got %q", got)
	}
	if got := DiscordLoginScope(DiscordRegistration{RoleEnabled: true}); got != "identify guilds guilds.members.read" {
		t.Fatalf("got %q", got)
	}
}

func TestCheckDiscordRegistrationOff(t *testing.T) {
	if err := CheckDiscordRegistration(context.Background(), "", DiscordRegistration{}); err != nil {
		t.Fatal(err)
	}
	if err := CheckDiscordRegistration(context.Background(), "", DiscordRegistration{GuildEnabled: true}); err == nil {
		t.Fatal("empty guild id should fail")
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
