package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/auth"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/config"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/github"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/launchd"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/state"
	"github.com/spf13/cobra"
)

func newPurgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "purge",
		Short: "Stop everything, delete scale sets, clean workdirs",
		RunE:  runPurge,
	}

	cmd.Flags().Duration("timeout", 5*time.Minute, "max wait for busy runners")
	cmd.Flags().Bool("force", false, "don't wait for busy runners")

	return cmd
}

func runPurge(cmd *cobra.Command, _ []string) error {
	if cfgFile == "" {
		return fmt.Errorf("--config is required")
	}

	timeout, err := cmd.Flags().GetDuration("timeout")
	if err != nil {
		return fmt.Errorf("get timeout flag: %w", err)
	}

	force, err := cmd.Flags().GetBool("force")
	if err != nil {
		return fmt.Errorf("get force flag: %w", err)
	}

	stopDaemonIfRunning()

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	creds, _, err := auth.Load(auth.LoadOpts{TokenFlag: tokenFlag})
	if err != nil {
		return fmt.Errorf("load credentials: %w", err)
	}

	githubURL, err := resolveGitHubURL(creds, cfg)
	if err != nil {
		return err
	}

	ghClient, err := github.NewClient(creds, githubURL)
	if err != nil {
		return fmt.Errorf("create github client: %w", err)
	}

	ctx := context.Background()
	deletedSets := purgeScaleSets(ctx, ghClient, cfg, force, timeout)
	cleanedDirs := cleanupWorkdirs(cfg.Runner.WorkdirBase)
	cleanupStateFiles(cfg.Daemon.StateDir)

	fmt.Printf("purge complete: deleted %d scale sets, cleaned %d workdirs\n", deletedSets, cleanedDirs)
	return nil
}

func stopDaemonIfRunning() {
	label := launchd.DefaultLabel()
	pid, running := launchd.Status(label)
	if !running {
		return
	}

	fmt.Printf("stopping running daemon (pid=%d)...\n", pid)
	sigErr := syscall.Kill(pid, syscall.SIGTERM)
	if sigErr != nil {
		fmt.Printf("  stop warning: %v\n", sigErr)
	} else {
		waitForExit(pid, 30*time.Second)
	}

	uninstallErr := launchd.Uninstall(label)
	if uninstallErr != nil {
		fmt.Printf("  uninstall warning: %v\n", uninstallErr)
	}
}

func purgeScaleSets(ctx context.Context, ghClient *github.Client, cfg *config.Config, force bool, timeout time.Duration) int {
	deletedSets := 0
	for _, g := range cfg.Groups {
		fmt.Printf("purging scale set %q...\n", g.Name)
		ss, getErr := ghClient.GetScaleSet(ctx, cfg.GitHub.RunnerGroupID, g.Name)
		if getErr != nil {
			fmt.Printf("  scale set %q not found, skipping\n", g.Name)
			continue
		}
		if ss == nil {
			continue
		}

		if !force {
			waitForIdleRunners(ctx, ghClient, ss.ID, g.Name, timeout)
		}

		if delErr := ghClient.DeleteScaleSet(ctx, ss.ID); delErr != nil {
			fmt.Printf("  failed to delete scale set %q: %v\n", g.Name, delErr)
			continue
		}
		deletedSets++
		fmt.Printf("  deleted scale set %q (id=%d)\n", g.Name, ss.ID)
	}
	return deletedSets
}

func waitForIdleRunners(ctx context.Context, ghClient *github.Client, scaleSetID int, name string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	pollInterval := 5 * time.Second

	for time.Now().Before(deadline) {
		ss, err := ghClient.GetScaleSetByID(ctx, scaleSetID)
		if err != nil {
			fmt.Printf("  warning: cannot check scale set %q status: %v\n", name, err)
			return
		}

		if ss.Statistics == nil || ss.Statistics.TotalBusyRunners == 0 {
			return
		}

		fmt.Printf("  waiting for %d busy runners in %q...\n", ss.Statistics.TotalBusyRunners, name)

		select {
		case <-ctx.Done():
			return
		case <-time.After(pollInterval):
		}
	}

	fmt.Printf("  timeout waiting for idle runners in %q, proceeding with delete\n", name)
}

func cleanupWorkdirs(workdirBase string) int {
	entries, err := os.ReadDir(workdirBase)
	if err != nil {
		return 0
	}

	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(workdirBase, e.Name())
		if rmErr := os.RemoveAll(p); rmErr != nil {
			fmt.Printf("  failed to remove workdir %s: %v\n", p, rmErr)
			continue
		}
		count++
	}
	return count
}

func cleanupStateFiles(stateDir string) {
	for _, p := range state.New(stateDir).All() {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			fmt.Printf("  failed to remove %s: %v\n", p, err)
		}
	}
}
