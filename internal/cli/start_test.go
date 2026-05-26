package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/launchd"
)

func TestRunStartReturnsErrorWhenDaemonDoesNotWritePID(t *testing.T) {
	oldCfgFile := cfgFile
	oldExecutable := startExecutable
	oldIsRunning := startLaunchdIsRunning
	oldStatus := startLaunchdStatus
	oldInstall := startLaunchdInstall
	oldUninstall := startLaunchdUninstall
	oldWaitForPID := startWaitForPID
	t.Cleanup(func() {
		cfgFile = oldCfgFile
		startExecutable = oldExecutable
		startLaunchdIsRunning = oldIsRunning
		startLaunchdStatus = oldStatus
		startLaunchdInstall = oldInstall
		startLaunchdUninstall = oldUninstall
		startWaitForPID = oldWaitForPID
	})

	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	logDir := filepath.Join(dir, "logs")
	cfgFile = filepath.Join(dir, "config.yaml")
	yaml := strings.Join([]string{
		"groups:",
		"  - name: runners",
		"    max_runners: 1",
		"logging:",
		"  dir: " + logDir,
		"daemon:",
		"  state_dir: " + stateDir,
	}, "\n")
	if err := os.WriteFile(cfgFile, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var installed launchd.ServiceConfig
	uninstallCalls := 0
	startExecutable = func() (string, error) { return "/usr/local/bin/ghr", nil }
	startLaunchdIsRunning = func(string) bool { return false }
	startLaunchdStatus = func(string) (int, bool) { return 0, false }
	startLaunchdInstall = func(cfg *launchd.ServiceConfig) error {
		installed = *cfg
		return nil
	}
	startLaunchdUninstall = func(string) error {
		uninstallCalls++
		return nil
	}
	startWaitForPID = func(gotStateDir string, timeout time.Duration) int {
		if gotStateDir != stateDir {
			t.Fatalf("waitForPID stateDir = %q, want %q", gotStateDir, stateDir)
		}
		if timeout <= 0 {
			t.Fatalf("waitForPID timeout = %s, want positive", timeout)
		}
		return 0
	}

	err := runStart(newStartCmd(), nil)
	if err == nil {
		t.Fatal("runStart() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "daemon did not report a pid") {
		t.Fatalf("runStart() error = %q, want daemon pid failure", err)
	}
	if installed.StateDir != stateDir {
		t.Fatalf("installed StateDir = %q, want %q", installed.StateDir, stateDir)
	}
	if installed.LogDir != logDir {
		t.Fatalf("installed LogDir = %q, want %q", installed.LogDir, logDir)
	}
	if uninstallCalls != 2 {
		t.Fatalf("uninstall calls = %d, want 2 (stale cleanup and startup-timeout cleanup)", uninstallCalls)
	}
}

func TestRunStartRemovesStaleLaunchdJobBeforeInstall(t *testing.T) {
	oldCfgFile := cfgFile
	oldExecutable := startExecutable
	oldIsRunning := startLaunchdIsRunning
	oldStatus := startLaunchdStatus
	oldInstall := startLaunchdInstall
	oldUninstall := startLaunchdUninstall
	oldWaitForPID := startWaitForPID
	t.Cleanup(func() {
		cfgFile = oldCfgFile
		startExecutable = oldExecutable
		startLaunchdIsRunning = oldIsRunning
		startLaunchdStatus = oldStatus
		startLaunchdInstall = oldInstall
		startLaunchdUninstall = oldUninstall
		startWaitForPID = oldWaitForPID
	})

	dir := t.TempDir()
	cfgFile = filepath.Join(dir, "config.yaml")
	yaml := strings.Join([]string{
		"groups:",
		"  - name: runners",
		"    max_runners: 1",
		"logging:",
		"  dir: " + filepath.Join(dir, "logs"),
		"daemon:",
		"  state_dir: " + filepath.Join(dir, "state"),
	}, "\n")
	if err := os.WriteFile(cfgFile, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var events []string
	startExecutable = func() (string, error) { return "/usr/local/bin/ghr", nil }
	startLaunchdIsRunning = func(string) bool { return false }
	startLaunchdStatus = func(string) (int, bool) { return 0, false }
	startLaunchdUninstall = func(string) error {
		events = append(events, "uninstall")
		return nil
	}
	startLaunchdInstall = func(*launchd.ServiceConfig) error {
		events = append(events, "install")
		return nil
	}
	startWaitForPID = func(string, time.Duration) int {
		events = append(events, "wait")
		return 123
	}

	if err := runStart(newStartCmd(), nil); err != nil {
		t.Fatalf("runStart() error = %v", err)
	}

	want := []string{"uninstall", "install", "wait"}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v", events, want)
	}
}
