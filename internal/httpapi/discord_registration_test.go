package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/audit"
	"github.com/sounddock/sounddock/internal/auth"
)

func TestDiscordPutRegistrationWhitelist(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	prev, err := auth.LoadDiscordRegistration(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	var prevEnabled bool
	if err := pool.QueryRow(ctx, `SELECT enabled FROM discord_settings WHERE id=1`).Scan(&prevEnabled); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_ = auth.SaveDiscordRegistration(c, pool, prev)
		_, _ = pool.Exec(c, `UPDATE discord_settings SET enabled=$1 WHERE id=1`, prevEnabled)
	})
	if _, err := pool.Exec(ctx, `UPDATE discord_settings SET enabled=true WHERE id=1`); err != nil {
		t.Fatal(err)
	}

	s := &Server{Pool: pool, Audit: audit.New(pool)}
	admin := &auth.User{ID: uuid.New(), Username: "admin", IsAdmin: true}
	put := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/integrations/discord", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), userKey, admin))
		rec := httptest.NewRecorder()
		s.discordPut(rec, req)
		return rec
	}

	rec := put(`{"registration_whitelist_enabled":true,"registration_guilds":[
		{"guild_id":"100","label":"Main","role_ids":["101"," 102 "]},
		{"guild_id":"200","role_ids":[]}]}`)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Enabled   bool                            `json:"enabled"`
		Whitelist bool                            `json:"registration_whitelist_enabled"`
		Guilds    []auth.DiscordRegistrationGuild `json:"registration_guilds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Whitelist || len(out.Guilds) != 2 || out.Guilds[0].RoleIDs[1] != "102" || out.Guilds[1].GuildID != "200" {
		t.Fatalf("got %+v", out)
	}
	if !out.Enabled {
		t.Fatal("saving the whitelist without an enabled field switched the bot off")
	}

	for _, bad := range []string{
		`{"registration_guilds":[{"guild_id":"abc"}]}`,
		`{"registration_guilds":[{"guild_id":"100"},{"guild_id":"100"}]}`,
		`{"registration_whitelist_enabled":true,"registration_guilds":[]}`,
	} {
		if rec := put(bad); rec.Code != 400 {
			t.Fatalf("%s: status %d", bad, rec.Code)
		}
	}
	after, _ := auth.LoadDiscordRegistration(ctx, pool)
	if len(after.Guilds) != 2 {
		t.Fatalf("rejected update changed the whitelist: %+v", after)
	}

	if rec := put(`{"login_enabled":false}`); rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var botOn bool
	_ = pool.QueryRow(ctx, `SELECT enabled FROM discord_settings WHERE id=1`).Scan(&botOn)
	if !botOn {
		t.Fatal("saving sign-in settings switched the bot off")
	}
}
