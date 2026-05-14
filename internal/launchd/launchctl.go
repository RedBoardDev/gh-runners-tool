package launchd

import (
	"fmt"
	"os/exec"
)

func launchctlLoad(plistPath string) error {
	out, err := exec.Command("launchctl", "load", plistPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl load: %w: %s", err, string(out))
	}
	return nil
}

func launchctlUnload(plistPath string) error {
	out, err := exec.Command("launchctl", "unload", plistPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl unload: %w: %s", err, string(out))
	}
	return nil
}

func launchctlStart(label string) error {
	out, err := exec.Command("launchctl", "start", label).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl start: %w: %s", err, string(out))
	}
	return nil
}

func launchctlStop(label string) error {
	out, err := exec.Command("launchctl", "stop", label).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl stop: %w: %s", err, string(out))
	}
	return nil
}
