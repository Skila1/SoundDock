package auth

import (
	"context"
	"testing"
)

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
