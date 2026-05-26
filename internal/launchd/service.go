package launchd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type ServiceConfig struct {
	Label      string
	BinaryPath string
	ConfigPath string
	LogDir     string
	StateDir   string
}

func DefaultLabel() string { return "com.ghr.daemon" }

func PlistPath(label string) string {
	if os.Getuid() == 0 {
		return filepath.Join("/Library", "LaunchDaemons", label+".plist")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
}

func Install(cfg *ServiceConfig) error {
	if err := ensureServiceDirectories(cfg); err != nil {
		return err
	}

	data, err := generatePlist(cfg)
	if err != nil {
		return fmt.Errorf("generate plist: %w", err)
	}

	plistPath := PlistPath(cfg.Label)
	dir := filepath.Dir(plistPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create plist directory %s: %w", dir, err)
	}

	if err := os.WriteFile(plistPath, data, 0o644); err != nil {
		return fmt.Errorf("write plist %s: %w", plistPath, err)
	}

	if err := launchctlBootstrap(plistPath); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w", err)
	}

	if err := launchctlKickstart(cfg.Label); err != nil {
		return fmt.Errorf("launchctl kickstart: %w", err)
	}

	return nil
}

func ensureServiceDirectories(cfg *ServiceConfig) error {
	dirs := []struct {
		name string
		path string
	}{
		{"log directory", cfg.LogDir},
		{"state directory", cfg.StateDir},
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir.path, 0o755); err != nil {
			return fmt.Errorf("create %s %s: %w", dir.name, dir.path, err)
		}
	}

	return nil
}

func Uninstall(label string) error {
	plistPath := PlistPath(label)

	_ = launchctlBootout(label)

	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist %s: %w", plistPath, err)
	}

	return nil
}

func IsRunning(label string) bool {
	_, running := Status(label)
	return running
}

func Status(label string) (int, bool) {
	out, err := exec.Command("launchctl", "list").Output()
	if err != nil {
		return 0, false
	}

	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, label) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if fields[2] != label {
			continue
		}
		pid, parseErr := strconv.Atoi(fields[0])
		if parseErr != nil || pid <= 0 {
			return 0, false
		}
		return pid, true
	}

	return 0, false
}
