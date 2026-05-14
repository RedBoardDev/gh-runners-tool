package cli

import (
	"fmt"
	"syscall"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/launchd"
	"github.com/spf13/cobra"
)

func newStopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the ghr daemon",
		RunE:  runStop,
	}

	cmd.Flags().Duration("timeout", 30*time.Second, "max wait for graceful shutdown")
	cmd.Flags().Bool("force", false, "skip SIGTERM, send SIGKILL immediately")

	return cmd
}

func runStop(cmd *cobra.Command, args []string) error {
	timeout, err := cmd.Flags().GetDuration("timeout")
	if err != nil {
		return fmt.Errorf("get timeout flag: %w", err)
	}

	force, err := cmd.Flags().GetBool("force")
	if err != nil {
		return fmt.Errorf("get force flag: %w", err)
	}

	label := launchd.DefaultLabel()
	pid, running := launchd.Status(label)
	if !running {
		fmt.Println("ghr is not running")
		return nil
	}

	if force {
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
			return fmt.Errorf("send SIGKILL to pid %d: %w", pid, err)
		}
	} else {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return fmt.Errorf("send SIGTERM to pid %d: %w", pid, err)
		}

		if !waitForExit(pid, timeout) {
			fmt.Println("graceful shutdown timed out, sending SIGKILL")
			if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
				return fmt.Errorf("send SIGKILL to pid %d: %w", pid, err)
			}
		}
	}

	uninstallErr := launchd.Uninstall(label)
	if uninstallErr != nil {
		return fmt.Errorf("uninstall launchd service: %w", uninstallErr)
	}

	fmt.Println("ghr stopped")
	return nil
}

func waitForExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}
