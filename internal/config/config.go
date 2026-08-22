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
	RoleApp     Role = "app" // HTTP + jobs, no Discord gateway
	RoleAPI     Role = "api"
	RoleWorker  Role = "worker"
	RoleDiscord Role = "discord"
)

type Config struct {
	Role            Role
	HTTPAddr        string
	PublicURL       string
	InstanceName    string
	TrustedProxies  []string
	DatabaseURL     string
	MasterKey       string
	DataDir         string
	CacheDir        string
	BackupDir       string
	ManagedDir      string
	UseSecureCookie bool
	LogLevel        string
	OpenSubsonic    bool
	MetricsEnabled  bool
	MetricsToken    string
	RedisURL        string
	MeiliURL        string
	MeiliKey        string
	ShutdownWait    time.Duration
}

func Load() Config {
	role := Role(env("SD_ROLE", "all"))
	if role == "" {
		role = RoleAll
	}
	return Config{
		Role:            role,
		HTTPAddr:        env("SD_HTTP_ADDR", ":8080"),
		PublicURL:       strings.TrimRight(env("SD_PUBLIC_URL", ""), "/"),
		InstanceName:    env("SD_INSTANCE_NAME", "SoundDock"),
		TrustedProxies:  splitCSV(env("SD_TRUSTED_PROXIES", "127.0.0.1/32,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16")),
		DatabaseURL:     env("SD_DATABASE_URL", "postgres://sounddock:sounddock@127.0.0.1:5432/sounddock?sslmode=disable"),
		MasterKey:       env("SD_MASTER_KEY", ""),
		DataDir:         env("SD_DATA_DIR", "./data"),
		CacheDir:        env("SD_CACHE_DIR", "./data/cache"),
		BackupDir:       env("SD_BACKUP_DIR", "./data/backups"),
		ManagedDir:      env("SD_MANAGED_DIR", "./data/managed"),
		UseSecureCookie: envBool("SD_COOKIE_SECURE", false),
		LogLevel:        env("SD_LOG_LEVEL", "info"),
		OpenSubsonic:    envBool("SD_OPENSUBSONIC", false),
		MetricsEnabled:  envBool("SD_METRICS_ENABLED", false),
		MetricsToken:    env("SD_METRICS_TOKEN", ""),
		RedisURL:        env("SD_REDIS_URL", ""),
		MeiliURL:        env("SD_MEILISEARCH_URL", ""),
		MeiliKey:        env("SD_MEILISEARCH_KEY", ""),
		ShutdownWait:    40 * time.Second,
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
