package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/config"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/launchd"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show ghr daemon status",
		RunE:  runStatus,
	}

	cmd.Flags().Bool("json", false, "output in JSON format")
	cmd.Flags().Bool("watch", false, "live refresh mode")
	cmd.Flags().Duration("interval", 5*time.Second, "refresh interval for --watch")

	return cmd
}

func runStatus(cmd *cobra.Command, args []string) error {
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return fmt.Errorf("get json flag: %w", err)
	}

	stateDir := resolveStateDir()

	socketPath := filepath.Join(stateDir, "ghr.sock")
	resp, socketErr := querySocket(socketPath, "/status")

	if socketErr != nil {
		return showOfflineStatus(jsonOutput)
	}

	if jsonOutput {
		fmt.Println(string(resp))
		return nil
	}

	return displayStatus(resp)
}

func resolveStateDir() string {
	if cfgFile != "" {
		cfg, err := config.Load(cfgFile)
		if err == nil {
			return cfg.Daemon.StateDir
		}
	}

	if os.Getuid() == 0 {
		return "/var/lib/ghr/state"
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".local", "state", "ghr")
}

func querySocket(socketPath string, endpoint string) ([]byte, error) {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get("http://unix" + endpoint)
	if err != nil {
		return nil, fmt.Errorf("connect to daemon socket: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read socket response: %w", err)
	}

	return body, nil
}

func showOfflineStatus(jsonOutput bool) error {
	label := launchd.DefaultLabel()
	pid, running := launchd.Status(label)

	if jsonOutput {
		status := map[string]interface{}{
			"status":  "stopped",
			"running": running,
			"pid":     pid,
		}
		data, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal status: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Println("Service")
	if running {
		fmt.Printf("  Status:    running (via launchd, pid=%d)\n", pid)
		fmt.Println("  Note:      daemon socket not available")
	} else {
		fmt.Println("  Status:    stopped")
	}
	fmt.Println()
	fmt.Println("No active groups or runners.")
	fmt.Println("Use 'ghr start' to start the daemon.")

	return nil
}

func displayStatus(data []byte) error {
	var status struct {
		Groups map[string][]struct {
			Name  string `json:"name"`
			State string `json:"state"`
			PID   int    `json:"pid"`
		} `json:"groups"`
		Health struct {
			LastCheck string `json:"last_check"`
		} `json:"health"`
	}

	if err := json.Unmarshal(data, &status); err != nil {
		return fmt.Errorf("parse status response: %w", err)
	}

	label := launchd.DefaultLabel()
	pid, _ := launchd.Status(label)

	fmt.Println("Service")
	fmt.Println("  Status:    running")
	if pid > 0 {
		fmt.Printf("  PID:       %d\n", pid)
	}
	fmt.Println()

	fmt.Println("Groups")
	totalRunning := 0
	totalIdle := 0
	for group, runners := range status.Groups {
		running := 0
		idle := 0
		for _, r := range runners {
			if r.State == "busy" {
				running++
			} else {
				idle++
			}
		}
		totalRunning += running
		totalIdle += idle
		fmt.Printf("  %-20s  running=%d  idle=%d  total=%d\n", group, running, idle, len(runners))
	}
	fmt.Printf("  Total: running=%d  idle=%d\n", totalRunning, totalIdle)
	fmt.Println()

	fmt.Println("Health")
	fmt.Printf("  Last check: %s\n", status.Health.LastCheck)

	return nil
}
