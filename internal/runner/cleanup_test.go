package runner

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
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
	stale, err := pm.CleanupStale(context.Background())
	if err != nil {
		t.Fatalf("CleanupStale: %v", err)
	}

	if _, err := os.Stat(runnerDir); !os.IsNotExist(err) {
		t.Fatalf("expected runner dir to be removed, stat returned: %v", err)
	}
	if want := map[string][]string{"group-a": {"runner-1"}}; !reflect.DeepEqual(stale, want) {
		t.Fatalf("stale runners = %v, want %v so their registrations can be removed", stale, want)
	}
}

func TestCleanupStale_EmptyDir(t *testing.T) {
	workdirBase := t.TempDir()

	pm := NewProcessManager(workdirBase, silentLogger())
	if _, err := pm.CleanupStale(context.Background()); err != nil {
		t.Fatalf("CleanupStale on empty dir: %v", err)
	}
}

func TestCleanupStale_NonexistentDir(t *testing.T) {
	pm := NewProcessManager("/nonexistent/path/that/does/not/exist", silentLogger())
	if _, err := pm.CleanupStale(context.Background()); err != nil {
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
	if _, err := pm.CleanupStale(context.Background()); err != nil {
		t.Fatalf("CleanupStale: %v", err)
	}

	if _, err := os.Stat(runnerDir); !os.IsNotExist(err) {
		t.Fatalf("expected runner dir without PID file to be removed, stat returned: %v", err)
	}
}
