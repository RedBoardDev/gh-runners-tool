package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/config"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/launchd"
	"github.com/spf13/cobra"
)

var (
	startExecutable       = os.Executable
	startLaunchdInstall   = launchd.Install
	startLaunchdUninstall = launchd.Uninstall
	startLaunchdIsRunning = launchd.IsRunning
	startLaunchdStatus    = launchd.Status
	startWaitForPID       = waitForPID
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
	if startLaunchdIsRunning(label) {
		pid, _ := startLaunchdStatus(label)
		fmt.Printf("ghr is already running (pid=%d)\n", pid)
		return nil
	}

	binaryPath, err := startExecutable()
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}

	cfgAbs, err := filepath.Abs(cfgFile)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}

	if err := startLaunchdUninstall(label); err != nil {
		return fmt.Errorf("cleanup stale launchd service: %w", err)
	}

	svcCfg := launchd.ServiceConfig{
		Label:      label,
		BinaryPath: binaryPath,
		ConfigPath: cfgAbs,
		LogDir:     cfg.Logging.Dir,
		StateDir:   cfg.Daemon.StateDir,
	}

	if err := startLaunchdInstall(&svcCfg); err != nil {
		return fmt.Errorf("install launchd service: %w", err)
	}

	startupTimeout := 15 * time.Second
	pid := startWaitForPID(cfg.Daemon.StateDir, startupTimeout)
	if pid == 0 {
		if err := startLaunchdUninstall(label); err != nil {
			return fmt.Errorf("daemon did not report a pid within %s; cleanup launchd service: %w", startupTimeout, err)
		}
		return fmt.Errorf("daemon did not report a pid within %s; check logs at %s", startupTimeout, filepath.Join(cfg.Logging.Dir, "daemon.err"))
	}

	serviceType := "LaunchAgent"
	if os.Getuid() == 0 {
		serviceType = "LaunchDaemon"
	}

	fmt.Printf("ghr started (pid=%d)\n", pid)
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
