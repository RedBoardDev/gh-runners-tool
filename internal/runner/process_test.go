package runner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestRunnerEnvIsolatesHome(t *testing.T) {
	workdir := filepath.Join("runners", "g", "g-abc123")
	home := filepath.Join(workdir, "_home")
	tmp := filepath.Join(workdir, "_tmp")

	// Parent carries the shared host values that MUST be overridden — this is the
	// whole point: without isolation every concurrent runner shares these and
	// their caches race.
	parent := []string{
		"HOME=/home/host-user",
		"TMPDIR=/tmp/shared",
		"PATH=/usr/bin:/bin",
	}

	env := runnerEnv(parent, "jit-token", home, tmp)

	got := make(map[string]string, len(env))
	counts := make(map[string]int, len(env))
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
		counts[k]++
	}

	// Duplicate keys make getenv resolution platform dependent, so the override
	// could be silently ignored. There must be exactly one HOME.
	if counts["HOME"] != 1 {
		t.Fatalf("expected exactly one HOME entry, got %d", counts["HOME"])
	}
	if got["HOME"] != home {
		t.Fatalf("HOME not isolated: got %q, want %q", got["HOME"], home)
	}
	if got["TMPDIR"] != tmp {
		t.Fatalf("TMPDIR not isolated: got %q, want %q", got["TMPDIR"], tmp)
	}
	if got["ACTIONS_RUNNER_INPUT_JITCONFIG"] != "jit-token" {
		t.Fatalf("JIT config missing or wrong: got %q", got["ACTIONS_RUNNER_INPUT_JITCONFIG"])
	}
	if got["PATH"] != "/usr/bin:/bin" {
		t.Fatalf("non-overridden parent var must be preserved: PATH=%q", got["PATH"])
	}
	xdgKeys := []string{"XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME"}
	if runtime.GOOS != "darwin" {
		// The linux runner resolves _work from XDG_DATA_HOME, so overriding these
		// moves the job workspace somewhere the daemon never looks for it.
		for _, key := range xdgKeys {
			if got[key] != "" {
				t.Fatalf("%s must not be overridden on %s: got %q", key, runtime.GOOS, got[key])
			}
		}
		return
	}
	for _, key := range xdgKeys {
		if !strings.HasPrefix(got[key], home+string(filepath.Separator)) {
			t.Fatalf("%s must live under the isolated home: got %q", key, got[key])
		}
	}
}
