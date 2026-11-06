package discordx

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestLibrariesForLinkedAdminIsUnlimited(t *testing.T) {
	pool := discordTestPool(t)
	ctx := context.Background()
	guild := "lib-" + uuid.NewString()[:8]
	lib, stor, admin, member := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	adminDiscord, memberDiscord := guild+"-a", guild+"-m"
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM discord_guilds WHERE id=$1`, guild)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id = ANY($1)`, []uuid.UUID{admin, member})
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, lib)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, stor)
	})
	exec(`INSERT INTO storage_providers (id, name, type, config_enc) VALUES ($1,$2,'local',$3)`, stor, guild, []byte("/tmp"))
	exec(`INSERT INTO libraries (id, name, kind, storage_provider_id) VALUES ($1,$2,'music',$3)`, lib, guild, stor)
	exec(`INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x','A'), ($3,$4,'x','M')`, admin, guild+"a", member, guild+"m")
	exec(`INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE name='Administrator'`, admin)
	exec(`INSERT INTO user_identities (user_id, provider, provider_user_id) VALUES ($1,'discord',$2), ($3,'discord',$4)`, admin, adminDiscord, member, memberDiscord)
	exec(`INSERT INTO discord_guilds (id, name, library_mode) VALUES ($1,'s','selected')`, guild)
	exec(`INSERT INTO discord_guild_libraries (guild_id, library_id) VALUES ($1,$2)`, guild, lib)

	bot := New(pool, nil, nil, nil, nil, nil)
	if got := bot.librariesFor(ctx, guild, adminDiscord); got != nil {
		t.Fatalf("admin limited to %v", got)
	}
	for _, who := range []string{memberDiscord, "not-linked"} {
		if got := bot.librariesFor(ctx, guild, who); len(got) != 1 || got[0] != lib {
			t.Fatalf("%s got %v", who, got)
		}
	}
}
