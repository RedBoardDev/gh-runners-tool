package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/api"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/auth"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/config"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/controller"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/github"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/health"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/logging"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/monitoring"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/notification"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/runner"
)

type daemon struct {
	ctrl   *controller.GroupController
	health *health.Monitor
	api    *api.Server
	logMgr *logging.LogManager
	cfg    *config.Config
	logger *slog.Logger
}

func buildDaemon(cfg *config.Config, creds *auth.Credentials, githubURL string) (*daemon, error) {
	logMgr, err := logging.New(logging.LogConfig{
		Level:         cfg.Logging.Level,
		Format:        cfg.Logging.Format,
		Dir:           cfg.Logging.Dir,
		RetentionDays: cfg.Logging.RetentionDays,
		RunnerOutput:  cfg.Logging.RunnerOutput != nil && *cfg.Logging.RunnerOutput,
	})
	if err != nil {
		return nil, fmt.Errorf("setup logging: %w", err)
	}

	logger, err := logMgr.DaemonLogger()
	if err != nil {
		logMgr.Close()
		return nil, fmt.Errorf("create daemon logger: %w", err)
	}

	if err := logMgr.CleanupOldLogs(); err != nil {
		logger.Warn("log cleanup failed", "error", err)
	}

	ghClient, err := github.NewClient(creds, githubURL)
	if err != nil {
		logMgr.Close()
		return nil, fmt.Errorf("create github client: %w", err)
	}

	binaryMgr := runner.NewBinaryManager(cfg.Runner.CacheDir, logger)
	processMgr := runner.NewProcessManager(cfg.Runner.WorkdirBase, logger)

	if err := processMgr.CleanupStale(context.Background()); err != nil {
		logger.Warn("stale runner cleanup failed", "error", err)
	}
	processMgr.KillOrphanRunners(context.Background())

	notifSvc := buildNotificationService(cfg, logger)
	reporters := buildReporters(cfg, logger)

	ctrl := controller.New(
		ghClient, binaryMgr, processMgr, notifSvc, logMgr,
		cfg.Groups, controller.ControllerConfig{
			RunnerVersion: cfg.Runner.Version,
			RunnerGroupID: 1,
		}, logger,
	)

	var minDiskSpace int64
	if cfg.Health.MinDiskSpace != "" {
		minDiskSpace, _ = config.ParseByteSize(cfg.Health.MinDiskSpace)
	}

	healthMon := health.NewMonitor(health.MonitorConfig{
		Enabled:                cfg.Health.Enabled,
		CheckInterval:          cfg.Health.CheckInterval.Duration,
		RunnerTimeout:          cfg.Health.RunnerTimeout.Duration,
		IdleTimeout:            cfg.Health.IdleTimeout.Duration,
		DivergenceTimeout:      cfg.Health.DivergenceTimeout.Duration,
		MaxConsecutiveFailures: cfg.Health.MaxConsecutiveFailures,
		FailureCooldown:        cfg.Health.FailureCooldown.Duration,
		MinDiskSpace:           minDiskSpace,
		GroupMinRunners:        buildGroupMinRunners(cfg),
	}, notifSvc, ctrl, reporters, ctrl, logger)

	apiServer := api.NewServer(cfg.Daemon.StateDir, ctrl, healthMon, logger)

	return &daemon{
		ctrl:   ctrl,
		health: healthMon,
		api:    apiServer,
		logMgr: logMgr,
		cfg:    cfg,
		logger: logger,
	}, nil
}

func buildNotificationService(cfg *config.Config, logger *slog.Logger) *notification.Service {
	var providers []notification.Provider
	filters := make(map[string]notification.EventFilter)

	if cfg.Notifications.Discord.Enabled && cfg.Notifications.Discord.WebhookURL != "" {
		providers = append(providers, notification.NewDiscord(notification.DiscordConfig{
			WebhookURL: cfg.Notifications.Discord.WebhookURL,
			Username:   cfg.Notifications.Discord.Username,
			AvatarURL:  cfg.Notifications.Discord.AvatarURL,
			Mentions: notification.DiscordMentions{
				Error:    cfg.Notifications.Discord.Mentions.Error,
				Critical: cfg.Notifications.Discord.Mentions.Critical,
			},
		}))
		filters["discord"] = notification.EventFilter{
			Patterns: cfg.Notifications.Discord.Events,
		}
	}

	return notification.New(providers, filters, logger)
}

func buildReporters(cfg *config.Config, logger *slog.Logger) []health.Reporter {
	var reporters []health.Reporter

	logger.Debug("uptime-kuma config",
		"enabled", cfg.Monitoring.UptimeKuma.Enabled,
		"base_url_set", cfg.Monitoring.UptimeKuma.BaseURL != "",
		"daemon_token_set", cfg.Monitoring.UptimeKuma.DaemonToken != "",
		"group_tokens", len(cfg.Monitoring.UptimeKuma.GroupTokens),
	)
	if cfg.Monitoring.UptimeKuma.Enabled && cfg.Monitoring.UptimeKuma.BaseURL != "" {
		reporters = append(reporters, monitoring.NewUptimeKuma(monitoring.UptimeKumaConfig{
			BaseURL:            cfg.Monitoring.UptimeKuma.BaseURL,
			DaemonToken:        cfg.Monitoring.UptimeKuma.DaemonToken,
			GroupTokens:        cfg.Monitoring.UptimeKuma.GroupTokens,
			DegradedThreshold:  cfg.Monitoring.UptimeKuma.DegradedThreshold,
			ReportHealthAsPing: cfg.Monitoring.UptimeKuma.ReportHealthAsPing,
		}, logger))
	}

	return reporters
}

func resolveGitHubURL(creds *auth.Credentials, cfg *config.Config) (string, error) {
	if creds.GitHubURL != "" {
		return creds.GitHubURL, nil
	}
	if cfg.GitHub.URL != "" {
		return cfg.GitHub.URL, nil
	}
	return "", fmt.Errorf("github URL not configured: set it via 'ghr login' or in config github.url")
}

func pidFilePath(stateDir string) string {
	return filepath.Join(stateDir, "daemon.pid")
}

func writePIDFile(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create pid file directory %s: %w", dir, err)
	}
	pid := strconv.Itoa(os.Getpid())
	if err := os.WriteFile(path, []byte(pid), 0o644); err != nil {
		return fmt.Errorf("write pid file %s: %w", path, err)
	}
	return nil
}

func removePIDFile(path string) {
	_ = os.Remove(path)
}

func buildGroupMinRunners(cfg *config.Config) map[string]int {
	m := make(map[string]int, len(cfg.Groups))
	for _, g := range cfg.Groups {
		m[g.Name] = g.MinRunners
	}
	return m
}
