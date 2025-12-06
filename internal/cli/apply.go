package cli

import (
	"fmt"
	"os"
	"strconv"
	"syscall"

	"gh-runners-tool/internal/config"
	"github.com/spf13/cobra"
)

func applyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Validate config and signal daemon to reload",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := config.Load(configPath); err != nil {
				return fmt.Errorf("load config %s: %w", configPath, err)
			}

			pidBytes, err := os.ReadFile(pidFilePath())
			if err != nil {
				return fmt.Errorf("read daemon pid from %s: %w", pidFilePath(), err)
			}
			pid, err := strconv.Atoi(string(pidBytes))
			if err != nil {
				return fmt.Errorf("invalid pid file: %w", err)
			}
			if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
				return fmt.Errorf("signal daemon: %w", err)
			}
			cmd.Println("reload signal sent to daemon")
			return nil
		},
	}
	return cmd
}
