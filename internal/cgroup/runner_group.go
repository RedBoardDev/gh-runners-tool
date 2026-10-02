package cgroup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Handle struct {
	dir *os.File
}

func (h *Handle) FD() uintptr {
	return h.dir.Fd()
}

func (h *Handle) Close() error {
	return h.dir.Close()
}

func (m *Manager) runnerDir(base, name string) string {
	return filepath.Join(base, runnerPrefix+name)
}

func (m *Manager) Create(name string, limits Limits) (*Handle, error) {
	if err := validName(name); err != nil {
		return nil, fmt.Errorf("create cgroup %q: %w", name, err)
	}
	base, err := m.baseDir()
	if err != nil {
		return nil, fmt.Errorf("create cgroup %q: %w", name, err)
	}

	dir := m.runnerDir(base, name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create cgroup %s: %w", dir, err)
	}

	handle, err := m.openConfigured(dir, limits)
	if err != nil {
		if rmErr := m.rmdir(dir); rmErr != nil {
			err = errors.Join(err, fmt.Errorf("remove cgroup %s: %w", dir, rmErr))
		}
		return nil, err
	}
	return handle, nil
}

func (m *Manager) openConfigured(dir string, limits Limits) (*Handle, error) {
	if err := m.configure(dir, limits); err != nil {
		return nil, err
	}
	f, err := os.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("open cgroup %s: %w", dir, err)
	}
	return &Handle{dir: f}, nil
}

func applyLimits(dir string, limits Limits) error {
	writes := []struct {
		file  string
		value string
		set   bool
	}{
		{"memory.max", strconv.FormatInt(limits.MemoryMax, 10), limits.MemoryMax > 0},
		{"memory.high", strconv.FormatInt(limits.MemoryHigh, 10), limits.MemoryHigh > 0},
		{"cpu.weight", strconv.Itoa(limits.CPUWeight), limits.CPUWeight > 0},
		{"memory.oom.group", "1", true},
	}
	for _, w := range writes {
		if !w.set {
			continue
		}
		if err := writeControl(filepath.Join(dir, w.file), w.value); err != nil {
			return fmt.Errorf("write %s in %s: %w", w.file, dir, err)
		}
	}
	return nil
}

func (m *Manager) Destroy(name string) error {
	if err := validName(name); err != nil {
		return fmt.Errorf("destroy cgroup %q: %w", name, err)
	}
	base, err := m.baseDir()
	if err != nil {
		return fmt.Errorf("destroy cgroup %q: %w", name, err)
	}
	return m.destroyDir(m.runnerDir(base, name))
}

func (m *Manager) SweepStale() (int, error) {
	base, err := m.baseDir()
	if err != nil {
		return 0, fmt.Errorf("sweep stale cgroups: %w", err)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return 0, fmt.Errorf("read cgroup %s: %w", base, err)
	}

	var errs []error
	swept := 0
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), runnerPrefix) {
			continue
		}
		if err := m.destroyDir(filepath.Join(base, entry.Name())); err != nil {
			errs = append(errs, err)
			continue
		}
		swept++
	}
	return swept, errors.Join(errs...)
}

func (m *Manager) destroyDir(dir string) error {
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat cgroup %s: %w", dir, err)
	}

	killErr := m.killAll(dir)
	rmErr := m.removeWithRetry(dir)
	if rmErr == nil {
		return nil
	}
	return errors.Join(killErr, rmErr)
}

func (m *Manager) killAll(dir string) error {
	err := writeControl(filepath.Join(dir, "cgroup.kill"), "1")
	if err == nil {
		return nil
	}
	data, readErr := os.ReadFile(filepath.Join(dir, "cgroup.procs"))
	if readErr != nil {
		return fmt.Errorf("kill cgroup %s: %w", dir, errors.Join(err, readErr))
	}
	var errs []error
	for _, field := range strings.Fields(string(data)) {
		pid, convErr := strconv.Atoi(field)
		if convErr != nil {
			continue
		}
		if killErr := m.killPID(pid); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
			errs = append(errs, fmt.Errorf("kill pid %d in cgroup %s: %w", pid, dir, killErr))
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) removeWithRetry(dir string) error {
	for attempt := 0; ; attempt++ {
		err := m.rmdir(dir)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if !errors.Is(err, syscall.EBUSY) || attempt >= m.retries {
			return fmt.Errorf("remove cgroup %s: %w", dir, err)
		}
		time.Sleep(m.retryDelay)
	}
}
