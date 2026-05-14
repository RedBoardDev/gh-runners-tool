package cli

import (
	"fmt"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/auth"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/config"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/logging"
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

func runRun(cmd *cobra.Command, args []string) error {
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

	if logLevel != "" {
		cfg.Logging.Level = logLevel
	}

	logMgr, err := logging.New(logging.LogConfig{
		Level:         cfg.Logging.Level,
		Format:        cfg.Logging.Format,
		Dir:           cfg.Logging.Dir,
		RetentionDays: cfg.Logging.RetentionDays,
		RunnerOutput:  cfg.Logging.RunnerOutput != nil && *cfg.Logging.RunnerOutput,
	})
	if err != nil {
		return fmt.Errorf("setup logging: %w", err)
	}
	defer logMgr.Close()

	logger, err := logMgr.DaemonLogger()
	if err != nil {
		return fmt.Errorf("create daemon logger: %w", err)
	}

	logger.Info("ghr starting",
		"config", cfgFile,
		"groups", len(cfg.Groups),
		"auth_source", source,
		"auth_method", creds.Method,
	)

	if err := logMgr.CleanupOldLogs(); err != nil {
		logger.Warn("log cleanup failed", "error", err)
	}

	logger.Info("ghr daemon: not yet fully implemented, would start here",
		"groups_count", len(cfg.Groups),
	)

	fmt.Printf("ghr run: config loaded (%d groups), auth from %s, daemon not yet implemented\n",
		len(cfg.Groups), source)

	return nil
}
