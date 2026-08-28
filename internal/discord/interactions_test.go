package discordx

import "testing"

func TestEngineRepeatModeMapsTrackToOne(t *testing.T) {
	if engineRepeatMode("track") != "one" {
		t.Fatal(`"track" must map to engine "one"`)
	}
	for _, mode := range []string{"off", "queue", "one"} {
		if engineRepeatMode(mode) != mode {
			t.Fatalf("%q should pass through", mode)
		}
	}
}

func TestNextRepeatModeCycle(t *testing.T) {
	got := nextRepeatMode("off")
	if got != "one" {
		t.Fatalf("off -> %q, want one (engine), not track", got)
	}
	if engineRepeatMode(got) != "one" {
		t.Fatal("next from off must be an engine mode")
	}
	if nextRepeatMode("one") != "queue" {
		t.Fatal("one -> queue")
	}
	if nextRepeatMode("track") != "queue" {
		t.Fatal("legacy track -> queue")
	}
	if nextRepeatMode("queue") != "off" {
		t.Fatal("queue -> off")
	}
	if nextRepeatMode("") != "one" {
		t.Fatal("empty -> one")
	}
}
