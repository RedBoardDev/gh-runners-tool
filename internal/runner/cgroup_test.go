package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/cgroup"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

type fakeCgroups struct {
	createErr  error
	destroyErr error
	created    []string
	destroyed  []string
}

func (f *fakeCgroups) Create(name string, _ cgroup.Limits) (*cgroup.Handle, error) {
	f.created = append(f.created, name)
	return nil, f.createErr
}

func (f *fakeCgroups) Destroy(name string) error {
	f.destroyed = append(f.destroyed, name)
	return f.destroyErr
}

func limitedGroups() map[string]cgroup.Limits {
	return map[string]cgroup.Limits{"limited": {MemoryMax: 1 << 30}}
}

func writeRunScript(t *testing.T, content string) string {
	t.Helper()
	workdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workdir, "run.sh"), []byte(content), 0o755); err != nil {
		t.Fatalf("write run.sh: %v", err)
	}
	return workdir
}

func TestStart_CgroupCreateFailureAbortsBeforeSpawning(t *testing.T) {
	createErr := errors.New("cgroup refused")
	cg := &fakeCgroups{createErr: createErr}
	pm := NewProcessManager(t.TempDir(), silentLogger(), WithCgroups(cg, limitedGroups()))
	workdir := writeRunScript(t, "#!/bin/sh\ntouch spawned\n")

	_, err := pm.Start(context.Background(), &model.RunnerInstance{Name: "limited-ab12", Group: "limited"}, workdir, "jit", nil)

	if !errors.Is(err, createErr) {
		t.Fatalf("Start() = %v, want the cgroup error", err)
	}
	if _, statErr := os.Stat(filepath.Join(workdir, ".ghr-pid")); statErr == nil {
		t.Error("a process was started although its cgroup could not be created")
	}
	if len(cg.destroyed) != 0 {
		t.Errorf("destroyed %v, nothing was created", cg.destroyed)
	}
}

func TestStart_GroupWithoutLimitsNeverTouchesCgroups(t *testing.T) {
	cg := &fakeCgroups{}
	pm := NewProcessManager(t.TempDir(), silentLogger(), WithCgroups(cg, limitedGroups()))
	workdir := writeRunScript(t, "#!/bin/sh\nexit 0\n")

	proc, err := pm.Start(context.Background(), &model.RunnerInstance{Name: "plain-ab12", Group: "plain"}, workdir, "jit", nil)
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	if stopErr := pm.Stop(context.Background(), proc); stopErr != nil {
		t.Fatalf("Stop() = %v", stopErr)
	}

	if len(cg.created) != 0 || len(cg.destroyed) != 0 {
		t.Fatalf("cgroups touched for an unlimited group: created=%v destroyed=%v", cg.created, cg.destroyed)
	}
}

func TestCleanup_DestroysCgroupOnlyForLimitedGroups(t *testing.T) {
	tests := []struct {
		name          string
		group         string
		manager       bool
		wantDestroyed []string
	}{
		{"limited group", "limited", true, []string{"limited-r1"}},
		{"unlimited group", "plain", true, nil},
		{"no cgroup manager configured", "limited", false, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cg := &fakeCgroups{}
			var opts []ProcessOption
			if tt.manager {
				opts = append(opts, WithCgroups(cg, limitedGroups()))
			}
			workdir := t.TempDir()
			pm := NewProcessManager(filepath.Dir(workdir), silentLogger(), opts...)

			err := pm.Cleanup(&Process{Name: tt.group + "-r1", Group: tt.group, WorkDir: workdir})
			if err != nil {
				t.Fatalf("Cleanup() = %v", err)
			}

			if len(cg.destroyed) != len(tt.wantDestroyed) {
				t.Fatalf("destroyed = %v, want %v", cg.destroyed, tt.wantDestroyed)
			}
			for i, want := range tt.wantDestroyed {
				if cg.destroyed[i] != want {
					t.Errorf("destroyed[%d] = %q, want %q", i, cg.destroyed[i], want)
				}
			}
		})
	}
}

func TestCleanup_CgroupRemovalFailureStillRemovesWorkdir(t *testing.T) {
	cg := &fakeCgroups{destroyErr: errors.New("cgroup busy")}
	workdir := t.TempDir()
	pm := NewProcessManager(filepath.Dir(workdir), silentLogger(), WithCgroups(cg, limitedGroups()))

	err := pm.Cleanup(&Process{Name: "limited-r1", Group: "limited", WorkDir: workdir})

	if err != nil {
		t.Fatalf("Cleanup() = %v, a stuck cgroup must not fail the workdir cleanup", err)
	}
	if _, statErr := os.Stat(workdir); !os.IsNotExist(statErr) {
		t.Errorf("workdir should be removed, stat: %v", statErr)
	}
	if len(cg.destroyed) != 1 {
		t.Errorf("destroy attempts = %d, want 1", len(cg.destroyed))
	}
}

func TestProcess_SelfExitIsReapedAndObservable(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), silentLogger())
	workdir := writeRunScript(t, "#!/bin/sh\nexit 0\n")

	proc, err := pm.Start(context.Background(), &model.RunnerInstance{Name: "plain-ab12", Group: "plain"}, workdir, "jit", nil)
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}

	select {
	case <-proc.Exited():
	case <-time.After(10 * time.Second):
		t.Fatal("exit of the runner process was never observed")
	}
	if killErr := syscall.Kill(int(proc.PID), 0); killErr == nil {
		t.Error("the exited process is still a zombie: liveness checks would keep passing")
	}
	if stopErr := pm.Stop(context.Background(), proc); stopErr != nil {
		t.Errorf("Stop() after a self-exit = %v, want nil", stopErr)
	}
}
