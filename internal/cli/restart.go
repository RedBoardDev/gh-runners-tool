package cli

import (
	"fmt"
	"syscall"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/launchd"
	"github.com/spf13/cobra"
)

func newRestartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart the ghr daemon",
		RunE:  runRestart,
	}
	return cmd
}

func runRestart(cmd *cobra.Command, args []string) error {
	if cfgFile == "" {
		stateDir := resolveStateDir()
		if state, err := readDaemonState(stateDir); err == nil && state.ConfigPath != "" {
			cfgFile = state.ConfigPath
		}
	}

	label := launchd.DefaultLabel()
	if launchd.IsRunning(label) {
		pid, _ := launchd.Status(label)
		fmt.Printf("stopping ghr (pid=%d)...\n", pid)

		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			fmt.Printf("stop warning: %v\n", err)
		} else {
			waitForExit(pid, 30*time.Second)
		}

		if err := launchd.Uninstall(label); err != nil {
			fmt.Printf("uninstall warning: %v\n", err)
		}
	}

	return runStart(cmd, args)
}
