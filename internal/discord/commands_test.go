package discordx

import (
	"bytes"
	"strings"
	"testing"
)

func TestCommandSyncPutsOnlyGlobalsAndWipesGuilds(t *testing.T) {
	t.Parallel()
	ops := commandSyncPuts("app-1", []string{"g1", "g2"})
	if len(ops) != 3 {
		t.Fatalf("ops %d", len(ops))
	}
	if ops[0].URL != globalCommandsURL("app-1") {
		t.Fatalf("global url %s", ops[0].URL)
	}
	if bytes.Equal(ops[0].Body, []byte("[]")) {
		t.Fatal("global catalogue must not be empty")
	}
	if !bytes.Contains(ops[0].Body, []byte(`"name":"play"`)) {
		t.Fatalf("global body %s", ops[0].Body)
	}
	for i, g := range []string{"g1", "g2"} {
		op := ops[i+1]
		if op.URL != guildCommandsURL("app-1", g) {
			t.Fatalf("guild url %s", op.URL)
		}
		if !bytes.Equal(op.Body, []byte("[]")) {
			t.Fatalf("guild %s body %s", g, op.Body)
		}
	}
}

func TestCommandSyncSkipsBlankGuildIDs(t *testing.T) {
	t.Parallel()
	ops := commandSyncPuts("app-1", []string{"", "g1", " "})
	if len(ops) != 2 {
		t.Fatalf("ops %d", len(ops))
	}
	if !strings.Contains(ops[1].URL, "/guilds/g1/commands") {
		t.Fatalf("url %s", ops[1].URL)
	}
}
