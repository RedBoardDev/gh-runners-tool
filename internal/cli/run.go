package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/auth"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/config"
	"github.com/oklog/run"
	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the ghr daemon in foreground",
		RunE:  runRun,
	}
	return cmd
}

func runRun(_ *cobra.Command, _ []string) error {
	if cfgFile == "" {
		return fmt.Errorf("--config is required")
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	creds, source, err := auth.Load(auth.LoadOpts{TokenFlag: tokenFlag})
	if err != nil {
		return err
	}

	githubURL, err := resolveGitHubURL(creds, cfg)
	if err != nil {
		return err
	}

	if logLevel != "" {
		cfg.Logging.Level = logLevel
	}

	d, err := buildDaemon(cfg, creds, githubURL)
	if err != nil {
		return err
	}
	defer d.logMgr.Close()

	d.logger.Info("ghr starting",
		"config", cfgFile,
		"groups", len(cfg.Groups),
		"auth_source", source,
		"auth_method", creds.Method,
	)

	pidPath := pidFilePath(cfg.Daemon.StateDir)
	if err := writePIDFile(pidPath); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer removePIDFile(pidPath)

	if err := writeDaemonState(cfg.Daemon.StateDir, cfgFile); err != nil {
		return fmt.Errorf("write daemon state: %w", err)
	}
	defer removeDaemonState(cfg.Daemon.StateDir)

	return runDaemonGroup(d)
}

func runDaemonGroup(d *daemon) error {
	var g run.Group

	{
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(
			safeActor(d.logger, "controller", func() error { return d.ctrl.Run(ctx) }),
			func(error) { cancel() },
		)
	}

	{
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(
			safeActor(d.logger, "health", func() error { return d.health.Run(ctx) }),
			func(error) { cancel() },
		)
	}

	{
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(
			safeActor(d.logger, "api", func() error { return d.api.Run(ctx) }),
			func(error) { cancel() },
		)
	}

	{
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(
			safeActor(d.logger, "log-cleanup", func() error { return d.logMgr.StartCleanupScheduler(ctx) }),
			func(error) { cancel() },
		)
	}

	{
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(
			safeActor(d.logger, "watchdog", func() error { return runWatchdog(ctx, d.cfg.Daemon.StateDir, d.logger) }),
			func(error) { cancel() },
		)
	}

	{
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		g.Add(
			func() error {
				<-ctx.Done()
				return nil
			},
			func(error) { cancel() },
		)
	}

	groupErr := g.Run()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), d.cfg.Daemon.ShutdownTimeout.Duration)
	defer cancel()
	d.ctrl.Shutdown(shutdownCtx)

	d.logger.Info("ghr stopped")
	return groupErr
}

func safeActor(logger *slog.Logger, name string, fn func() error) func() error {
	return func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				logger.Error("actor panicked", "actor", name, "panic", fmt.Sprintf("%v", r), "stack", string(stack))
				err = fmt.Errorf("actor %s panicked: %v", name, r)
			}
		}()
		return fn()
	}
}
