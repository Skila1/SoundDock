package config

import (
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Role string

const (
	RoleAll     Role = "all"
	RoleAPI     Role = "api"
	RoleWorker  Role = "worker"
	RoleDiscord Role = "discord"
)

type Config struct {
	Role           Role
	HTTPAddr       string
	PublicURL      string
	InstanceName   string
	TrustedProxies []string
	DatabaseURL    string
	MasterKey      string
	DataDir        string
	CacheDir       string
	BackupDir      string
	ManagedDir     string
	UseSecureCookie bool
	LogLevel       string
	OpenSubsonic   bool
	MetricsEnabled bool
	MetricsToken   string
	RedisURL       string
	MeiliURL       string
	MeiliKey       string
	ShutdownWait   time.Duration

	DiscordClientID     string
	DiscordClientSecret string
	DiscordBotToken     string
	AdminDiscordIDs     []string
}

func Load() Config {
	role := Role(env("SOUNDDOCK_ROLE", "all"))
	if role == "" {
		role = RoleAll
	}
	return Config{
		Role:           role,
		HTTPAddr:       env("SOUNDDOCK_HTTP_ADDR", ":8080"),
		PublicURL:      strings.TrimRight(env("SOUNDDOCK_PUBLIC_URL", "http://localhost:8080"), "/"),
		InstanceName:   env("SOUNDDOCK_INSTANCE_NAME", "SoundDock"),
		TrustedProxies: splitCSV(env("SOUNDDOCK_TRUSTED_PROXIES", "127.0.0.1/32,::1/128")),
		DatabaseURL:    env("SOUNDDOCK_DATABASE_URL", "postgres://sounddock:sounddock@127.0.0.1:5432/sounddock?sslmode=disable"),
		MasterKey:      env("SOUNDDOCK_MASTER_KEY", ""),
		DataDir:        env("SOUNDDOCK_DATA_DIR", "./data"),
		CacheDir:       env("SOUNDDOCK_CACHE_DIR", "./data/cache"),
		BackupDir:      env("SOUNDDOCK_BACKUP_DIR", "./data/backups"),
		ManagedDir:     env("SOUNDDOCK_MANAGED_DIR", "./data/managed"),
		UseSecureCookie: envBool("SOUNDDOCK_COOKIE_SECURE", false),
		LogLevel:       env("SOUNDDOCK_LOG_LEVEL", "info"),
		OpenSubsonic:   envBool("SOUNDDOCK_OPENSUBSONIC", false),
		MetricsEnabled: envBool("SOUNDDOCK_METRICS_ENABLED", false),
		MetricsToken:   env("SOUNDDOCK_METRICS_TOKEN", ""),
		RedisURL:       env("SOUNDDOCK_REDIS_URL", ""),
		MeiliURL:       env("SOUNDDOCK_MEILISEARCH_URL", ""),
		MeiliKey:       env("SOUNDDOCK_MEILISEARCH_KEY", ""),
		ShutdownWait:   40 * time.Second,
		DiscordClientID:     env("SOUNDDOCK_DISCORD_CLIENT_ID", ""),
		DiscordClientSecret: env("SOUNDDOCK_DISCORD_CLIENT_SECRET", ""),
		DiscordBotToken:     env("SOUNDDOCK_DISCORD_BOT_TOKEN", ""),
		AdminDiscordIDs:     splitCSV(env("SOUNDDOCK_ADMIN_DISCORD_ID", env("SOUNDDOCK_ADMIN_DISCORD_IDS", ""))),
	}
}

func (c Config) CookieSecure() bool {
	if c.UseSecureCookie {
		return true
	}
	return strings.HasPrefix(strings.ToLower(c.PublicURL), "https://")
}

func (c Config) TrustedNets() []*net.IPNet {
	var out []*net.IPNet
	for _, cidr := range c.TrustedProxies {
		_, n, err := net.ParseCIDR(strings.TrimSpace(cidr))
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
