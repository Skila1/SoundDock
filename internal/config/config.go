package config

import (
	"net"
	"os"
	"path/filepath"
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
	LibraryHost     string
	UseSecureCookie bool
	LogLevel        string
	OpenSubsonic    bool
	MetricsEnabled  bool
	MetricsToken    string
	RedisURL        string
	MeiliURL        string
	MeiliKey        string
	ScapeXURL       string
	ShutdownWait    time.Duration
}

func Load() Config {
	role := Role(env("SD_ROLE", "all"))
	if role == "" {
		role = RoleAll
	}
	dataDir := env("SD_DATA_DIR", "./data")
	return Config{
		Role:            role,
		HTTPAddr:        env("SD_HTTP_ADDR", ":8080"),
		PublicURL:       strings.TrimRight(env("SD_PUBLIC_URL", ""), "/"),
		InstanceName:    env("SD_INSTANCE_NAME", "SoundDock"),
		TrustedProxies:  splitCSV(env("SD_TRUSTED_PROXIES", "127.0.0.1/32,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16")),
		DatabaseURL:     env("SD_DATABASE_URL", "postgres://sounddock:sounddock@127.0.0.1:5432/sounddock?sslmode=disable"),
		MasterKey:       loadMasterKey(dataDir),
		DataDir:         dataDir,
		CacheDir:        env("SD_CACHE_DIR", "./data/cache"),
		BackupDir:       env("SD_BACKUP_DIR", "./data/backups"),
		ManagedDir:      env("SD_MANAGED_DIR", "./data/managed"),
		LibraryHost:     env("SD_LIBRARY_HOST", ""),
		UseSecureCookie: envBool("SD_COOKIE_SECURE", false),
		LogLevel:        env("SD_LOG_LEVEL", "info"),
		OpenSubsonic:    envBool("SD_OPENSUBSONIC", false),
		MetricsEnabled:  envBool("SD_METRICS_ENABLED", false),
		MetricsToken:    env("SD_METRICS_TOKEN", ""),
		RedisURL:        env("SD_REDIS_URL", ""),
		MeiliURL:        env("SD_MEILISEARCH_URL", ""),
		MeiliKey:        env("SD_MEILISEARCH_KEY", ""),
		ScapeXURL:       strings.TrimRight(env("SD_SCAPEX_URL", ""), "/"),
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

// MasterKeyPath is {dataDir}/master.key. The file wins over SD_MASTER_KEY.
func MasterKeyPath(dataDir string) string {
	if dataDir == "" {
		dataDir = "./data"
	}
	return filepath.Join(dataDir, "master.key")
}

func loadMasterKey(dataDir string) string {
	if b, err := os.ReadFile(MasterKeyPath(dataDir)); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}
	return env("SD_MASTER_KEY", "")
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
