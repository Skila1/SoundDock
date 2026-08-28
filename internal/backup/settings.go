package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sounddock/sounddock/internal/config"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
)

const SettingKey = "backup_destination"

type Settings struct {
	LocalEnabled         bool   `json:"local_enabled"`
	R2Enabled            bool   `json:"r2_enabled"`
	IncludeMedia         bool   `json:"include_media"`
	ScheduledEnabled     bool   `json:"scheduled_enabled"`
	Endpoint             string `json:"endpoint"`
	Region               string `json:"region"`
	Bucket               string `json:"bucket"`
	AccessKey            string `json:"access_key"`
	SecretKey            string `json:"secret_key"`
	Prefix               string `json:"prefix"`
	UseSSL               bool   `json:"use_ssl"`
	SecretSet            bool   `json:"secret_set"`
	RestorePassphraseSet bool   `json:"restore_passphrase_set"`
	ReminderPending      bool   `json:"reminder_pending"`
}

type storedSettings struct {
	LocalEnabled     *bool  `json:"local_enabled"`
	R2Enabled        bool   `json:"r2_enabled"`
	IncludeMedia     *bool  `json:"include_media"`
	ScheduledEnabled *bool  `json:"scheduled_enabled"`
	Endpoint         string `json:"endpoint"`
	Region           string `json:"region"`
	Bucket           string `json:"bucket"`
	AccessKey        string `json:"access_key"`
	SecretKey        string `json:"secret_key"`
	SecretKeyEnc     []byte `json:"secret_key_enc"`
	Prefix           string `json:"prefix"`
	UseSSL           bool   `json:"use_ssl"`
	DekEnc           []byte `json:"dek_enc,omitempty"`
	RecoveryBox      []byte `json:"recovery_box,omitempty"`
	KDFTime          uint32 `json:"kdf_time,omitempty"`
	KDFMemory        uint32 `json:"kdf_memory,omitempty"`
	KDFThreads       uint8  `json:"kdf_threads,omitempty"`
	KDFSalt          []byte `json:"kdf_salt,omitempty"`
	ReminderPending  bool   `json:"reminder_pending,omitempty"`
}

func (s *Service) Attach(mediaDir string, box *cryptox.Box) {
	if s == nil {
		return
	}
	s.media = mediaDir
	s.box = box
}

func (s *Service) Configure(cfg config.Config) {
	if s == nil {
		return
	}
	s.dataDir = cfg.DataDir
	s.artwork = filepath.Join(cfg.CacheDir, "artwork")
	s.lyrics = filepath.Join(cfg.DataDir, "lyrics")
	s.master = cfg.MasterKey
	s.instance = cfg.InstanceName
	s.imageSchema = ImageSchemaHead()
}

func defaultSettings() Settings {
	return Settings{LocalEnabled: true, IncludeMedia: true, UseSSL: true, Region: "auto"}
}

func (s *Service) loadStored(ctx context.Context) storedSettings {
	var st storedSettings
	if s == nil || s.pool == nil {
		return st
	}
	var raw []byte
	if err := s.pool.QueryRow(ctx, `SELECT value FROM server_settings WHERE key=$1`, SettingKey).Scan(&raw); err != nil || len(raw) == 0 {
		return st
	}
	_ = json.Unmarshal(raw, &st)
	return st
}

func (s *Service) LoadSettings(ctx context.Context) Settings {
	out := defaultSettings()
	st := s.loadStored(ctx)
	if st.LocalEnabled != nil {
		out.LocalEnabled = *st.LocalEnabled
	}
	if st.IncludeMedia != nil {
		out.IncludeMedia = *st.IncludeMedia
	}
	if st.ScheduledEnabled != nil {
		out.ScheduledEnabled = *st.ScheduledEnabled
	}
	out.R2Enabled = st.R2Enabled
	out.Endpoint = st.Endpoint
	out.Region = st.Region
	if out.Region == "" {
		out.Region = "auto"
	}
	out.Bucket = st.Bucket
	out.AccessKey = st.AccessKey
	out.Prefix = st.Prefix
	out.UseSSL = st.UseSSL
	if len(st.SecretKeyEnc) > 0 && s != nil && s.box != nil {
		if p, err := s.box.Decrypt(st.SecretKeyEnc); err == nil {
			out.SecretKey = string(p)
		}
	} else if st.SecretKey != "" {
		out.SecretKey = st.SecretKey
	}
	out.SecretSet = out.SecretKey != ""
	out.RestorePassphraseSet = len(st.RecoveryBox) > 0 && len(st.DekEnc) > 0
	out.ReminderPending = st.ReminderPending
	return out
}

func (s *Service) kdfFromStored(st storedSettings) KDFParams {
	k := KDFParams{Time: st.KDFTime, Memory: st.KDFMemory, Threads: st.KDFThreads, Salt: st.KDFSalt}
	if k.Time == 0 {
		k.Time = kdfTimeDefault
	}
	if k.Memory == 0 {
		k.Memory = kdfMemoryDefault
	}
	if k.Threads == 0 {
		k.Threads = kdfThreadsDefault
	}
	return k
}

func (s *Service) SaveSettings(ctx context.Context, in Settings) error {
	prev := s.LoadSettings(ctx)
	st := s.loadStored(ctx)
	if strings.TrimSpace(in.SecretKey) == "" {
		in.SecretKey = prev.SecretKey
	}
	if in.ScheduledEnabled && !prev.RestorePassphraseSet && len(st.RecoveryBox) == 0 {
		return fmt.Errorf("scheduled backups cannot be enabled until a recovery passphrase is set")
	}
	st.LocalEnabled = &in.LocalEnabled
	st.R2Enabled = in.R2Enabled
	st.IncludeMedia = &in.IncludeMedia
	st.ScheduledEnabled = &in.ScheduledEnabled
	st.Endpoint = strings.TrimSpace(in.Endpoint)
	st.Region = strings.TrimSpace(in.Region)
	st.Bucket = strings.TrimSpace(in.Bucket)
	st.AccessKey = strings.TrimSpace(in.AccessKey)
	st.Prefix = strings.TrimSpace(in.Prefix)
	st.UseSSL = in.UseSSL
	if in.SecretKey != "" && s.box != nil {
		if enc, err := s.box.Encrypt([]byte(in.SecretKey)); err == nil {
			st.SecretKeyEnc = enc
			st.SecretKey = ""
		}
	}
	return s.saveStored(ctx, st)
}

func (s *Service) saveStored(ctx context.Context, st storedSettings) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO server_settings (key, value) VALUES ($1, $2::jsonb)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, SettingKey, b)
	return err
}

func (s Settings) Public() Settings {
	s.SecretKey = ""
	return s
}

func (s *Service) SetPassphrase(ctx context.Context, passphrase, current string) (Settings, error) {
	if len([]rune(passphrase)) < MinPassphrase {
		return Settings{}, ErrPassphraseShort
	}
	if s.master == "" {
		return Settings{}, ErrNoMaster
	}
	st := s.loadStored(ctx)
	if len(st.RecoveryBox) > 0 {
		if current == "" {
			return Settings{}, fmt.Errorf("current recovery passphrase is required to change it")
		}
		kdf := s.kdfFromStored(st)
		if _, _, err := unwrapRecovery(current, st.RecoveryBox, kdf); err != nil {
			return Settings{}, err
		}
		dek, err := unboxDEK(s.box, st.DekEnc)
		if err != nil {
			return Settings{}, err
		}
		box, err := wrapRecovery(passphrase, dek, []byte(s.master), kdf)
		if err != nil {
			return Settings{}, err
		}
		st.RecoveryBox = box
		st.ReminderPending = false
		if err := s.saveStored(ctx, st); err != nil {
			return Settings{}, err
		}
		return s.LoadSettings(ctx).Public(), nil
	}
	dek, err := newDEK()
	if err != nil {
		return Settings{}, err
	}
	enc, err := boxDEK(s.box, dek)
	if err != nil {
		return Settings{}, err
	}
	kdf := defaultKDF()
	box, err := wrapRecovery(passphrase, dek, []byte(s.master), kdf)
	if err != nil {
		return Settings{}, err
	}
	st.DekEnc = enc
	st.RecoveryBox = box
	st.KDFTime = kdf.Time
	st.KDFMemory = kdf.Memory
	st.KDFThreads = kdf.Threads
	st.KDFSalt = kdf.Salt
	st.ReminderPending = true
	if err := s.saveStored(ctx, st); err != nil {
		return Settings{}, err
	}
	return s.LoadSettings(ctx).Public(), nil
}

func (s *Service) ConsumeReminder(ctx context.Context) (string, error) {
	st := s.loadStored(ctx)
	if !st.ReminderPending {
		return "", fmt.Errorf("recovery reminder is not available")
	}
	st.ReminderPending = false
	if err := s.saveStored(ctx, st); err != nil {
		return "", err
	}
	name := s.instance
	if name == "" {
		name = "SoundDock"
	}
	return fmt.Sprintf("SoundDock recovery reminder\nInstance: %s\nDate: stored when the passphrase was set\n\nSoundDock does not store your recovery passphrase.\nKeep the passphrase you chose in a password manager.\nOld backups stay recoverable with the passphrase that was current when they were made.\n", name), nil
}

func (s *Service) DeclineReminder(ctx context.Context) error {
	st := s.loadStored(ctx)
	st.ReminderPending = false
	return s.saveStored(ctx, st)
}

func (s *Service) cfgSnapshot() config.Config {
	cfg := config.Load()
	if s != nil {
		if s.instance != "" {
			cfg.InstanceName = s.instance
		}
		if s.master != "" {
			cfg.MasterKey = s.master
		}
		if s.dataDir != "" {
			cfg.DataDir = s.dataDir
		}
	}
	return cfg
}

func (s *Service) ReminderText() string {
	name := s.instance
	if name == "" {
		name = "SoundDock"
	}
	return fmt.Sprintf("SoundDock recovery reminder\nInstance: %s\n\nSoundDock does not store your recovery passphrase.\nKeep the passphrase you chose in a password manager.\n", name)
}
