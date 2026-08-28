package discordx

import "testing"

func TestShouldSeedVoiceState(t *testing.T) {
	bot := "bot-id"
	if shouldSeedVoiceState(bot, bot, "vc-1") {
		t.Fatal("must exclude the bot")
	}
	if shouldSeedVoiceState(bot, "user-1", "") {
		t.Fatal("empty channel is not in voice")
	}
	if shouldSeedVoiceState(bot, "", "vc-1") {
		t.Fatal("empty user")
	}
	if !shouldSeedVoiceState(bot, "user-1", "vc-1") {
		t.Fatal("human in a channel should be seeded")
	}
}
