package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/config"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/state"
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

func runStatus(cmd *cobra.Command, _ []string) error {
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return fmt.Errorf("get json flag: %w", err)
	}

	watch, err := cmd.Flags().GetBool("watch")
	if err != nil {
		return fmt.Errorf("get watch flag: %w", err)
	}

	interval, err := cmd.Flags().GetDuration("interval")
	if err != nil {
		return fmt.Errorf("get interval flag: %w", err)
	}

	stateDir := resolveStateDir()
	socketPath := state.New(stateDir).Socket()

	if !watch {
		return renderOnce(socketPath, stateDir, jsonOutput)
	}

	return runWatch(cmd.Context(), socketPath, stateDir, jsonOutput, interval)
}

func renderOnce(socketPath, stateDir string, jsonOutput bool) error {
	resp, socketErr := querySocket(socketPath, "/status")
	if socketErr != nil {
		return showOfflineStatus(stateDir, jsonOutput)
	}

	if jsonOutput {
		fmt.Println(string(resp))
		return nil
	}

	return displayStatus(resp)
}

func runWatch(ctx context.Context, socketPath, stateDir string, jsonOutput bool, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if !jsonOutput {
			fmt.Print("\033[H\033[2J")
		}

		renderErr := renderOnce(socketPath, stateDir, jsonOutput)
		if renderErr != nil && !jsonOutput {
			fmt.Fprintf(os.Stderr, "status error: %v\n", renderErr)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
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

func querySocket(socketPath, endpoint string) ([]byte, error) {
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
