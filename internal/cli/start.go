package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/config"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/launchd"
	"github.com/spf13/cobra"
)

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the ghr daemon via launchd",
		RunE:  runStart,
	}

	cmd.Flags().Bool("foreground", false, "run in foreground (same as 'ghr run')")

	return cmd
}

func runStart(cmd *cobra.Command, args []string) error {
	foreground, err := cmd.Flags().GetBool("foreground")
	if err == nil && foreground {
		return runRun(cmd, args)
	}

	if cfgFile == "" {
		return fmt.Errorf("--config is required")
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	label := launchd.DefaultLabel()
	if launchd.IsRunning(label) {
		pid, _ := launchd.Status(label)
		fmt.Printf("ghr is already running (pid=%d)\n", pid)
		return nil
	}

	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}

	svcCfg := launchd.ServiceConfig{
		Label:      label,
		BinaryPath: binaryPath,
		ConfigPath: cfgFile,
		LogDir:     cfg.Logging.Dir,
		StateDir:   cfg.Daemon.StateDir,
	}

	if err := launchd.Install(&svcCfg); err != nil {
		return fmt.Errorf("install launchd service: %w", err)
	}

	pid := waitForPID(cfg.Daemon.StateDir, 5*time.Second)

	serviceType := "LaunchAgent"
	if os.Getuid() == 0 {
		serviceType = "LaunchDaemon"
	}

	if pid > 0 {
		fmt.Printf("ghr started (pid=%d)\n", pid)
	} else {
		fmt.Println("ghr started")
	}
	fmt.Printf("Service: %s (%s)\n", label, serviceType)
	fmt.Printf("Config:  %s\n", cfgFile)
	fmt.Printf("Groups:  %d", len(cfg.Groups))
	if len(cfg.Groups) > 0 {
		fmt.Print(" (")
		for i, g := range cfg.Groups {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Print(g.Name)
		}
		fmt.Print(")")
	}
	fmt.Println()
	fmt.Printf("Logs:    %s\n", cfg.Logging.Dir)

	return nil
}

func waitForPID(stateDir string, timeout time.Duration) int {
	pidPath := pidFilePath(stateDir)
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidPath)
		if err == nil && len(data) > 0 {
			var pid int
			if _, scanErr := fmt.Sscanf(string(data), "%d", &pid); scanErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	return 0
}
