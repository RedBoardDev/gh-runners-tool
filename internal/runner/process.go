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
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/logging"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

const stopGracePeriod = 10 * time.Second

const killTimeout = 5 * time.Second

type Process struct {
	Name      string
	Group     string
	WorkDir   string
	PID       int32
	RunnerID  int
	StartedAt time.Time
	cmd       *exec.Cmd
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

	m.logger.DebugContext(ctx, "prepared runner workdir", "workdir", workdir, logging.KeyRunner, instance.Name)
	return workdir, nil
}

func (m *ProcessManager) Start(ctx context.Context, instance *model.RunnerInstance, workdir, jitConfig string, logFile io.Writer) (*Process, error) {
	runScript := filepath.Join(workdir, "run.sh")

	// Isolate per-runner HOME and TMPDIR. Runners run as the same macOS user, so
	// without this every concurrent job shares ~/.yarn, ~/.npm, ~/.cache and /tmp
	// — tools that mutate those caches (e.g. Yarn's global store) then race across
	// jobs and crash. These dirs live under the workdir, so Cleanup's RemoveAll
	// wipes them automatically when the ephemeral runner exits.
	runnerHome := filepath.Join(workdir, "_home")
	runnerTmp := filepath.Join(workdir, "_tmp")
	for _, dir := range []string{runnerHome, runnerTmp} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create isolated dir %s: %w", dir, err)
		}
	}

	cmd := exec.CommandContext(ctx, runScript)
	cmd.Dir = workdir
	cmd.Env = runnerEnv(os.Environ(), jitConfig, runnerHome, runnerTmp)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start runner %s: %w", instance.Name, err)
	}

	pid := int32(cmd.Process.Pid)
	pidFile := filepath.Join(workdir, ".ghr-pid")
	if err := os.WriteFile(pidFile, []byte(strconv.FormatInt(int64(pid), 10)), 0o644); err != nil {
		m.logger.WarnContext(ctx, "failed to write PID file", logging.KeyPath, pidFile, logging.KeyError, err)
	}

	m.logger.InfoContext(ctx, "runner started", logging.KeyRunner, instance.Name, logging.KeyPID, pid)

	return &Process{
		Name:      instance.Name,
		Group:     instance.Group,
		WorkDir:   workdir,
		PID:       pid,
		StartedAt: time.Now(),
		cmd:       cmd,
	}, nil
}

// runnerEnv builds the child environment from parent, overriding the keys that
// must be isolated per runner. Entries are replaced (not appended) so the child
// never sees duplicate keys — with duplicates, getenv resolution is platform
// dependent and our override could be silently ignored.
func runnerEnv(parent []string, jitConfig, runnerHome, runnerTmp string) []string {
	overrides := map[string]string{
		"ACTIONS_RUNNER_INPUT_JITCONFIG": jitConfig,
		"HOME":                           runnerHome,
		"TMPDIR":                         runnerTmp,
	}
	// XDG vars are safe to override on macOS (the runner ignores them).
	// On Linux the runner uses XDG_DATA_HOME for internal path resolution,
	// so overriding it redirects _work to an unexpected location.
	if runtime.GOOS == "darwin" {
		overrides["XDG_CACHE_HOME"] = filepath.Join(runnerHome, ".cache")
		overrides["XDG_CONFIG_HOME"] = filepath.Join(runnerHome, ".config")
		overrides["XDG_DATA_HOME"] = filepath.Join(runnerHome, ".local", "share")
	}

	env := make([]string, 0, len(parent)+len(overrides))
	for _, kv := range parent {
		key, _, found := strings.Cut(kv, "=")
		if found {
			if _, overridden := overrides[key]; overridden {
				continue
			}
		}
		env = append(env, kv)
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}

func (m *ProcessManager) Stop(ctx context.Context, proc *Process) error {
	if proc.cmd == nil || proc.cmd.Process == nil {
		return nil
	}

	m.logger.InfoContext(ctx, "stopping runner", logging.KeyRunner, proc.Name, logging.KeyPID, proc.PID)

	if err := proc.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		if isProcessFinished(err) {
			return nil
		}
		return fmt.Errorf("send SIGTERM to runner %s (pid %d): %w", proc.Name, proc.PID, err)
	}

	done := make(chan error, 1)
	go func() {
		done <- proc.cmd.Wait()
	}()

	select {
	case err := <-done:
		if isExpectedExit(err) {
			return nil
		}
		return err
	case <-time.After(stopGracePeriod):
		m.logger.WarnContext(ctx, "runner did not exit after SIGTERM, sending SIGKILL", logging.KeyRunner, proc.Name, logging.KeyPID, proc.PID)
		if err := proc.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("kill runner %s (pid %d): %w", proc.Name, proc.PID, err)
		}
		select {
		case err := <-done:
			if isExpectedExit(err) {
				return nil
			}
			return err
		case <-time.After(killTimeout):
			return fmt.Errorf("runner %s (pid %d) did not exit after SIGKILL within %s", proc.Name, proc.PID, killTimeout)
		}
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
