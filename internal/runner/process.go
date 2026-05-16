package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

const stopGracePeriod = 10 * time.Second

type Process struct {
	Name      string
	Group     string
	WorkDir   string
	PID       int32
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

func (m *ProcessManager) Prepare(ctx context.Context, instance *model.RunnerInstance, cachedDir string) (string, error) {
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

func (m *ProcessManager) Start(ctx context.Context, instance *model.RunnerInstance, workdir, jitConfig string, logFile io.Writer) (*Process, error) {
	runScript := filepath.Join(workdir, "run.sh")
	cmd := exec.CommandContext(ctx, runScript)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), "ACTIONS_RUNNER_INPUT_JITCONFIG="+jitConfig)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start runner %s: %w", instance.Name, err)
	}

	pid := int32(cmd.Process.Pid)
	pidFile := filepath.Join(workdir, ".ghr-pid")
	if err := os.WriteFile(pidFile, []byte(strconv.FormatInt(int64(pid), 10)), 0o644); err != nil {
		m.logger.WarnContext(ctx, "failed to write PID file", "path", pidFile, "error", err)
	}

	m.logger.InfoContext(ctx, "runner started", "runner", instance.Name, "pid", pid)

	return &Process{
		Name:      instance.Name,
		Group:     instance.Group,
		WorkDir:   workdir,
		PID:       pid,
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
		if isProcessFinished(err) {
			return nil
		}
		return fmt.Errorf("send SIGTERM to runner %s (pid %d): %w", proc.Name, proc.PID, err)
	}

	done := make(chan error, 1)
	go func() {
		done <- proc.Cmd.Wait()
	}()

	select {
	case err := <-done:
		if isExpectedExit(err) {
			return nil
		}
		return err
	case <-time.After(stopGracePeriod):
		m.logger.WarnContext(ctx, "runner did not exit after SIGTERM, sending SIGKILL", "runner", proc.Name, "pid", proc.PID)
		if err := proc.Cmd.Process.Kill(); err != nil {
			return fmt.Errorf("kill runner %s (pid %d): %w", proc.Name, proc.PID, err)
		}
		return <-done
	}
}

func isProcessFinished(err error) bool {
	return errors.Is(err, os.ErrProcessDone)
}

func isExpectedExit(err error) bool {
	if err == nil {
		return true
	}
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

func (m *ProcessManager) Cleanup(proc *Process) error {
	if err := os.RemoveAll(proc.WorkDir); err != nil {
		return fmt.Errorf("remove workdir %s: %w", proc.WorkDir, err)
	}
	return nil
}
