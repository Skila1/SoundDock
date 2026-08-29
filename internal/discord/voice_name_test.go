package discordx

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestMemberDisplayNamePrefersNick(t *testing.T) {
	m := &discordgo.Member{
		Nick: "ServerNick",
		User: &discordgo.User{Username: "user", GlobalName: "Global"},
	}
	if got := memberDisplayName(m); got != "ServerNick" {
		t.Fatalf("got %q", got)
	}
}

func TestMemberDisplayNameUsesGlobalThenUsername(t *testing.T) {
	if got := memberDisplayName(&discordgo.Member{User: &discordgo.User{Username: "user", GlobalName: "Global"}}); got != "Global" {
		t.Fatalf("global %q", got)
	}
	if got := memberDisplayName(&discordgo.Member{User: &discordgo.User{Username: "user"}}); got != "user" {
		t.Fatalf("username %q", got)
	}
	if got := memberDisplayName(nil); got != "" {
		t.Fatalf("nil %q", got)
	}
}

func TestVoiceDisplayNameFromMember(t *testing.T) {
	vs := &discordgo.VoiceState{Member: &discordgo.Member{User: &discordgo.User{Username: "pixel", GlobalName: "Pixel"}}}
	if got := voiceDisplayName(vs); got != "Pixel" {
		t.Fatalf("got %q", got)
	}
	if voiceDisplayName(nil) != "" {
		t.Fatal("nil")
	}
}
