package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func (m *ProcessManager) CleanupStale(ctx context.Context) error {
	entries, err := os.ReadDir(m.workdirBase)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read workdir base %s: %w", m.workdirBase, err)
	}

	for _, groupEntry := range entries {
		if !groupEntry.IsDir() {
			continue
		}
		if err := m.cleanupStaleGroup(ctx, groupEntry.Name()); err != nil {
			m.logger.WarnContext(ctx, "failed to cleanup stale group", "group", groupEntry.Name(), "error", err)
		}
	}

	return nil
}

func (m *ProcessManager) cleanupStaleGroup(ctx context.Context, group string) error {
	groupDir := filepath.Join(m.workdirBase, group)
	entries, err := os.ReadDir(groupDir)
	if err != nil {
		return fmt.Errorf("read group dir %s: %w", groupDir, err)
	}

	for _, runnerEntry := range entries {
		if !runnerEntry.IsDir() {
			continue
		}
		m.cleanupStaleRunner(ctx, group, runnerEntry.Name())
	}

	return nil
}

func (m *ProcessManager) cleanupStaleRunner(ctx context.Context, group, runner string) {
	runnerDir := filepath.Join(m.workdirBase, group, runner)
	pidFile := filepath.Join(runnerDir, ".ghr-pid")

	pidBytes, err := os.ReadFile(pidFile)
	if err != nil {
		m.logger.DebugContext(ctx, "no PID file found, removing stale workdir", "dir", runnerDir)
		removeErr := os.RemoveAll(runnerDir)
		if removeErr != nil {
			m.logger.WarnContext(ctx, "failed to remove stale workdir", "dir", runnerDir, "error", removeErr)
		}
		return
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		m.logger.WarnContext(ctx, "invalid PID file content, removing workdir", "dir", runnerDir, "error", err)
		removeErr := os.RemoveAll(runnerDir)
		if removeErr != nil {
			m.logger.WarnContext(ctx, "failed to remove stale workdir", "dir", runnerDir, "error", removeErr)
		}
		return
	}

	if isProcessAlive(pid) {
		m.logger.WarnContext(ctx, "killing stale runner process", "pid", pid, "runner", runner, "group", group)
		killErr := syscall.Kill(pid, syscall.SIGKILL)
		if killErr != nil {
			m.logger.WarnContext(ctx, "failed to kill stale process", "pid", pid, "error", killErr)
		}
	}

	removeErr := os.RemoveAll(runnerDir)
	if removeErr != nil {
		m.logger.WarnContext(ctx, "failed to remove stale workdir", "dir", runnerDir, "error", removeErr)
	} else {
		m.logger.InfoContext(ctx, "cleaned up stale runner", "runner", runner, "group", group, "pid", pid)
	}
}

func (m *ProcessManager) KillOrphanRunners(ctx context.Context) {
	out, err := exec.CommandContext(ctx, "pgrep", "-f", m.workdirBase).Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pid, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || pid <= 0 {
			continue
		}
		m.logger.WarnContext(ctx, "killing orphan runner process", "pid", pid)
		syscall.Kill(pid, syscall.SIGKILL)
	}
}

func isProcessAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
