package runner

import (
	"fmt"
	"os/exec"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/cgroup"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/logging"
)

type cgroupManager interface {
	Create(name string, limits cgroup.Limits) (*cgroup.Handle, error)
	Destroy(name string) error
}

type ProcessOption func(*ProcessManager)

func WithCgroups(mgr cgroupManager, limits map[string]cgroup.Limits) ProcessOption {
	return func(m *ProcessManager) {
		m.cgroups = mgr
		m.cgroupLimits = limits
	}
}

func (m *ProcessManager) cgroupLimitsFor(group string) (cgroup.Limits, bool) {
	if m.cgroups == nil {
		return cgroup.Limits{}, false
	}
	limits, ok := m.cgroupLimits[group]
	return limits, ok
}

func (m *ProcessManager) placeInCgroup(cmd *exec.Cmd, name, group string) (release func(), err error) {
	limits, ok := m.cgroupLimitsFor(group)
	if !ok {
		return func() {}, nil
	}

	handle, err := m.cgroups.Create(name, limits)
	if err != nil {
		return nil, fmt.Errorf("create cgroup for runner %s: %w", name, err)
	}
	if err := attachCgroup(cmd, handle.FD()); err != nil {
		m.closeHandle(name, handle)
		m.destroyCgroup(name, group)
		return nil, fmt.Errorf("attach runner %s to cgroup: %w", name, err)
	}
	return func() { m.closeHandle(name, handle) }, nil
}

func (m *ProcessManager) closeHandle(name string, handle *cgroup.Handle) {
	if err := handle.Close(); err != nil {
		m.logger.Warn("failed to close cgroup handle", logging.KeyRunner, name, logging.KeyError, err)
	}
}

func (m *ProcessManager) destroyCgroup(name, group string) {
	if _, ok := m.cgroupLimitsFor(group); !ok {
		return
	}
	if err := m.cgroups.Destroy(name); err != nil {
		m.logger.Warn("failed to remove runner cgroup", logging.KeyRunner, name, logging.KeyError, err)
	}
}
