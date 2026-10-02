package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type cgroupFixture struct {
	root    string
	proc    string
	pidFile string
}

func newCgroupFixture(t *testing.T, controllers bool) cgroupFixture {
	t.Helper()
	dir := t.TempDir()
	f := cgroupFixture{
		root:    filepath.Join(dir, "cgroup"),
		proc:    filepath.Join(dir, "proc"),
		pidFile: filepath.Join(dir, "ghr.pid"),
	}
	write(t, filepath.Join(f.root, "system.slice", "ghr.service", "cgroup.subtree_control"), "cpu memory\n")
	if controllers {
		write(t, filepath.Join(f.root, "cgroup.controllers"), "cpu memory\n")
	}
	return f
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func (f cgroupFixture) daemonRunsIn(t *testing.T, cgroupPath string) {
	t.Helper()
	pid := strconv.Itoa(os.Getpid())
	write(t, f.pidFile, pid)
	write(t, filepath.Join(f.proc, pid, "cgroup"), "0::"+cgroupPath+"\n")
}

func (f cgroupFixture) check() CgroupCheck {
	return CgroupCheck{Enabled: true, Root: f.root, ProcRoot: f.proc, PIDFile: f.pidFile}
}

func TestCgroupCheck(t *testing.T) {
	tests := []struct {
		name        string
		controllers bool
		arrange     func(t *testing.T, f cgroupFixture)
		disabled    bool
		wantStatus  Status
		wantHint    string
	}{
		{
			name:       "no group sets resources",
			disabled:   true,
			wantStatus: StatusSkip,
		},
		{
			name:        "enforcement active",
			controllers: true,
			arrange: func(t *testing.T, f cgroupFixture) {
				f.daemonRunsIn(t, "/system.slice/ghr.service/daemon")
			},
			wantStatus: StatusOK,
		},
		{
			name:        "daemon outside its delegated leaf",
			controllers: true,
			arrange: func(t *testing.T, f cgroupFixture) {
				f.daemonRunsIn(t, "/system.slice/ghr.service")
			},
			wantStatus: StatusFail,
			wantHint:   "Delegate=yes",
		},
		{
			name:        "daemon not running",
			controllers: true,
			wantStatus:  StatusWarn,
			wantHint:    "Delegate=yes",
		},
		{
			name:        "cgroup v2 not mounted",
			controllers: false,
			wantStatus:  StatusFail,
			wantHint:    "cgroup v2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCgroupFixture(t, tt.controllers)
			if tt.arrange != nil {
				tt.arrange(t, f)
			}
			check := f.check()
			check.Enabled = !tt.disabled

			res := check.Run(context.Background())

			if res.Status != tt.wantStatus {
				t.Fatalf("status = %s (%s), want %s", res.Status, res.Summary, tt.wantStatus)
			}
			if tt.wantHint != "" && !strings.Contains(res.Hint, tt.wantHint) {
				t.Errorf("hint %q lacks %q", res.Hint, tt.wantHint)
			}
		})
	}
}

func TestCgroupCheck_Name(t *testing.T) {
	if got := (CgroupCheck{}).Name(); got != "cgroup" {
		t.Fatalf("Name() = %q, want cgroup", got)
	}
}

func TestDaemonPID_IgnoresStalePIDFiles(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"own pid is alive", strconv.Itoa(os.Getpid()), true},
		{"garbage", "not-a-pid", false},
		{"zero", "0", false},
		{"negative", "-4", false},
		{"dead pid", "2147483646", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, tt.name+".pid")
			write(t, path, tt.content)
			if _, got := daemonPID(path); got != tt.want {
				t.Fatalf("daemonPID() running = %v, want %v", got, tt.want)
			}
		})
	}
	if _, got := daemonPID(filepath.Join(dir, "missing.pid")); got {
		t.Fatal("a missing pid file must not count as running")
	}
}
