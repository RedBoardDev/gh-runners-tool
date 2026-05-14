package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupStale_DeadProcess(t *testing.T) {
	workdirBase := t.TempDir()

	groupDir := filepath.Join(workdirBase, "group-a")
	runnerDir := filepath.Join(groupDir, "runner-1")
	if err := os.MkdirAll(runnerDir, 0o755); err != nil {
		t.Fatalf("create runner dir: %v", err)
	}

	pidFile := filepath.Join(runnerDir, ".ghr-pid")
	if err := os.WriteFile(pidFile, []byte("9999999"), 0o644); err != nil {
		t.Fatalf("write PID file: %v", err)
	}

	pm := NewProcessManager(workdirBase, silentLogger())
	if err := pm.CleanupStale(context.Background()); err != nil {
		t.Fatalf("CleanupStale: %v", err)
	}

	if _, err := os.Stat(runnerDir); !os.IsNotExist(err) {
		t.Fatalf("expected runner dir to be removed, stat returned: %v", err)
	}
}

func TestCleanupStale_EmptyDir(t *testing.T) {
	workdirBase := t.TempDir()

	pm := NewProcessManager(workdirBase, silentLogger())
	if err := pm.CleanupStale(context.Background()); err != nil {
		t.Fatalf("CleanupStale on empty dir: %v", err)
	}
}

func TestCleanupStale_NonexistentDir(t *testing.T) {
	pm := NewProcessManager("/nonexistent/path/that/does/not/exist", silentLogger())
	if err := pm.CleanupStale(context.Background()); err != nil {
		t.Fatalf("CleanupStale on nonexistent dir: %v", err)
	}
}

func TestCleanupStale_NoPidFile(t *testing.T) {
	workdirBase := t.TempDir()

	groupDir := filepath.Join(workdirBase, "group-b")
	runnerDir := filepath.Join(groupDir, "runner-orphan")
	if err := os.MkdirAll(runnerDir, 0o755); err != nil {
		t.Fatalf("create runner dir: %v", err)
	}

	pm := NewProcessManager(workdirBase, silentLogger())
	if err := pm.CleanupStale(context.Background()); err != nil {
		t.Fatalf("CleanupStale: %v", err)
	}

	if _, err := os.Stat(runnerDir); !os.IsNotExist(err) {
		t.Fatalf("expected runner dir without PID file to be removed, stat returned: %v", err)
	}
}
