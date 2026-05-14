package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeConfig writes a YAML string to a temp file and returns its path.
func writeConfig(t *testing.T, yaml string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestLoad_MinimalConfig(t *testing.T) {
	yaml := `
groups:
  - name: test-group
    max_runners: 2
`
	cfg, err := Load(writeConfig(t, yaml))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	// Group values.
	if len(cfg.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(cfg.Groups))
	}
	g := cfg.Groups[0]
	if g.Name != "test-group" {
		t.Errorf("group name = %q, want %q", g.Name, "test-group")
	}
	if g.MaxRunners != 2 {
		t.Errorf("max_runners = %d, want 2", g.MaxRunners)
	}

	// Defaults: runner.
	if cfg.Runner.Version != "latest" {
		t.Errorf("runner.version = %q, want %q", cfg.Runner.Version, "latest")
	}

	// Defaults: github.
	if cfg.GitHub.RunnerGroup != "default" {
		t.Errorf("github.runner_group = %q, want %q", cfg.GitHub.RunnerGroup, "default")
	}

	// Defaults: health.
	if !cfg.Health.Enabled {
		t.Error("health.enabled = false, want true (default)")
	}
	if cfg.Health.CheckInterval.Duration != 30*time.Second {
		t.Errorf("health.check_interval = %v, want 30s", cfg.Health.CheckInterval.Duration)
	}
	if cfg.Health.RunnerTimeout.Duration != 2*time.Hour {
		t.Errorf("health.runner_timeout = %v, want 2h", cfg.Health.RunnerTimeout.Duration)
	}
	if cfg.Health.DivergenceTimeout.Duration != 5*time.Minute {
		t.Errorf("health.divergence_timeout = %v, want 5m", cfg.Health.DivergenceTimeout.Duration)
	}
	if cfg.Health.MaxConsecutiveFailures != 5 {
		t.Errorf("health.max_consecutive_failures = %d, want 5", cfg.Health.MaxConsecutiveFailures)
	}
	if cfg.Health.FailureCooldown.Duration != 1*time.Minute {
		t.Errorf("health.failure_cooldown = %v, want 1m", cfg.Health.FailureCooldown.Duration)
	}
	if cfg.Health.MinDiskSpace != "1GB" {
		t.Errorf("health.min_disk_space = %q, want %q", cfg.Health.MinDiskSpace, "1GB")
	}

	// Defaults: logging.
	if cfg.Logging.Level != "info" {
		t.Errorf("logging.level = %q, want %q", cfg.Logging.Level, "info")
	}
	if cfg.Logging.Format != "text" {
		t.Errorf("logging.format = %q, want %q", cfg.Logging.Format, "text")
	}
	if cfg.Logging.RetentionDays != 30 {
		t.Errorf("logging.retention_days = %d, want 30", cfg.Logging.RetentionDays)
	}
	if cfg.Logging.RunnerOutput == nil || !*cfg.Logging.RunnerOutput {
		t.Error("logging.runner_output = false/nil, want true (default)")
	}

	// Defaults: notifications.
	if cfg.Notifications.Discord.Username != "ghr" {
		t.Errorf("notifications.discord.username = %q, want %q", cfg.Notifications.Discord.Username, "ghr")
	}

	// Defaults: daemon.
	if cfg.Daemon.ShutdownTimeout.Duration != 30*time.Second {
		t.Errorf("daemon.shutdown_timeout = %v, want 30s", cfg.Daemon.ShutdownTimeout.Duration)
	}
}

func TestLoad_FullConfig(t *testing.T) {
	yaml := `
github:
  url: "https://github.example.com"
  runner_group: "custom-group"

runner:
  version: "2.320.0"
  cache_dir: "/tmp/ghr-cache"
  workdir_base: "/tmp/ghr-runners"

groups:
  - name: production
    max_runners: 10
    min_runners: 2
    labels:
      - self-hosted
      - linux
    runner_group: "prod-pool"
    version: "2.319.0"
  - name: staging
    max_runners: 5
    min_runners: 0
    labels:
      - staging

health:
  enabled: true
  check_interval: "1m"
  runner_timeout: "3h"
  idle_timeout: "30m"
  divergence_timeout: "10m"
  max_consecutive_failures: 10
  failure_cooldown: "2m"
  min_disk_space: "5GB"

logging:
  level: "debug"
  format: "json"
  dir: "/tmp/ghr-logs"
  retention_days: 14
  runner_output: false

notifications:
  discord:
    enabled: true
    events:
      - runner.started
      - runner.failed
    username: "my-bot"
    mentions:
      error: "<@&111>"
      critical: "<@&222>"

monitoring:
  uptime_kuma:
    enabled: true
    degraded_threshold: 0.8
    report_health_as_ping: true

daemon:
  state_dir: "/tmp/ghr-state"
  shutdown_timeout: "1m"
`
	cfg, err := Load(writeConfig(t, yaml))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	// GitHub.
	if cfg.GitHub.URL != "https://github.example.com" {
		t.Errorf("github.url = %q, want %q", cfg.GitHub.URL, "https://github.example.com")
	}
	if cfg.GitHub.RunnerGroup != "custom-group" {
		t.Errorf("github.runner_group = %q, want %q", cfg.GitHub.RunnerGroup, "custom-group")
	}

	// Runner.
	if cfg.Runner.Version != "2.320.0" {
		t.Errorf("runner.version = %q, want %q", cfg.Runner.Version, "2.320.0")
	}
	if cfg.Runner.CacheDir != "/tmp/ghr-cache" {
		t.Errorf("runner.cache_dir = %q, want %q", cfg.Runner.CacheDir, "/tmp/ghr-cache")
	}
	if cfg.Runner.WorkdirBase != "/tmp/ghr-runners" {
		t.Errorf("runner.workdir_base = %q, want %q", cfg.Runner.WorkdirBase, "/tmp/ghr-runners")
	}

	// Groups.
	if len(cfg.Groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(cfg.Groups))
	}
	prod := cfg.Groups[0]
	if prod.Name != "production" {
		t.Errorf("groups[0].name = %q, want %q", prod.Name, "production")
	}
	if prod.MaxRunners != 10 {
		t.Errorf("groups[0].max_runners = %d, want 10", prod.MaxRunners)
	}
	if prod.MinRunners != 2 {
		t.Errorf("groups[0].min_runners = %d, want 2", prod.MinRunners)
	}
	if len(prod.Labels) != 2 || prod.Labels[0] != "self-hosted" || prod.Labels[1] != "linux" {
		t.Errorf("groups[0].labels = %v, want [self-hosted linux]", prod.Labels)
	}
	if prod.RunnerGroup != "prod-pool" {
		t.Errorf("groups[0].runner_group = %q, want %q", prod.RunnerGroup, "prod-pool")
	}
	if prod.Version != "2.319.0" {
		t.Errorf("groups[0].version = %q, want %q", prod.Version, "2.319.0")
	}

	staging := cfg.Groups[1]
	if staging.Name != "staging" {
		t.Errorf("groups[1].name = %q, want %q", staging.Name, "staging")
	}
	if staging.MaxRunners != 5 {
		t.Errorf("groups[1].max_runners = %d, want 5", staging.MaxRunners)
	}

	// Health.
	if !cfg.Health.Enabled {
		t.Error("health.enabled = false, want true")
	}
	if cfg.Health.CheckInterval.Duration != 1*time.Minute {
		t.Errorf("health.check_interval = %v, want 1m", cfg.Health.CheckInterval.Duration)
	}
	if cfg.Health.RunnerTimeout.Duration != 3*time.Hour {
		t.Errorf("health.runner_timeout = %v, want 3h", cfg.Health.RunnerTimeout.Duration)
	}
	if cfg.Health.IdleTimeout.Duration != 30*time.Minute {
		t.Errorf("health.idle_timeout = %v, want 30m", cfg.Health.IdleTimeout.Duration)
	}
	if cfg.Health.DivergenceTimeout.Duration != 10*time.Minute {
		t.Errorf("health.divergence_timeout = %v, want 10m", cfg.Health.DivergenceTimeout.Duration)
	}
	if cfg.Health.MaxConsecutiveFailures != 10 {
		t.Errorf("health.max_consecutive_failures = %d, want 10", cfg.Health.MaxConsecutiveFailures)
	}
	if cfg.Health.FailureCooldown.Duration != 2*time.Minute {
		t.Errorf("health.failure_cooldown = %v, want 2m", cfg.Health.FailureCooldown.Duration)
	}
	if cfg.Health.MinDiskSpace != "5GB" {
		t.Errorf("health.min_disk_space = %q, want %q", cfg.Health.MinDiskSpace, "5GB")
	}

	// Logging.
	if cfg.Logging.Level != "debug" {
		t.Errorf("logging.level = %q, want %q", cfg.Logging.Level, "debug")
	}
	if cfg.Logging.Format != "json" {
		t.Errorf("logging.format = %q, want %q", cfg.Logging.Format, "json")
	}
	if cfg.Logging.Dir != "/tmp/ghr-logs" {
		t.Errorf("logging.dir = %q, want %q", cfg.Logging.Dir, "/tmp/ghr-logs")
	}
	if cfg.Logging.RetentionDays != 14 {
		t.Errorf("logging.retention_days = %d, want 14", cfg.Logging.RetentionDays)
	}
	// With *bool, runner_output: false in YAML is now respected.
	if cfg.Logging.RunnerOutput == nil {
		t.Error("logging.runner_output = nil, want false")
	} else if *cfg.Logging.RunnerOutput {
		t.Error("logging.runner_output = true, want false (explicitly set in YAML)")
	}

	// Notifications.
	if !cfg.Notifications.Discord.Enabled {
		t.Error("notifications.discord.enabled = false, want true")
	}
	if cfg.Notifications.Discord.Username != "my-bot" {
		t.Errorf("notifications.discord.username = %q, want %q", cfg.Notifications.Discord.Username, "my-bot")
	}
	if len(cfg.Notifications.Discord.Events) != 2 {
		t.Errorf("notifications.discord.events len = %d, want 2", len(cfg.Notifications.Discord.Events))
	}
	if cfg.Notifications.Discord.Mentions.Error != "<@&111>" {
		t.Errorf("notifications.discord.mentions.error = %q, want %q", cfg.Notifications.Discord.Mentions.Error, "<@&111>")
	}
	if cfg.Notifications.Discord.Mentions.Critical != "<@&222>" {
		t.Errorf("notifications.discord.mentions.critical = %q, want %q", cfg.Notifications.Discord.Mentions.Critical, "<@&222>")
	}

	// Monitoring.
	if !cfg.Monitoring.UptimeKuma.Enabled {
		t.Error("monitoring.uptime_kuma.enabled = false, want true")
	}
	if cfg.Monitoring.UptimeKuma.DegradedThreshold != 0.8 {
		t.Errorf("monitoring.uptime_kuma.degraded_threshold = %f, want 0.8", cfg.Monitoring.UptimeKuma.DegradedThreshold)
	}
	if !cfg.Monitoring.UptimeKuma.ReportHealthAsPing {
		t.Error("monitoring.uptime_kuma.report_health_as_ping = false, want true")
	}

	// Daemon.
	if cfg.Daemon.StateDir != "/tmp/ghr-state" {
		t.Errorf("daemon.state_dir = %q, want %q", cfg.Daemon.StateDir, "/tmp/ghr-state")
	}
	if cfg.Daemon.ShutdownTimeout.Duration != 1*time.Minute {
		t.Errorf("daemon.shutdown_timeout = %v, want 1m", cfg.Daemon.ShutdownTimeout.Duration)
	}
}

func TestLoad_ValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantInErr string // substring expected in the error message
	}{
		{
			name:      "no groups",
			yaml:      `github: {url: "https://github.com"}`,
			wantInErr: "at least one group is required",
		},
		{
			name: "empty group name",
			yaml: `
groups:
  - name: ""
    max_runners: 1`,
			wantInErr: "name is required",
		},
		{
			name: "duplicate group names",
			yaml: `
groups:
  - name: dup
    max_runners: 1
  - name: dup
    max_runners: 2`,
			wantInErr: "duplicate group name",
		},
		{
			name: "max_runners less than 1",
			yaml: `
groups:
  - name: grp
    max_runners: 0`,
			wantInErr: "max_runners must be >= 1",
		},
		{
			name: "min_runners negative",
			yaml: `
groups:
  - name: grp
    max_runners: 5
    min_runners: -1`,
			wantInErr: "min_runners must be >= 0",
		},
		{
			name: "min_runners greater than max_runners",
			yaml: `
groups:
  - name: grp
    max_runners: 2
    min_runners: 5`,
			wantInErr: "min_runners (5) must be <= max_runners (2)",
		},
		{
			name: "empty label string",
			yaml: `
groups:
  - name: grp
    max_runners: 1
    labels:
      - ""`,
			wantInErr: "labels[0] must not be empty",
		},
		{
			name: "invalid logging level",
			yaml: `
logging:
  level: "verbose"
groups:
  - name: grp
    max_runners: 1`,
			wantInErr: "logging.level must be one of",
		},
		{
			name: "invalid logging format",
			yaml: `
logging:
  format: "xml"
groups:
  - name: grp
    max_runners: 1`,
			wantInErr: "logging.format must be one of",
		},
		{
			name: "check_interval too small",
			yaml: `
health:
  check_interval: "2s"
groups:
  - name: grp
    max_runners: 1`,
			wantInErr: "health.check_interval must be >= 5s",
		},
		{
			name: "runner_timeout too small",
			yaml: `
health:
  runner_timeout: "30s"
groups:
  - name: grp
    max_runners: 1`,
			wantInErr: "health.runner_timeout must be >= 1m",
		},
		{
			name: "shutdown_timeout too small",
			yaml: `
daemon:
  shutdown_timeout: "2s"
groups:
  - name: grp
    max_runners: 1`,
			wantInErr: "daemon.shutdown_timeout must be >= 5s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tt.yaml))
			if err == nil {
				t.Fatal("Load() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantInErr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.wantInErr)
			}
		})
	}
}

func TestLoad_DefaultPaths_NonRoot(t *testing.T) {
	// This test runs as a non-root user in development.
	if os.Getuid() == 0 {
		t.Skip("test requires non-root user")
	}

	yaml := `
groups:
  - name: grp
    max_runners: 1
`
	cfg, err := Load(writeConfig(t, yaml))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error: %v", err)
	}

	expectedDataDir := filepath.Join(home, ".local", "share", "ghr")

	if !strings.HasPrefix(cfg.Runner.CacheDir, expectedDataDir) {
		t.Errorf("runner.cache_dir = %q, want prefix %q", cfg.Runner.CacheDir, expectedDataDir)
	}
	if !strings.HasPrefix(cfg.Runner.WorkdirBase, expectedDataDir) {
		t.Errorf("runner.workdir_base = %q, want prefix %q", cfg.Runner.WorkdirBase, expectedDataDir)
	}
	if !strings.HasPrefix(cfg.Logging.Dir, expectedDataDir) {
		t.Errorf("logging.dir = %q, want prefix %q", cfg.Logging.Dir, expectedDataDir)
	}

	expectedStateDir := filepath.Join(home, ".local", "state", "ghr")
	if !strings.HasPrefix(cfg.Daemon.StateDir, expectedStateDir) {
		t.Errorf("daemon.state_dir = %q, want prefix %q", cfg.Daemon.StateDir, expectedStateDir)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("Load() expected error for non-existent file, got nil")
	}
	if !strings.Contains(err.Error(), "read config file") {
		t.Errorf("error = %q, want substring %q", err.Error(), "read config file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	invalidYAML := `
groups:
  - name: test
    max_runners: [[[invalid
`
	_, err := Load(writeConfig(t, invalidYAML))
	if err == nil {
		t.Fatal("Load() expected error for invalid YAML, got nil")
	}
	if !strings.Contains(err.Error(), "parse config file") {
		t.Errorf("error = %q, want substring %q", err.Error(), "parse config file")
	}
}

func TestLoad_DurationParsing(t *testing.T) {
	yaml := `
health:
  check_interval: "30s"
  runner_timeout: "5m"
  idle_timeout: "2h"
  divergence_timeout: "10m"
  failure_cooldown: "90s"

daemon:
  shutdown_timeout: "1m30s"

groups:
  - name: grp
    max_runners: 1
`
	cfg, err := Load(writeConfig(t, yaml))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	tests := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"check_interval 30s", cfg.Health.CheckInterval.Duration, 30 * time.Second},
		{"runner_timeout 5m", cfg.Health.RunnerTimeout.Duration, 5 * time.Minute},
		{"idle_timeout 2h", cfg.Health.IdleTimeout.Duration, 2 * time.Hour},
		{"divergence_timeout 10m", cfg.Health.DivergenceTimeout.Duration, 10 * time.Minute},
		{"failure_cooldown 90s", cfg.Health.FailureCooldown.Duration, 90 * time.Second},
		{"shutdown_timeout 1m30s", cfg.Daemon.ShutdownTimeout.Duration, time.Minute + 30*time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("duration = %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestLoad_EnvVarResolution(t *testing.T) {
	t.Setenv("GHR_DISCORD_WEBHOOK_URL", "https://discord.com/api/webhooks/test")
	t.Setenv("GHR_UPTIME_KUMA_URL", "https://uptime.example.com/api/push/abc123")

	yaml := `
groups:
  - name: grp
    max_runners: 1
`
	cfg, err := Load(writeConfig(t, yaml))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.Notifications.Discord.WebhookURL != "https://discord.com/api/webhooks/test" {
		t.Errorf("discord.webhook_url = %q, want %q", cfg.Notifications.Discord.WebhookURL, "https://discord.com/api/webhooks/test")
	}
	if cfg.Monitoring.UptimeKuma.BaseURL != "https://uptime.example.com/api/push/abc123" {
		t.Errorf("uptime_kuma.base_url = %q, want %q", cfg.Monitoring.UptimeKuma.BaseURL, "https://uptime.example.com/api/push/abc123")
	}
}
