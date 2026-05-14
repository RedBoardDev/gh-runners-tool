package cli

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"gh-runners-tool/internal/config"
	"gh-runners-tool/internal/logging"
	"gh-runners-tool/internal/provider/github"
	"gh-runners-tool/internal/reconciler"
	"gh-runners-tool/internal/runner"
	"github.com/spf13/cobra"
)

func daemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run the controller daemon",
		RunE:  runDaemon,
	}
	return cmd
}

func runDaemon(cmd *cobra.Command, _ []string) error {
	logger := logging.New()

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_PAT")
	}
	if token == "" {
		return fmt.Errorf("GITHUB_TOKEN (or GITHUB_PAT) is required in environment")
	}

	if err := os.MkdirAll(defaultStateDir(), 0o755); err != nil {
		return fmt.Errorf("prepare state dir: %w", err)
	}
	if err := os.WriteFile(pidFilePath(), []byte(fmt.Sprintf("%d", os.Getpid())), 0o644); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer os.Remove(pidFilePath())

	gh := github.New(token)
	rm := runner.New(cfg.Defaults.CacheDir, logger)

	rm.CleanupStale(uniqueWorkdirs(cfg))

	logger.Printf("github cleanup: startup sweep")
	if err := cleanupGitHubRegistrations(context.Background(), gh, cfg, logger); err != nil {
		logger.Printf("warning: github cleanup failed: %v", err)
	}

	rec := reconciler.New(logger, gh, rm)
	rec.SetDesired(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGHUP)
		for range signals {
			logger.Printf("reload requested (SIGHUP)")
			updated, err := config.Load(configPath)
			if err != nil {
				logger.Printf("reload failed: %v", err)
				continue
			}
			rec.SetDesired(updated)
		}
	}()

	err = rec.Run(ctx, interval)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rec.Shutdown(shutdownCtx)
	logger.Printf("github cleanup: shutdown sweep")
	if err := cleanupGitHubRegistrations(shutdownCtx, gh, cfg, logger); err != nil {
		logger.Printf("warning: github cleanup (shutdown) failed: %v", err)
	}
	return err
}

func cleanupGitHubRegistrations(ctx context.Context, gh *github.Client, cfg *config.Config, logger *log.Logger) error {
	runners, err := gh.ListRunners(ctx, cfg.GitHub)
	if err != nil {
		return err
	}
	groupPrefixes := make(map[string]struct{}, len(cfg.Groups))
	for _, g := range cfg.Groups {
		groupPrefixes[g.Name+"-"] = struct{}{}
	}

	deleted := 0
	for _, rn := range runners {
		for prefix := range groupPrefixes {
			if strings.HasPrefix(rn.Name, prefix) {
				if err := gh.DeleteRunner(ctx, cfg.GitHub, rn.ID); err != nil {
					return fmt.Errorf("delete runner %s (%d): %w", rn.Name, rn.ID, err)
				}
				deleted++
				break
			}
		}
	}
	logger.Printf("github cleanup: inspected=%d deleted=%d", len(runners), deleted)
	return nil
}
