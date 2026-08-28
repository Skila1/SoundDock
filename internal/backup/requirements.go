package backup

import (
	"os"
	"strings"

	"github.com/sounddock/sounddock/internal/config"
)

const (
	ClassRecovered    = "recovered"
	ClassMustReenter  = "must_reenter"
	ClassHostSpecific = "host_specific"
	ClassConfirm      = "confirm"
	SourceRecoveryBox = "recovery.box"
	SourceSQL         = "sql"
	SourceEnv         = "env"
	SourceManifest    = "manifest"
)

// RestoreRequirement is one env/host item. No secrets.
type RestoreRequirement struct {
	Key             string `json:"key"`
	Class           string `json:"class"`
	Source          string `json:"source"`
	PresentAtBackup bool   `json:"present_at_backup"`
	PresentOnHost   bool   `json:"present_on_host,omitempty"`
	Recovered       bool   `json:"recovered"`
	Note            string `json:"note,omitempty"`
}

// RestoreRequirements is written into the archive (no secrets, no .env).
type RestoreRequirements struct {
	InstanceName  string               `json:"instance_name"`
	SchemaVersion int                  `json:"schema_version"`
	Items         []RestoreRequirement `json:"items"`
}

var hostSpecificKeys = []string{
	"SD_DATABASE_URL", "POSTGRES_PASSWORD", "POSTGRES_USER", "POSTGRES_DB",
	"SD_ROLE", "SD_HTTP_ADDR", "SD_PORT", "SD_DATA_DIR", "SD_CACHE_DIR",
	"SD_BACKUP_DIR", "SD_MANAGED_DIR", "SD_LIBRARY_HOST", "SD_UPDATE_DIR",
	"SD_COMPOSE_PROJECT", "SD_IMAGE", "SD_DOCKER_GID", "SD_ALLOW_DOCKER_SOCK",
	"SD_TRUSTED_PROXIES", "SD_REDIS_URL", "SD_MEILISEARCH_URL", "SD_SCAPEX_URL",
	"SCAPEX_YTDLP", "SCAPEX_YT_BROWSER",
}

var secretEnvOnly = []string{
	"SD_DISCORD_CLIENT_ID", "SD_DISCORD_CLIENT_SECRET", "SD_DISCORD_BOT_TOKEN",
	"SD_LASTFM_API_KEY", "SD_LASTFM_API_SECRET", "SD_METRICS_TOKEN",
	"SD_MEILISEARCH_KEY",
}

var confirmKeys = []string{
	"SD_PUBLIC_URL", "SD_COOKIE_SECURE", "SD_OPENSUBSONIC", "SD_METRICS_ENABLED",
	"SD_LIBRARY_HOST",
}

// ClassifyRestoreRequirements builds restore-requirements.json from live config and env.
// Values are never copied; only names and classification.
func ClassifyRestoreRequirements(cfg config.Config) RestoreRequirements {
	out := RestoreRequirements{
		InstanceName:  cfg.InstanceName,
		SchemaVersion: ImageSchemaHead(),
		Items:         []RestoreRequirement{},
	}
	out.Items = append(out.Items, RestoreRequirement{
		Key:             "SD_MASTER_KEY",
		Class:           ClassRecovered,
		Source:          SourceRecoveryBox,
		PresentAtBackup: cfg.MasterKey != "",
		Recovered:       true,
		Note:            "Restored via recovery.box into /data/master.key. Discord, Spotify, storage, and webhook secrets in SQL become usable after this key is loaded.",
	})
	out.Items = append(out.Items, RestoreRequirement{
		Key:             "SD_INSTANCE_NAME",
		Class:           ClassRecovered,
		Source:          SourceManifest,
		PresentAtBackup: cfg.InstanceName != "",
		Recovered:       true,
	})

	seen := map[string]bool{"SD_MASTER_KEY": true, "SD_INSTANCE_NAME": true}

	for _, k := range confirmKeys {
		if os.Getenv(k) == "" {
			continue
		}
		note := "Set this on the new host. It is not copied from the archive."
		if k == "SD_LIBRARY_HOST" {
			note = "Remount the same trees that libraries.root_prefix and local provider paths expect. NAS is not packed in the archive."
		}
		if k == "SD_PUBLIC_URL" {
			note = "Confirm the public URL on the new host (tunnel or reverse proxy)."
		}
		out.Items = append(out.Items, RestoreRequirement{
			Key:             k,
			Class:           ClassMustReenter,
			Source:          SourceEnv,
			PresentAtBackup: true,
			Recovered:       false,
			Note:            note,
		})
		seen[k] = true
	}

	for _, k := range secretEnvOnly {
		if os.Getenv(k) == "" {
			continue
		}
		out.Items = append(out.Items, RestoreRequirement{
			Key:             k,
			Class:           ClassMustReenter,
			Source:          SourceEnv,
			PresentAtBackup: true,
			Recovered:       false,
			Note:            "This was env-only on the source host and was never written to SQL. Re-enter it, or prefer Admin so it is stored encrypted.",
		})
		seen[k] = true
	}

	if os.Getenv("SCAPEX_YT_COOKIES") != "" {
		out.Items = append(out.Items, RestoreRequirement{
			Key:             "SCAPEX_YT_COOKIES",
			Class:           ClassMustReenter,
			Source:          SourceEnv,
			PresentAtBackup: true,
			Recovered:       false,
			Note:            "Cookie file contents were not packed. Place a new cookies file on the host.",
		})
		seen["SCAPEX_YT_COOKIES"] = true
	}

	for _, k := range hostSpecificKeys {
		if seen[k] {
			continue
		}
		out.Items = append(out.Items, RestoreRequirement{
			Key:             k,
			Class:           ClassHostSpecific,
			Source:          SourceEnv,
			PresentAtBackup: os.Getenv(k) != "",
			Recovered:       false,
			Note:            "Host-specific. Do not copy from the old machine.",
		})
		seen[k] = true
	}
	return out
}

// AnnotateHost fills PresentOnHost from the current process env after restore.
func (r RestoreRequirements) AnnotateHost() RestoreRequirements {
	for i := range r.Items {
		r.Items[i].PresentOnHost = os.Getenv(r.Items[i].Key) != ""
		if r.Items[i].Key == "SD_MASTER_KEY" {
			r.Items[i].PresentOnHost = r.Items[i].PresentOnHost || masterKeyFilePresent()
		}
	}
	return r
}

func masterKeyFilePresent() bool {
	for _, p := range []string{
		os.Getenv("SD_DATA_DIR") + "/master.key",
		"./data/master.key",
		"/data/master.key",
	} {
		if p == "/master.key" || strings.HasPrefix(p, "/master.key") {
			continue
		}
		if st, err := os.Stat(p); err == nil && st.Size() > 0 {
			return true
		}
	}
	return false
}
