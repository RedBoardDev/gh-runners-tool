package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const stateFileName = "daemon.state.json"

type daemonState struct {
	ConfigPath string    `json:"config_path"`
	StartedAt  time.Time `json:"started_at"`
	PID        int       `json:"pid"`
}

func writeDaemonState(stateDir, configPath string) error {
	state := daemonState{
		ConfigPath: configPath,
		StartedAt:  time.Now(),
		PID:        os.Getpid(),
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal daemon state: %w", err)
	}

	dir := stateDir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create state directory %s: %w", dir, err)
	}

	path := filepath.Join(dir, stateFileName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write daemon state %s: %w", path, err)
	}
	return nil
}

func readDaemonState(stateDir string) (*daemonState, error) {
	path := filepath.Join(stateDir, stateFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read daemon state %s: %w", path, err)
	}

	var state daemonState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse daemon state %s: %w", path, err)
	}
	return &state, nil
}

func removeDaemonState(stateDir string) {
	path := filepath.Join(stateDir, stateFileName)
	_ = os.Remove(path)
}
