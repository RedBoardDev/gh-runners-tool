package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/state"
)

type daemonState struct {
	ConfigPath string    `json:"config_path"`
	StartedAt  time.Time `json:"started_at"`
	PID        int       `json:"pid"`
}

func writeDaemonState(stateDir, configPath string) error {
	ds := daemonState{
		ConfigPath: configPath,
		StartedAt:  time.Now(),
		PID:        os.Getpid(),
	}

	data, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal daemon state: %w", err)
	}

	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("create state directory %s: %w", stateDir, err)
	}

	path := state.New(stateDir).StateFile()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write daemon state %s: %w", path, err)
	}
	return nil
}

func readDaemonState(stateDir string) (*daemonState, error) {
	path := state.New(stateDir).StateFile()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read daemon state %s: %w", path, err)
	}

	var ds daemonState
	if err := json.Unmarshal(data, &ds); err != nil {
		return nil, fmt.Errorf("parse daemon state %s: %w", path, err)
	}
	return &ds, nil
}

func removeDaemonState(stateDir string) {
	_ = os.Remove(state.New(stateDir).StateFile())
}
