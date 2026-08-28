package auth

import (
	"strings"
	"testing"
)

func TestDiscordAvatarURLUsesHash(t *testing.T) {
	if DiscordAvatarURL("", "abc") != "" {
		t.Fatal("empty id")
	}
	if got := DiscordAvatarURL("99", "facehash"); got != "https://cdn.discordapp.com/avatars/99/facehash.png?size=80" {
		t.Fatal(got)
	}
	if got := DiscordAvatarURL("99", "a_anim"); got != "https://cdn.discordapp.com/avatars/99/a_anim.gif?size=80" {
		t.Fatal(got)
	}
	if got := DiscordAvatarURL("123", ""); !strings.Contains(got, "embed/avatars/") {
		t.Fatal(got)
	}
}
