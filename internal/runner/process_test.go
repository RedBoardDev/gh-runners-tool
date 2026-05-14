package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

func TestPrepare(t *testing.T) {
	workdirBase := t.TempDir()
	cachedDir := t.TempDir()

	files := map[string]string{
		"run.sh":    "#!/bin/bash\necho run\n",
		"config.sh": "#!/bin/bash\necho config\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(cachedDir, name), []byte(content), 0o755); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	pm := NewProcessManager(workdirBase, silentLogger())
	instance := model.RunnerInstance{
		ID:    "abc123",
		Name:  "test-group-abc123",
		Group: "test-group",
	}

	workdir, err := pm.Prepare(context.Background(), &instance, cachedDir)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	expectedDir := filepath.Join(workdirBase, "test-group", "test-group-abc123")
	if workdir != expectedDir {
		t.Fatalf("expected workdir %q, got %q", expectedDir, workdir)
	}

	for name, content := range files {
		p := filepath.Join(workdir, name)
		data, readErr := os.ReadFile(p)
		if readErr != nil {
			t.Fatalf("read copied file %s: %v", name, readErr)
		}
		if string(data) != content {
			t.Fatalf("file %s content mismatch: got %q, want %q", name, string(data), content)
		}
	}
}

func TestCleanup(t *testing.T) {
	workdir := t.TempDir()
	sentinel := filepath.Join(workdir, "run.sh")
	if err := os.WriteFile(sentinel, []byte("#!/bin/bash\n"), 0o755); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	proc := &Process{
		Name:    "test-runner",
		Group:   "test-group",
		WorkDir: workdir,
		PID:     99999,
	}

	pm := NewProcessManager(filepath.Dir(workdir), silentLogger())
	if err := pm.Cleanup(proc); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	if _, err := os.Stat(workdir); !os.IsNotExist(err) {
		t.Fatalf("expected workdir to be removed, stat returned: %v", err)
	}
}
