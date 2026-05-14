package runner

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

const stopGracePeriod = 10 * time.Second

type Process struct {
	Name      string
	Group     string
	WorkDir   string
	PID       int
	StartedAt time.Time
	Cmd       *exec.Cmd
}

type ProcessManager struct {
	workdirBase string
	logger      *slog.Logger
}

func NewProcessManager(workdirBase string, logger *slog.Logger) *ProcessManager {
	return &ProcessManager{
		workdirBase: workdirBase,
		logger:      logger,
	}
}

func (m *ProcessManager) Prepare(ctx context.Context, instance model.RunnerInstance, cachedDir string) (string, error) {
	workdir := filepath.Join(m.workdirBase, instance.Group, instance.Name)

	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return "", fmt.Errorf("create workdir %s: %w", workdir, err)
	}

	if err := copyDir(cachedDir, workdir); err != nil {
		return "", fmt.Errorf("copy runner bits to %s: %w", workdir, err)
	}

	m.logger.DebugContext(ctx, "prepared runner workdir", "workdir", workdir, "runner", instance.Name)
	return workdir, nil
}

func (m *ProcessManager) Start(ctx context.Context, instance model.RunnerInstance, workdir string, jitConfig string, logFile io.Writer) (*Process, error) {
	runScript := filepath.Join(workdir, "run.sh")
	cmd := exec.CommandContext(ctx, runScript)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), "ACTIONS_RUNNER_INPUT_JITCONFIG="+jitConfig)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start runner %s: %w", instance.Name, err)
	}

	pidFile := filepath.Join(workdir, ".ghr-pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		m.logger.WarnContext(ctx, "failed to write PID file", "path", pidFile, "error", err)
	}

	m.logger.InfoContext(ctx, "runner started", "runner", instance.Name, "pid", cmd.Process.Pid)

	return &Process{
		Name:      instance.Name,
		Group:     instance.Group,
		WorkDir:   workdir,
		PID:       cmd.Process.Pid,
		StartedAt: time.Now(),
		Cmd:       cmd,
	}, nil
}

func (m *ProcessManager) Stop(ctx context.Context, proc *Process) error {
	if proc.Cmd == nil || proc.Cmd.Process == nil {
		return nil
	}

	m.logger.InfoContext(ctx, "stopping runner", "runner", proc.Name, "pid", proc.PID)

	if err := proc.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("send SIGTERM to runner %s (pid %d): %w", proc.Name, proc.PID, err)
	}

	done := make(chan error, 1)
	go func() {
		done <- proc.Cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(stopGracePeriod):
		m.logger.WarnContext(ctx, "runner did not exit after SIGTERM, sending SIGKILL", "runner", proc.Name, "pid", proc.PID)
		if err := proc.Cmd.Process.Kill(); err != nil {
			return fmt.Errorf("kill runner %s (pid %d): %w", proc.Name, proc.PID, err)
		}
		return <-done
	}
}

func (m *ProcessManager) Cleanup(proc *Process) error {
	if err := os.RemoveAll(proc.WorkDir); err != nil {
		return fmt.Errorf("remove workdir %s: %w", proc.WorkDir, err)
	}
	return nil
}

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

func isProcessAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
