package launchd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultLabel(t *testing.T) {
	label := DefaultLabel()
	if label != "com.ghr.daemon" {
		t.Errorf("DefaultLabel() = %q, want %q", label, "com.ghr.daemon")
	}
}

func TestPlistPath_NonRoot(t *testing.T) {
	path := PlistPath("com.ghr.daemon")
	if !strings.HasSuffix(path, "Library/LaunchAgents/com.ghr.daemon.plist") &&
		!strings.HasSuffix(path, "Library/LaunchDaemons/com.ghr.daemon.plist") {
		t.Errorf("PlistPath() = %q, expected LaunchAgents or LaunchDaemons suffix", path)
	}
}

func TestPlistPath_ContainsLabel(t *testing.T) {
	tests := []struct {
		name  string
		label string
	}{
		{"default label", "com.ghr.daemon"},
		{"custom label", "com.ghr.test"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := PlistPath(tc.label)
			if !strings.Contains(path, tc.label+".plist") {
				t.Errorf("PlistPath(%q) = %q, missing label in path", tc.label, path)
			}
		})
	}
}

func TestStatus_NotRunning(t *testing.T) {
	pid, running := Status("com.ghr.test.nonexistent.label.12345")
	if running {
		t.Errorf("Status() running = true for nonexistent label")
	}
	if pid != 0 {
		t.Errorf("Status() pid = %d, want 0", pid)
	}
}

func TestIsRunning_NotRunning(t *testing.T) {
	if IsRunning("com.ghr.test.nonexistent.label.12345") {
		t.Error("IsRunning() = true for nonexistent label")
	}
}

func TestEnsureServiceDirectoriesCreatesRuntimeDirs(t *testing.T) {
	dir := t.TempDir()
	cfg := ServiceConfig{
		LogDir:   filepath.Join(dir, "logs"),
		StateDir: filepath.Join(dir, "state"),
	}

	if err := ensureServiceDirectories(&cfg); err != nil {
		t.Fatalf("ensureServiceDirectories() error = %v", err)
	}

	for _, path := range []string{cfg.LogDir, cfg.StateDir} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", path)
		}
	}
}
