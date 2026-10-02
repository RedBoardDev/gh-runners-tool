package cgroup

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

func (m *Manager) VerifyEnforcement(pid int, needCPU bool) (string, error) {
	if err := m.CheckUnified(); err != nil {
		return "", err
	}
	dir, err := m.cgroupDirOf(strconv.Itoa(pid))
	if err != nil {
		return "", err
	}
	if filepath.Base(dir) != daemonLeaf {
		return "", fmt.Errorf("%w: daemon (pid %d) runs in %s, not in a %q leaf cgroup", ErrNotEnforcing, pid, dir, daemonLeaf)
	}

	base := filepath.Dir(dir)
	data, err := os.ReadFile(filepath.Join(base, "cgroup.subtree_control"))
	if err != nil {
		return "", fmt.Errorf("read subtree_control of %s: %w", base, err)
	}
	enabled := strings.Fields(string(data))
	for _, controller := range controllersFor(needCPU) {
		if !slices.Contains(enabled, controller) {
			return "", fmt.Errorf("%w: controller %q is not enabled in %s", ErrNotEnforcing, controller, base)
		}
	}
	return base, nil
}
