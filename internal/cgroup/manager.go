package cgroup

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	DefaultRoot = "/sys/fs/cgroup"

	defaultProcRoot  = "/proc"
	daemonLeaf       = "daemon"
	runnerPrefix     = "runner-"
	removeRetries    = 20
	removeRetryDelay = 50 * time.Millisecond
)

type Manager struct {
	root     string
	procRoot string
	logger   *slog.Logger

	rmdir      func(string) error
	configure  func(dir string, limits Limits) error
	killPID    func(int) error
	retries    int
	retryDelay time.Duration

	mu   sync.Mutex
	base string
}

func NewManager(root string, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Manager{
		root:       root,
		procRoot:   defaultProcRoot,
		logger:     logger,
		rmdir:      syscall.Rmdir,
		configure:  applyLimits,
		killPID:    func(pid int) error { return syscall.Kill(pid, syscall.SIGKILL) },
		retries:    removeRetries,
		retryDelay: removeRetryDelay,
	}
}

func (m *Manager) UseProcRoot(procRoot string) *Manager {
	m.procRoot = procRoot
	return m
}

func controllersFor(needCPU bool) []string {
	if needCPU {
		return []string{"memory", "cpu"}
	}
	return []string{"memory"}
}

func (m *Manager) Setup(needCPU bool) error {
	if err := m.CheckUnified(); err != nil {
		return err
	}
	if err := m.requireCloneIntoCgroup(); err != nil {
		return err
	}
	base, err := m.cgroupDirOf("self")
	if err != nil {
		return err
	}
	if filepath.Clean(base) == filepath.Clean(m.root) {
		return fmt.Errorf("%w: ghr runs in the root cgroup; %s", ErrUnavailable, delegationHint)
	}
	if err := requireWritable(base); err != nil {
		return err
	}

	leaf := filepath.Join(base, daemonLeaf)
	if err := os.Mkdir(leaf, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("create daemon cgroup %s: %w", leaf, err)
	}
	if err := writeControl(filepath.Join(leaf, "cgroup.procs"), strconv.Itoa(os.Getpid())); err != nil {
		return fmt.Errorf("move ghr into daemon cgroup %s: %w", leaf, err)
	}
	enable := "+" + strings.Join(controllersFor(needCPU), " +")
	if err := writeControl(filepath.Join(base, "cgroup.subtree_control"), enable); err != nil {
		return fmt.Errorf("%w: enable %q in %s: %w; %s", ErrUnavailable, enable, base, err, delegationHint)
	}

	m.mu.Lock()
	m.base = base
	m.mu.Unlock()
	return nil
}

func (m *Manager) baseDir() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.base == "" {
		return "", ErrNotSetUp
	}
	return m.base, nil
}

func (m *Manager) CheckUnified() error {
	if _, err := os.Stat(filepath.Join(m.root, "cgroup.controllers")); err != nil {
		return fmt.Errorf("%w: cgroup v2 is not mounted at %s: %w", ErrUnavailable, m.root, err)
	}
	return nil
}

func (m *Manager) cgroupDirOf(proc string) (string, error) {
	data, err := os.ReadFile(filepath.Join(m.procRoot, proc, "cgroup"))
	if err != nil {
		return "", fmt.Errorf("read cgroup membership of %s: %w", proc, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if path, ok := strings.CutPrefix(line, "0::"); ok {
			path = strings.TrimSuffix(path, " (deleted)")
			return filepath.Join(m.root, filepath.Clean("/"+path)), nil
		}
	}
	return "", fmt.Errorf("%w: %s is not in a cgroup v2 hierarchy", ErrUnavailable, proc)
}

func requireWritable(base string) error {
	path := filepath.Join(base, "cgroup.subtree_control")
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("%w: %s is not writable: %w; %s", ErrUnavailable, path, err, delegationHint)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

func writeControl(path, value string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(value); err != nil {
		closeErr := f.Close()
		return errors.Join(err, closeErr)
	}
	return f.Close()
}
