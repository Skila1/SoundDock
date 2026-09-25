package config

import (
	"fmt"
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
	AllowedOrigins  []string
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
	publicURL := strings.TrimRight(env("SD_PUBLIC_URL", ""), "/")
	secureCookieDefault := envBool("SD_COOKIE_SECURE", strings.HasPrefix(strings.ToLower(publicURL), "https://"))
	allowedOrigins := splitCSV(env("SD_ALLOWED_ORIGINS", "http://localhost:5173,http://127.0.0.1:5173"))
	if publicURL != "" {
		allowedOrigins = appendUnique(allowedOrigins, publicURL)
	}
	return Config{
		Role:            role,
		HTTPAddr:        env("SD_HTTP_ADDR", ":8080"),
		PublicURL:       publicURL,
		InstanceName:    env("SD_INSTANCE_NAME", "SoundDock"),
		AllowedOrigins:  allowedOrigins,
		TrustedProxies:  splitCSV(env("SD_TRUSTED_PROXIES", "127.0.0.1/32,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16")),
		DatabaseURL:     env("SD_DATABASE_URL", "postgres://sounddock:sounddock@127.0.0.1:5432/sounddock?sslmode=disable"),
		MasterKey:       loadMasterKey(dataDir),
		DataDir:         dataDir,
		CacheDir:        env("SD_CACHE_DIR", "./data/cache"),
		BackupDir:       env("SD_BACKUP_DIR", "./data/backups"),
		ManagedDir:      env("SD_MANAGED_DIR", "./data/managed"),
		LibraryHost:     env("SD_LIBRARY_HOST", ""),
		UseSecureCookie: secureCookieDefault,
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

func (c Config) CORSAllowedOrigins() []string {
	return appendUnique(nil, c.AllowedOrigins...)
}

func (c Config) Validate() error {
	prod := env("SD_ENV", "") == "production" || env("SD_ENVIRONMENT", "") == "production" || strings.HasPrefix(strings.ToLower(c.PublicURL), "https://")
	if !prod {
		return nil
	}
	if c.MasterKey == "" || len(c.MasterKey) < 32 || strings.Contains(strings.ToLower(c.MasterKey), "changeme") || strings.Contains(strings.ToLower(c.MasterKey), "change-me") || strings.Contains(strings.ToLower(c.MasterKey), "local-docker") {
		return fmt.Errorf("SD_MASTER_KEY must be set to a strong, non-default value in production")
	}
	if strings.Contains(strings.ToLower(c.DatabaseURL), "changeme") || strings.Contains(strings.ToLower(c.DatabaseURL), "postgres://sounddock:sounddock") || strings.Contains(strings.ToLower(c.DatabaseURL), "@postgres") {
		return fmt.Errorf("SD_DATABASE_URL must be set to a production database connection string")
	}
	if !c.UseSecureCookie {
		return fmt.Errorf("SD_COOKIE_SECURE must be enabled when serving HTTPS traffic")
	}
	return nil
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

func appendUnique(dst []string, vals ...string) []string {
	seen := map[string]bool{}
	for _, v := range dst {
		v = strings.TrimSpace(v)
		if v != "" {
			seen[strings.ToLower(v)] = true
		}
	}
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if !seen[strings.ToLower(v)] {
			seen[strings.ToLower(v)] = true
			dst = append(dst, v)
		}
	}
	return dst
}
