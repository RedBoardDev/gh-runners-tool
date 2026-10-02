package cgroup

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	minKernelMajor = 5
	minKernelMinor = 7
)

func (m *Manager) requireCloneIntoCgroup() error {
	release, ok := m.kernelRelease()
	if !ok {
		return nil
	}
	major, minor, ok := parseKernelVersion(release)
	if !ok {
		return nil
	}
	if major < minKernelMajor || (major == minKernelMajor && minor < minKernelMinor) {
		return fmt.Errorf("%w: kernel %s lacks CLONE_INTO_CGROUP, Linux %d.%d or newer is required",
			ErrUnavailable, release, minKernelMajor, minKernelMinor)
	}
	return nil
}

func parseKernelVersion(release string) (major, minor int, ok bool) {
	parts := strings.SplitN(release, ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	digits := strings.TrimRightFunc(parts[1], func(r rune) bool { return r < '0' || r > '9' })
	minor, err = strconv.Atoi(digits)
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

func (m *Manager) kernelRelease() (string, bool) {
	data, err := os.ReadFile(filepath.Join(m.procRoot, "sys", "kernel", "osrelease"))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(data)), true
}
