//go:build linux

package runner

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/cgroup"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

func TestCgroupIntegration_RunnerRunsInsideItsLimitedCgroup(t *testing.T) {
	if os.Getenv("GHR_TEST_CGROUPS") != "1" {
		t.Skip("needs a delegated cgroup v2 subtree: GHR_TEST_CGROUPS=1 systemd-run --scope -p Delegate=yes go test -run CgroupIntegration ./internal/runner")
	}

	logger := silentLogger()
	mgr := cgroup.NewManager(cgroup.DefaultRoot, logger)
	if err := mgr.Setup(true); err != nil {
		t.Fatalf("cgroup setup: %v", err)
	}
	limits := map[string]cgroup.Limits{"it": {MemoryMax: 256 << 20, CPUWeight: 50}}
	pm := NewProcessManager(t.TempDir(), logger, WithCgroups(mgr, limits))
	workdir := writeRunScript(t, "#!/bin/sh\nsleep 300 &\nwait\n")

	name := "it-" + strconv.Itoa(os.Getpid())
	proc, err := pm.Start(context.Background(), &model.RunnerInstance{Name: name, Group: "it"}, workdir, "jit", nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	membership, err := os.ReadFile("/proc/" + strconv.Itoa(int(proc.PID)) + "/cgroup")
	if err != nil {
		t.Fatalf("read membership: %v", err)
	}
	path := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(membership)), "0::"))
	if !strings.HasSuffix(path, "/runner-"+name) {
		t.Fatalf("runner is in %q, want a runner-%s cgroup", path, name)
	}

	dir := filepath.Join(cgroup.DefaultRoot, path)
	for file, want := range map[string]string{"memory.max": "268435456", "cpu.weight": "50", "memory.oom.group": "1"} {
		got, readErr := os.ReadFile(filepath.Join(dir, file))
		if readErr != nil || strings.TrimSpace(string(got)) != want {
			t.Errorf("%s = %q (%v), want %q", file, got, readErr, want)
		}
	}

	if err := pm.Stop(context.Background(), proc); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := pm.Cleanup(proc); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("cgroup %s survived teardown (orphaned background process?): %v", dir, err)
	}
}
