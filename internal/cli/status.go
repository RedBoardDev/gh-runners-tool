package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"gh-runners-tool/internal/config"
	"github.com/spf13/cobra"
)

func statusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show daemon presence (pid file)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("load config %s: %w", configPath, err)
			}

			pidBytes, err := os.ReadFile(pidFilePath())
			if err != nil {
				return fmt.Errorf("daemon not running or pid file missing (%s): %w", pidFilePath(), err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
			if err != nil {
				return fmt.Errorf("invalid pid file: %w", err)
			}

			alive, err := pidAlive(pid)
			if err != nil {
				return fmt.Errorf("probe daemon pid %d: %w", pid, err)
			}

			stats, total, warnings, err := collectRunnerStats(cfg)
			if err != nil {
				return err
			}

			cmd.Printf("daemon: %s (pid=%d)\n", ternary(alive, "running", "not responding"), pid)
			cmd.Printf("config: %s\n", configPath)

			for _, g := range cfg.Groups {
				s := stats[g.Name]
				cmd.Printf("group %-20s desired=%-3d running=%-3d stale=%-3d unknown=%-3d base=%s\n",
					g.Name, g.Count, s.Running, s.Stale, s.Unknown, g.WorkdirBase)
			}
			cmd.Printf("total runners: running=%d stale=%d unknown=%d\n", total.Running, total.Stale, total.Unknown)
			for _, w := range warnings {
				cmd.Printf("warning: %s\n", w)
			}
			return nil
		},
	}
	return cmd
}

type runnerStats struct {
	Running int
	Stale   int
	Unknown int
}

func collectRunnerStats(cfg *config.Config) (map[string]runnerStats, runnerStats, []string, error) {
	stats := make(map[string]runnerStats, len(cfg.Groups))
	for _, g := range cfg.Groups {
		stats[g.Name] = runnerStats{}
	}

	baseToGroup := make(map[string]string, len(cfg.Groups))
	for _, g := range cfg.Groups {
		baseToGroup[g.WorkdirBase] = g.Name
	}

	var total runnerStats
	var warnings []string

	for base, group := range baseToGroup {
		entries, err := os.ReadDir(base)
		if err != nil {
			if os.IsNotExist(err) {
				warnings = append(warnings, fmt.Sprintf("workdir base missing: %s", base))
				continue
			}
			return nil, total, warnings, fmt.Errorf("read workdir base %s: %w", base, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dir := filepath.Join(base, entry.Name())
			pidPath := filepath.Join(dir, ".ghr-pid")
			pidBytes, err := os.ReadFile(pidPath)
			if err != nil {
				stats[group] = addUnknown(stats[group])
				total.Unknown++
				continue
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
			if err != nil {
				stats[group] = addUnknown(stats[group])
				total.Unknown++
				continue
			}
			alive, err := pidAlive(pid)
			if err != nil {
				return nil, total, warnings, fmt.Errorf("probe runner pid %d (%s): %w", pid, dir, err)
			}
			if alive {
				stats[group] = addRunning(stats[group])
				total.Running++
			} else {
				stats[group] = addStale(stats[group])
				total.Stale++
			}
		}
	}
	return stats, total, warnings, nil
}

func pidAlive(pid int) (bool, error) {
	if pid <= 0 {
		return false, fmt.Errorf("invalid pid %d", pid)
	}
	err := syscall.Kill(pid, 0)
	if err == nil || errors.Is(err, syscall.EPERM) {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	return false, err
}

func addRunning(s runnerStats) runnerStats {
	s.Running++
	return s
}

func addStale(s runnerStats) runnerStats {
	s.Stale++
	return s
}

func addUnknown(s runnerStats) runnerStats {
	s.Unknown++
	return s
}

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}
