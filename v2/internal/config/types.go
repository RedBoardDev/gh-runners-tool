package config

import (
	"fmt"
	"time"
)

type Config struct {
	GitHub        GitHubConfig        `yaml:"github"`
	Runner        RunnerConfig        `yaml:"runner"`
	Groups        []GroupConfig       `yaml:"groups"`
	Health        HealthConfig        `yaml:"health"`
	Logging       LoggingConfig       `yaml:"logging"`
	Notifications NotificationsConfig `yaml:"notifications"`
	Monitoring    MonitoringConfig    `yaml:"monitoring"`
	Daemon        DaemonConfig        `yaml:"daemon"`
}

type GitHubConfig struct {
	URL         string `yaml:"url"`
	RunnerGroup string `yaml:"runner_group"`
}

type RunnerConfig struct {
	Version     string `yaml:"version"`
	CacheDir    string `yaml:"cache_dir"`
	WorkdirBase string `yaml:"workdir_base"`
}

type GroupConfig struct {
	Name        string             `yaml:"name"`
	MaxRunners  int                `yaml:"max_runners"`
	MinRunners  int                `yaml:"min_runners"`
	Labels      []string           `yaml:"labels"`
	RunnerGroup string             `yaml:"runner_group"`
	Version     string             `yaml:"version"`
	Health      *GroupHealthConfig `yaml:"health,omitempty"`
}

type GroupHealthConfig struct {
	RunnerTimeout Duration `yaml:"runner_timeout"`
}

type HealthConfig struct {
	Enabled                bool     `yaml:"enabled"`
	CheckInterval          Duration `yaml:"check_interval"`
	RunnerTimeout          Duration `yaml:"runner_timeout"`
	IdleTimeout            Duration `yaml:"idle_timeout"`
	DivergenceTimeout      Duration `yaml:"divergence_timeout"`
	MaxConsecutiveFailures int      `yaml:"max_consecutive_failures"`
	FailureCooldown        Duration `yaml:"failure_cooldown"`
	MinDiskSpace           string   `yaml:"min_disk_space"`
}

type LoggingConfig struct {
	Level         string `yaml:"level"`
	Format        string `yaml:"format"`
	Dir           string `yaml:"dir"`
	RetentionDays int    `yaml:"retention_days"`
	RunnerOutput  *bool  `yaml:"runner_output"`
}

type NotificationsConfig struct {
	Discord DiscordConfig `yaml:"discord"`
}

type DiscordConfig struct {
	Enabled    bool     `yaml:"enabled"`
	WebhookURL string   `yaml:"-"`
	Events     []string `yaml:"events"`
	Username   string   `yaml:"username"`
	AvatarURL  string   `yaml:"avatar_url"`
	Mentions   struct {
		Error    string `yaml:"error"`
		Critical string `yaml:"critical"`
	} `yaml:"mentions"`
}

type MonitoringConfig struct {
	UptimeKuma UptimeKumaConfig `yaml:"uptime_kuma"`
}

type UptimeKumaConfig struct {
	Enabled            bool              `yaml:"enabled"`
	BaseURL            string            `yaml:"-"`
	DaemonToken        string            `yaml:"-"`
	GroupTokens        map[string]string `yaml:"-"`
	DegradedThreshold  float64           `yaml:"degraded_threshold"`
	ReportHealthAsPing bool              `yaml:"report_health_as_ping"`
}

type DaemonConfig struct {
	StateDir        string   `yaml:"state_dir"`
	ShutdownTimeout Duration `yaml:"shutdown_timeout"`
}

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return fmt.Errorf("unmarshaling duration: %w", err)
	}
	if s == "" || s == "0" {
		d.Duration = 0
		return nil
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = dur
	return nil
}

func (d Duration) MarshalYAML() (interface{}, error) {
	return d.Duration.String(), nil
}
