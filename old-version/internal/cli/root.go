package cli

import (
	"os"
	"path/filepath"
	"time"

	"gh-runners-tool/internal/config"
	"github.com/spf13/cobra"
)

var (
	configPath string
	interval   time.Duration
)

func Execute() error {
	root := &cobra.Command{
		Use:   "ghr",
		Short: "GitHub runner controller (macOS)",
	}

	root.PersistentFlags().StringVar(&configPath, "config", "config.yaml", "Path to configuration file")
	root.PersistentFlags().DurationVar(&interval, "interval", 15*time.Second, "Reconciliation interval for daemon")

	root.AddCommand(daemonCmd())
	root.AddCommand(applyCmd())
	root.AddCommand(statusCmd())
	root.AddCommand(purgeCmd())

	return root.Execute()
}

func defaultStateDir() string {
	if dir := os.Getenv("GHR_STATE_DIR"); dir != "" {
		return dir
	}
	system := filepath.Join("/var/lib/ghr/state")
	if err := os.MkdirAll(system, 0o755); err == nil {
		return system
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return system
	}
	return filepath.Join(home, ".local", "state", "ghr")
}

func pidFilePath() string {
	return filepath.Join(defaultStateDir(), "daemon.pid")
}

func uniqueWorkdirs(cfg *config.Config) []string {
	seen := make(map[string]struct{})
	add := func(path string) {
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
	}
	add(cfg.Defaults.WorkdirBase)
	for _, g := range cfg.Groups {
		add(g.WorkdirBase)
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}
