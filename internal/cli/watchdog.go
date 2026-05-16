package cli

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/state"
)

const (
	watchdogInterval         = 30 * time.Second
	watchdogTimeout          = 5 * time.Second
	watchdogFailureThreshold = 3
)

// runWatchdog probes the daemon's own /health endpoint over the unix socket.
// After watchdogFailureThreshold consecutive failures it logs critical and
// exits with code 2 so launchd can respawn the process.
func runWatchdog(ctx context.Context, stateDir string, logger *slog.Logger) error {
	socketPath := state.New(stateDir).Socket()
	client := &http.Client{
		Timeout: watchdogTimeout,
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
	}

	// Allow the API server to come up before starting to probe.
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(watchdogInterval):
	}

	ticker := time.NewTicker(watchdogInterval)
	defer ticker.Stop()

	failures := 0
	for {
		if probeOK(ctx, client) {
			failures = 0
		} else {
			failures++
			logger.Warn("watchdog probe failed", "consecutive_failures", failures)
			if failures >= watchdogFailureThreshold {
				logger.Error("watchdog tripped; exiting for launchd to respawn",
					"threshold", watchdogFailureThreshold)
				os.Exit(2)
			}
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func probeOK(ctx context.Context, client *http.Client) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/health", http.NoBody)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 500
}
