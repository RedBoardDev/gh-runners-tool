package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

func Load(path string) (*Config, error) {
	_ = godotenv.Load()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %q: %w", path, err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file %q: %w", path, err)
	}

	applyDefaults(cfg)
	resolveEnvVars(cfg)

	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

func applyDefaults(cfg *Config) {
	isRoot := os.Getuid() == 0

	var dataDir, logDir, stateDir string
	if isRoot {
		dataDir = "/var/lib/ghr"
		logDir = "/var/log/ghr"
		stateDir = "/var/lib/ghr/state"
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		dataDir = filepath.Join(home, ".local", "share", "ghr")
		logDir = filepath.Join(home, ".local", "share", "ghr", "logs")
		stateDir = filepath.Join(home, ".local", "state", "ghr")
	}

	if cfg.GitHub.RunnerGroup == "" {
		cfg.GitHub.RunnerGroup = "default"
	}

	if cfg.Runner.Version == "" {
		cfg.Runner.Version = "latest"
	}
	if cfg.Runner.CacheDir == "" {
		cfg.Runner.CacheDir = filepath.Join(dataDir, "cache")
	}
	if cfg.Runner.WorkdirBase == "" {
		cfg.Runner.WorkdirBase = filepath.Join(dataDir, "runners")
	}

	if isHealthZero(cfg.Health) {
		cfg.Health.Enabled = true
	}
	if cfg.Health.CheckInterval.Duration == 0 {
		cfg.Health.CheckInterval = Duration{30 * time.Second}
	}
	if cfg.Health.RunnerTimeout.Duration == 0 {
		cfg.Health.RunnerTimeout = Duration{2 * time.Hour}
	}
	if cfg.Health.DivergenceTimeout.Duration == 0 {
		cfg.Health.DivergenceTimeout = Duration{5 * time.Minute}
	}
	if cfg.Health.MaxConsecutiveFailures == 0 {
		cfg.Health.MaxConsecutiveFailures = 5
	}
	if cfg.Health.FailureCooldown.Duration == 0 {
		cfg.Health.FailureCooldown = Duration{1 * time.Minute}
	}
	if cfg.Health.MinDiskSpace == "" {
		cfg.Health.MinDiskSpace = "1GB"
	}

	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "text"
	}
	if cfg.Logging.Dir == "" {
		cfg.Logging.Dir = logDir
	}
	if cfg.Logging.RetentionDays == 0 {
		cfg.Logging.RetentionDays = 30
	}
	if cfg.Logging.RunnerOutput == nil {
		t := true
		cfg.Logging.RunnerOutput = &t
	}

	if cfg.Notifications.Discord.Username == "" {
		cfg.Notifications.Discord.Username = "ghr"
	}

	if cfg.Daemon.StateDir == "" {
		cfg.Daemon.StateDir = stateDir
	}
	if cfg.Daemon.ShutdownTimeout.Duration == 0 {
		cfg.Daemon.ShutdownTimeout = Duration{30 * time.Second}
	}
}

func resolveEnvVars(cfg *Config) {
	if v := os.Getenv("GHR_DISCORD_WEBHOOK_URL"); v != "" {
		cfg.Notifications.Discord.WebhookURL = v
	}
	if v := os.Getenv("GHR_UPTIME_KUMA_URL"); v != "" {
		cfg.Monitoring.UptimeKuma.BaseURL = v
	}
}
