package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/launchd"
)

type statusResponse struct {
	Groups map[string][]statusRunner `json:"groups"`
	Health statusHealth              `json:"health"`
}

type statusRunner struct {
	Name    string `json:"name"`
	State   string `json:"state"`
	PID     int32  `json:"pid"`
	JobName string `json:"job_name"`
}

type statusHealthIssue struct {
	Level   string `json:"level"`
	Type    string `json:"type"`
	Group   string `json:"group"`
	Runner  string `json:"runner"`
	Message string `json:"message"`
}

type statusHealth struct {
	LastCheck string              `json:"last_check"`
	Issues    []statusHealthIssue `json:"issues"`
}

func showOfflineStatus(stateDir string, jsonOutput bool) error {
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

	if state, readErr := readDaemonState(stateDir); readErr == nil {
		fmt.Printf("  Config:    %s\n", state.ConfigPath)
		fmt.Printf("  Started:   %s\n", state.StartedAt.Format(time.RFC3339))
	}

	fmt.Println()
	fmt.Println("No active groups or runners.")
	fmt.Println("Use 'ghr start' to start the daemon.")

	return nil
}

func displayStatus(data []byte) error {
	var status statusResponse
	if err := json.Unmarshal(data, &status); err != nil {
		return fmt.Errorf("parse status response: %w", err)
	}

	label := launchd.DefaultLabel()
	pid, _ := launchd.Status(label)

	renderServiceSection(pid, "")
	renderGroupsTable(status.Groups)
	renderRunnersTable(status.Groups)
	renderHealthSection(status.Health)

	return nil
}

func renderServiceSection(pid int, configPath string) {
	fmt.Println("Service")
	fmt.Println("  Status:    running")
	if pid > 0 {
		fmt.Printf("  PID:       %d\n", pid)
	}
	if configPath != "" {
		fmt.Printf("  Config:    %s\n", configPath)
	}
	fmt.Println()
}

func renderGroupsTable(groups map[string][]statusRunner) {
	fmt.Println("Groups")
	fmt.Printf("  %-20s %5s %7s %5s %8s\n", "Name", "Max", "Running", "Idle", "Health")
	fmt.Printf("  %-20s %5s %7s %5s %8s\n", "----", "---", "-------", "----", "------")

	totalRunning := 0
	totalIdle := 0

	for group, runners := range groups {
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
		fmt.Printf("  %-20s %5d %7d %5d %8s\n", group, len(runners), running, idle, "OK")
	}

	fmt.Printf("  Total: running=%d  idle=%d\n", totalRunning, totalIdle)
	fmt.Println()
}

func renderRunnersTable(groups map[string][]statusRunner) {
	hasRunners := false
	for _, runners := range groups {
		if len(runners) > 0 {
			hasRunners = true
			break
		}
	}
	if !hasRunners {
		return
	}

	fmt.Println("Runners")
	fmt.Printf("  %-30s %-8s %-25s %6s\n", "Runner", "Status", "Job", "PID")
	fmt.Printf("  %-30s %-8s %-25s %6s\n", "------", "------", "---", "---")

	for _, runners := range groups {
		for _, r := range runners {
			job := r.JobName
			if job == "" {
				job = "-"
			}
			fmt.Printf("  %-30s %-8s %-25s %6d\n", r.Name, r.State, job, r.PID)
		}
	}
	fmt.Println()
}

func renderHealthSection(h statusHealth) {
	fmt.Println("Health")
	if h.LastCheck != "" {
		fmt.Printf("  Last check:  %s\n", h.LastCheck)
	} else {
		fmt.Println("  Last check:  n/a")
	}
	fmt.Printf("  Issues:      %d\n", len(h.Issues))
	for _, issue := range h.Issues {
		fmt.Printf("    [%s] %s: %s\n", issue.Level, issue.Type, issue.Message)
	}
}
