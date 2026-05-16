package launchd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

// domainTarget returns the launchctl service target prefix
// (e.g. "gui/501" or "system") suitable for bootstrap/bootout/kickstart.
func domainTarget() string {
	if os.Getuid() == 0 {
		return "system"
	}
	return "gui/" + strconv.Itoa(os.Getuid())
}

func launchctlBootstrap(plistPath string) error {
	out, err := exec.Command("launchctl", "bootstrap", domainTarget(), plistPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl bootstrap: %w: %s", err, string(out))
	}
	return nil
}

func launchctlBootout(label string) error {
	out, err := exec.Command("launchctl", "bootout", domainTarget()+"/"+label).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl bootout: %w: %s", err, string(out))
	}
	return nil
}

func launchctlKickstart(label string) error {
	out, err := exec.Command("launchctl", "kickstart", "-k", domainTarget()+"/"+label).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl kickstart: %w: %s", err, string(out))
	}
	return nil
}
