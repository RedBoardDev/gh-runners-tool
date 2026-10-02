package cgroup

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

const ownCgroupPath = "/system.slice/ghr.service"

type fakeHost struct {
	root string
	proc string
	base string
	mgr  *Manager
}

func newFakeHost(t *testing.T) *fakeHost {
	t.Helper()
	dir := t.TempDir()
	h := &fakeHost{
		root: filepath.Join(dir, "cgroup"),
		proc: filepath.Join(dir, "proc"),
	}
	h.base = filepath.Join(h.root, ownCgroupPath)

	mustMkdir(t, h.base)
	mustWrite(t, filepath.Join(h.root, "cgroup.controllers"), "cpuset cpu io memory pids")
	mustWrite(t, filepath.Join(h.base, "cgroup.subtree_control"), "")
	mustWrite(t, filepath.Join(h.base, "cgroup.procs"), strconv.Itoa(os.Getpid())+"\n")
	h.setProcCgroup(t, "self", "0::"+ownCgroupPath+"\n")
	mustMkdir(t, filepath.Join(h.proc, "sys", "kernel"))
	mustWrite(t, filepath.Join(h.proc, "sys", "kernel", "osrelease"), "6.1.0-18-amd64\n")

	h.mgr = NewManager(h.root, nil)
	h.mgr.procRoot = h.proc
	h.mgr.rmdir = os.RemoveAll
	h.mgr.retryDelay = 0
	h.mgr.killPID = func(int) error { return nil }
	return h
}

func (h *fakeHost) setProcCgroup(t *testing.T, proc, content string) {
	t.Helper()
	mustMkdir(t, filepath.Join(h.proc, proc))
	mustWrite(t, filepath.Join(h.proc, proc, "cgroup"), content)
}

func (h *fakeHost) setUp(t *testing.T) {
	t.Helper()
	if err := h.mgr.Setup(true); err != nil {
		t.Fatalf("Setup: %v", err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestSetup_PreparesDelegatedCgroup(t *testing.T) {
	h := newFakeHost(t)

	h.setUp(t)

	leaf := filepath.Join(h.base, "daemon")
	if info, err := os.Stat(leaf); err != nil || !info.IsDir() {
		t.Fatalf("daemon leaf missing: %v", err)
	}
	if got := readFile(t, filepath.Join(leaf, "cgroup.procs")); got != strconv.Itoa(os.Getpid()) {
		t.Errorf("daemon leaf procs = %q, want own pid", got)
	}
	if got := readFile(t, filepath.Join(h.base, "cgroup.subtree_control")); got != "+memory +cpu" {
		t.Errorf("subtree_control = %q, want %q", got, "+memory +cpu")
	}

	t.Run("running Setup again is harmless", func(t *testing.T) {
		if err := h.mgr.Setup(true); err != nil {
			t.Fatalf("second Setup: %v", err)
		}
	})
}

func TestSetup_ErrorsWhenCgroupsAreNotDelegated(t *testing.T) {
	tests := []struct {
		name      string
		arrange   func(t *testing.T, h *fakeHost)
		wantIs    error
		wantInErr string
	}{
		{
			name: "cgroup v2 not mounted",
			arrange: func(t *testing.T, h *fakeHost) {
				if err := os.Remove(filepath.Join(h.root, "cgroup.controllers")); err != nil {
					t.Fatal(err)
				}
			},
			wantIs:    ErrUnavailable,
			wantInErr: "cgroup v2 is not mounted",
		},
		{
			name: "process only in a v1 hierarchy",
			arrange: func(t *testing.T, h *fakeHost) {
				h.setProcCgroup(t, "self", "12:memory:/user.slice\n1:name=systemd:/user.slice\n")
			},
			wantIs:    ErrUnavailable,
			wantInErr: "not in a cgroup v2 hierarchy",
		},
		{
			name: "process in the root cgroup",
			arrange: func(t *testing.T, h *fakeHost) {
				h.setProcCgroup(t, "self", "0::/\n")
			},
			wantIs:    ErrUnavailable,
			wantInErr: "root cgroup",
		},
		{
			name: "own cgroup has no writable subtree_control",
			arrange: func(t *testing.T, h *fakeHost) {
				if err := os.Remove(filepath.Join(h.base, "cgroup.subtree_control")); err != nil {
					t.Fatal(err)
				}
			},
			wantIs:    ErrUnavailable,
			wantInErr: "Delegate=yes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newFakeHost(t)
			tt.arrange(t, h)

			err := h.mgr.Setup(true)
			if !errors.Is(err, tt.wantIs) {
				t.Fatalf("Setup() = %v, want %v", err, tt.wantIs)
			}
			if !strings.Contains(err.Error(), tt.wantInErr) {
				t.Errorf("error %q lacks %q", err.Error(), tt.wantInErr)
			}
			if _, statErr := os.Stat(filepath.Join(h.base, "daemon")); statErr == nil {
				t.Error("daemon leaf must not be created when delegation is missing")
			}
		})
	}
}

func TestSetup_ErrorsWhenSubtreeControlIsReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	h := newFakeHost(t)
	if err := os.Chmod(filepath.Join(h.base, "cgroup.subtree_control"), 0o444); err != nil {
		t.Fatal(err)
	}

	err := h.mgr.Setup(true)
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "Delegate=yes") {
		t.Fatalf("Setup() = %v, want ErrUnavailable mentioning Delegate=yes", err)
	}
}

func TestCreate_WritesExactLimits(t *testing.T) {
	tests := []struct {
		name   string
		limits Limits
		want   map[string]string
		absent []string
	}{
		{
			name:   "all limits",
			limits: Limits{MemoryMax: 12 << 30, MemoryHigh: 11 << 30, CPUWeight: 100},
			want: map[string]string{
				"memory.max":       "12884901888",
				"memory.high":      "11811160064",
				"cpu.weight":       "100",
				"memory.oom.group": "1",
			},
		},
		{
			name:   "memory max only",
			limits: Limits{MemoryMax: 1 << 30},
			want:   map[string]string{"memory.max": "1073741824", "memory.oom.group": "1"},
			absent: []string{"memory.high", "cpu.weight"},
		},
		{
			name:   "cpu weight only",
			limits: Limits{CPUWeight: 10000},
			want:   map[string]string{"cpu.weight": "10000", "memory.oom.group": "1"},
			absent: []string{"memory.max", "memory.high"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newFakeHost(t)
			h.setUp(t)

			handle, err := h.mgr.Create("grp-ab12", tt.limits)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			defer handle.Close()

			dir := filepath.Join(h.base, "runner-grp-ab12")
			for file, want := range tt.want {
				if got := readFile(t, filepath.Join(dir, file)); got != want {
					t.Errorf("%s = %q, want %q", file, got, want)
				}
			}
			for _, file := range tt.absent {
				if _, err := os.Stat(filepath.Join(dir, file)); err == nil {
					t.Errorf("%s must not be written when the limit is unset", file)
				}
			}
			if handle.FD() == 0 {
				t.Error("handle must expose the cgroup directory descriptor")
			}
		})
	}
}

func TestCreate_Errors(t *testing.T) {
	t.Run("invalid names are rejected", func(t *testing.T) {
		h := newFakeHost(t)
		h.setUp(t)
		for _, name := range []string{"", ".", "..", "a/b", "../escape", "nul\x00"} {
			if _, err := h.mgr.Create(name, Limits{MemoryMax: 1}); !errors.Is(err, ErrInvalidName) {
				t.Errorf("Create(%q) = %v, want ErrInvalidName", name, err)
			}
		}
	})

	t.Run("before Setup", func(t *testing.T) {
		h := newFakeHost(t)
		if _, err := h.mgr.Create("r", Limits{}); !errors.Is(err, ErrNotSetUp) {
			t.Fatalf("Create() = %v, want ErrNotSetUp", err)
		}
	})

	t.Run("name already taken", func(t *testing.T) {
		h := newFakeHost(t)
		h.setUp(t)
		mustMkdir(t, filepath.Join(h.base, "runner-dup"))
		if _, err := h.mgr.Create("dup", Limits{MemoryMax: 1}); err == nil {
			t.Fatal("Create() succeeded over an existing cgroup")
		}
	})

	t.Run("failed limit write removes the cgroup", func(t *testing.T) {
		h := newFakeHost(t)
		h.setUp(t)
		writeErr := errors.New("write refused")
		h.mgr.configure = func(string, Limits) error { return writeErr }
		var removed []string
		h.mgr.rmdir = func(path string) error {
			removed = append(removed, path)
			return os.RemoveAll(path)
		}

		_, err := h.mgr.Create("bad", Limits{MemoryMax: 1})
		if !errors.Is(err, writeErr) {
			t.Fatalf("Create() = %v, want the write error", err)
		}
		dir := filepath.Join(h.base, "runner-bad")
		if len(removed) != 1 || removed[0] != dir {
			t.Errorf("removed = %v, want [%s]", removed, dir)
		}
		if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
			t.Errorf("half-configured cgroup left behind: %v", statErr)
		}
	})
}

func TestDestroy_KillsThenRemoves(t *testing.T) {
	h := newFakeHost(t)
	h.setUp(t)
	handle, err := h.mgr.Create("job", Limits{MemoryMax: 1 << 20})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	dir := filepath.Join(h.base, "runner-job")
	var killSeenBeforeRemove string
	h.mgr.rmdir = func(path string) error {
		killSeenBeforeRemove = readFile(t, filepath.Join(path, "cgroup.kill"))
		return os.RemoveAll(path)
	}

	if err := h.mgr.Destroy("job"); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if killSeenBeforeRemove != "1" {
		t.Errorf("cgroup.kill = %q when the directory was removed, want \"1\"", killSeenBeforeRemove)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("cgroup directory still present: %v", err)
	}
}

func TestDestroy_MissingCgroupIsFine(t *testing.T) {
	h := newFakeHost(t)
	h.setUp(t)

	if err := h.mgr.Destroy("never-existed"); err != nil {
		t.Fatalf("Destroy() = %v, want nil for ENOENT", err)
	}
}

func TestDestroy_RemoveRetries(t *testing.T) {
	tests := []struct {
		name         string
		failures     int
		failWith     error
		wantAttempts int
		wantErr      bool
	}{
		{"succeeds first time", 0, syscall.EBUSY, 1, false},
		{"recovers after EBUSY", 3, syscall.EBUSY, 4, false},
		{"gives up after bounded retries", 1000, syscall.EBUSY, removeRetries + 1, true},
		{"other errors are not retried", 1000, syscall.EPERM, 1, true},
		{"ENOENT during removal is fine", 1000, syscall.ENOENT, 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newFakeHost(t)
			h.setUp(t)
			mustMkdir(t, filepath.Join(h.base, "runner-r"))

			attempts := 0
			h.mgr.rmdir = func(string) error {
				attempts++
				if attempts <= tt.failures {
					return tt.failWith
				}
				return nil
			}

			err := h.mgr.Destroy("r")
			if (err != nil) != tt.wantErr {
				t.Fatalf("Destroy() = %v, wantErr %v", err, tt.wantErr)
			}
			if attempts != tt.wantAttempts {
				t.Errorf("rmdir attempts = %d, want %d", attempts, tt.wantAttempts)
			}
			if tt.wantErr && !errors.Is(err, tt.failWith) {
				t.Errorf("error %v does not wrap %v", err, tt.failWith)
			}
		})
	}
}

func TestDestroy_FallsBackToKillingListedProcesses(t *testing.T) {
	h := newFakeHost(t)
	h.setUp(t)
	dir := filepath.Join(h.base, "runner-old")
	mustMkdir(t, filepath.Join(dir, "cgroup.kill"))
	mustWrite(t, filepath.Join(dir, "cgroup.procs"), "101\n102\nnot-a-pid\n")

	var killed []int
	h.mgr.killPID = func(pid int) error {
		killed = append(killed, pid)
		if pid == 102 {
			return syscall.ESRCH
		}
		return nil
	}
	h.mgr.rmdir = func(string) error { return nil }

	if err := h.mgr.Destroy("old"); err != nil {
		t.Fatalf("Destroy() = %v", err)
	}
	if len(killed) != 2 || killed[0] != 101 || killed[1] != 102 {
		t.Errorf("killed = %v, want [101 102]", killed)
	}
}

func TestSweepStale(t *testing.T) {
	h := newFakeHost(t)
	h.setUp(t)
	for _, name := range []string{"runner-a", "runner-b", "daemon", "unrelated"} {
		mustMkdir(t, filepath.Join(h.base, name))
	}
	mustWrite(t, filepath.Join(h.base, "runner-file"), "not a cgroup")

	swept, err := h.mgr.SweepStale()
	if err != nil {
		t.Fatalf("SweepStale: %v", err)
	}
	if swept != 2 {
		t.Errorf("swept = %d, want 2", swept)
	}
	for _, gone := range []string{"runner-a", "runner-b"} {
		if _, err := os.Stat(filepath.Join(h.base, gone)); !os.IsNotExist(err) {
			t.Errorf("%s should have been removed", gone)
		}
	}
	for _, kept := range []string{"daemon", "unrelated", "runner-file"} {
		if _, err := os.Stat(filepath.Join(h.base, kept)); err != nil {
			t.Errorf("%s must be left alone: %v", kept, err)
		}
	}

	t.Run("reports what it could not remove", func(t *testing.T) {
		h := newFakeHost(t)
		h.setUp(t)
		mustMkdir(t, filepath.Join(h.base, "runner-ok"))
		mustMkdir(t, filepath.Join(h.base, "runner-stuck"))
		h.mgr.rmdir = func(path string) error {
			if filepath.Base(path) == "runner-stuck" {
				return syscall.EPERM
			}
			return os.RemoveAll(path)
		}

		swept, err := h.mgr.SweepStale()
		if swept != 1 || !errors.Is(err, syscall.EPERM) {
			t.Fatalf("SweepStale() = %d, %v; want 1 swept and EPERM", swept, err)
		}
	})

	t.Run("before Setup", func(t *testing.T) {
		if _, err := newFakeHost(t).mgr.SweepStale(); !errors.Is(err, ErrNotSetUp) {
			t.Fatalf("SweepStale() = %v, want ErrNotSetUp", err)
		}
	})
}

func TestVerifyEnforcement(t *testing.T) {
	tests := []struct {
		name       string
		membership string
		controls   string
		wantErr    error
	}{
		{"enforcing", "0::" + ownCgroupPath + "/daemon\n", "cpu memory\n", nil},
		{"controllers enabled in another order", "0::" + ownCgroupPath + "/daemon\n", "memory cpu io\n", nil},
		{"daemon outside its leaf", "0::" + ownCgroupPath + "\n", "cpu memory\n", ErrNotEnforcing},
		{"cpu controller missing", "0::" + ownCgroupPath + "/daemon\n", "memory\n", ErrNotEnforcing},
		{"no controllers at all", "0::" + ownCgroupPath + "/daemon\n", "\n", ErrNotEnforcing},
		{"daemon in a v1 hierarchy", "1:name=systemd:/x\n", "", ErrUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newFakeHost(t)
			h.setProcCgroup(t, "4242", tt.membership)
			mustWrite(t, filepath.Join(h.base, "cgroup.subtree_control"), tt.controls)

			base, err := h.mgr.VerifyEnforcement(4242, true)
			if tt.wantErr == nil {
				if err != nil || base != h.base {
					t.Fatalf("VerifyEnforcement() = %q, %v; want %q, nil", base, err, h.base)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("VerifyEnforcement() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestCheckUnified(t *testing.T) {
	h := newFakeHost(t)
	if err := h.mgr.CheckUnified(); err != nil {
		t.Fatalf("CheckUnified() = %v, want nil", err)
	}
	if err := os.Remove(filepath.Join(h.root, "cgroup.controllers")); err != nil {
		t.Fatal(err)
	}
	if err := h.mgr.CheckUnified(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("CheckUnified() = %v, want ErrUnavailable", err)
	}
}

func TestSetup_EnablesOnlyNeededControllers(t *testing.T) {
	tests := []struct {
		name    string
		needCPU bool
		want    string
	}{
		{"memory only", false, "+memory"},
		{"memory and cpu", true, "+memory +cpu"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newFakeHost(t)
			if err := h.mgr.Setup(tt.needCPU); err != nil {
				t.Fatalf("Setup: %v", err)
			}
			if got := readFile(t, filepath.Join(h.base, "cgroup.subtree_control")); got != tt.want {
				t.Fatalf("subtree_control = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSetup_FailsFastOnKernelWithoutCloneIntoCgroup(t *testing.T) {
	tests := []struct {
		release string
		wantErr bool
	}{
		{"4.19.0", true},
		{"5.6.19-generic", true},
		{"5.7.0", false},
		{"5.15.0-91-generic", false},
		{"6.1.0", false},
		{"garbage", false},
	}
	for _, tt := range tests {
		t.Run(tt.release, func(t *testing.T) {
			h := newFakeHost(t)
			mustWrite(t, filepath.Join(h.proc, "sys", "kernel", "osrelease"), tt.release+"\n")

			err := h.mgr.Setup(false)

			if tt.wantErr != (err != nil) {
				t.Fatalf("Setup() = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "CLONE_INTO_CGROUP") {
					t.Errorf("error %v should explain the kernel requirement", err)
				}
				if _, statErr := os.Stat(filepath.Join(h.base, "daemon")); statErr == nil {
					t.Error("nothing may be reconfigured on an unsupported kernel")
				}
			}
		})
	}
}
