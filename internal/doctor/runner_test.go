package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunnerCheck_NotConfiguredIsFail(t *testing.T) {
	res := RunnerCheck{}.Run(context.Background())
	if res.Status != StatusFail {
		t.Errorf("status = %s, want FAIL", res.Status)
	}
}

func TestRunnerCheck_MissingDirIsWarn(t *testing.T) {
	res := RunnerCheck{CacheDir: filepath.Join(t.TempDir(), "absent")}.Run(context.Background())
	if res.Status != StatusWarn {
		t.Errorf("status = %s, want WARN", res.Status)
	}
}

func TestRunnerCheck_EmptyDirIsWarn(t *testing.T) {
	res := RunnerCheck{CacheDir: t.TempDir()}.Run(context.Background())
	if res.Status != StatusWarn {
		t.Errorf("status = %s, want WARN", res.Status)
	}
}

func TestRunnerCheck_VersionWithCompleteMarkerIsOK(t *testing.T) {
	dir := t.TempDir()
	v := filepath.Join(dir, "2.319.1")
	if err := os.MkdirAll(v, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(v, ".complete"), nil, 0o644); err != nil {
		t.Fatalf("marker: %v", err)
	}
	res := RunnerCheck{CacheDir: dir}.Run(context.Background())
	if res.Status != StatusOK {
		t.Errorf("status = %s, want OK", res.Status)
	}
}

func TestRunnerCheck_IncompleteVersionIsIgnored(t *testing.T) {
	dir := t.TempDir()
	v := filepath.Join(dir, "2.319.1")
	if err := os.MkdirAll(v, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	res := RunnerCheck{CacheDir: dir}.Run(context.Background())
	if res.Status != StatusWarn {
		t.Errorf("status = %s, want WARN", res.Status)
	}
}
