package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type LogManager struct {
	cfg     LogConfig
	rootDir string
	level   slog.Level

	mu      sync.Mutex
	writers []*dateAwareWriter
}

func New(cfg LogConfig) (*LogManager, error) {
	if cfg.Dir == "" {
		return nil, fmt.Errorf("logging: dir must not be empty")
	}

	daemonDir := filepath.Join(cfg.Dir, "daemon")
	groupsDir := filepath.Join(cfg.Dir, "groups")

	if err := os.MkdirAll(daemonDir, 0o755); err != nil {
		return nil, fmt.Errorf("logging: create daemon dir: %w", err)
	}
	if err := os.MkdirAll(groupsDir, 0o755); err != nil {
		return nil, fmt.Errorf("logging: create groups dir: %w", err)
	}

	return &LogManager{
		cfg:     cfg,
		rootDir: cfg.Dir,
		level:   ParseLevel(cfg.Level),
	}, nil
}

func (m *LogManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var firstErr error
	for _, w := range m.writers {
		if err := w.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	m.writers = nil
	return firstErr
}

func (m *LogManager) trackWriter(w *dateAwareWriter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writers = append(m.writers, w)
}

func (m *LogManager) consoleHandler() slog.Handler {
	opts := &slog.HandlerOptions{Level: m.level}
	if strings.EqualFold(m.cfg.Format, "json") {
		return slog.NewJSONHandler(os.Stderr, opts)
	}
	return slog.NewTextHandler(os.Stderr, opts)
}

func (m *LogManager) fileHandler(subdir string) (slog.Handler, error) {
	dir := filepath.Join(m.rootDir, subdir)
	w, err := newDateAwareWriter(dir)
	if err != nil {
		return nil, err
	}
	m.trackWriter(w)
	opts := &slog.HandlerOptions{Level: m.level}
	return slog.NewJSONHandler(w, opts), nil
}

func (m *LogManager) DaemonLogger() (*slog.Logger, error) {
	daemonFileH, err := m.fileHandler("daemon")
	if err != nil {
		return nil, fmt.Errorf("logging: daemon file handler: %w", err)
	}
	multi := NewMultiHandler(daemonFileH, m.consoleHandler())
	return slog.New(multi).With("component", "daemon"), nil
}

func (m *LogManager) GroupLogger(group string) (*slog.Logger, error) {
	groupDir := filepath.Join("groups", group)
	groupFileH, err := m.fileHandler(groupDir)
	if err != nil {
		return nil, fmt.Errorf("logging: group file handler for %q: %w", group, err)
	}

	daemonFileH, err := m.fileHandler("daemon")
	if err != nil {
		return nil, fmt.Errorf("logging: daemon file handler (group %q): %w", group, err)
	}

	multi := NewMultiHandler(groupFileH, daemonFileH, m.consoleHandler())
	return slog.New(multi).With("component", "group", "group", group), nil
}

func (m *LogManager) RunnerLogger(group, runner string) (*slog.Logger, error) {
	runnerDir := filepath.Join("groups", group, "runners", runner)
	runnerFileH, err := m.fileHandler(runnerDir)
	if err != nil {
		return nil, fmt.Errorf("logging: runner file handler for %q/%q: %w", group, runner, err)
	}

	groupDir := filepath.Join("groups", group)
	groupFileH, err := m.fileHandler(groupDir)
	if err != nil {
		return nil, fmt.Errorf("logging: group file handler for runner %q/%q: %w", group, runner, err)
	}

	daemonFileH, err := m.fileHandler("daemon")
	if err != nil {
		return nil, fmt.Errorf("logging: daemon file handler (runner %q/%q): %w", group, runner, err)
	}

	multi := NewMultiHandler(runnerFileH, groupFileH, daemonFileH, m.consoleHandler())
	return slog.New(multi).With("component", "runner", "group", group, "runner", runner), nil
}

func (m *LogManager) RunnerOutputFile(group, runner string) (io.WriteCloser, error) {
	dir := filepath.Join(m.rootDir, "groups", group, "runners", runner)
	w, err := newDateAwareWriter(dir)
	if err != nil {
		return nil, fmt.Errorf("logging: runner output file for %q/%q: %w", group, runner, err)
	}
	m.trackWriter(w)
	return w, nil
}

func (m *LogManager) RemoveRunnerLogs(group, runner string) error {
	dir := filepath.Join(m.rootDir, "groups", group, "runners", runner)

	m.mu.Lock()
	kept := m.writers[:0]
	for _, w := range m.writers {
		if w.dir == dir {
			if closeErr := w.Close(); closeErr != nil {
				m.mu.Unlock()
				return fmt.Errorf("logging: close runner writer %q/%q: %w", group, runner, closeErr)
			}
			continue
		}
		kept = append(kept, w)
	}
	m.writers = kept
	m.mu.Unlock()

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("logging: remove runner log dir %s: %w", dir, err)
	}
	return nil
}

func (m *LogManager) StartCleanupScheduler(ctx context.Context) error {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := m.CleanupOldLogs(); err != nil {
				fmt.Fprintf(os.Stderr, "log cleanup error: %v\n", err)
			}
		}
	}
}

func (m *LogManager) CleanupOldLogs() error {
	if m.cfg.RetentionDays <= 0 {
		return nil
	}
	cutoff := nowFunc().AddDate(0, 0, -m.cfg.RetentionDays)

	return filepath.Walk(m.rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("logging: walk %s: %w", path, err)
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}
		if info.ModTime().Before(cutoff) {
			if removeErr := os.Remove(path); removeErr != nil {
				return fmt.Errorf("logging: remove old log %s: %w", path, removeErr)
			}
		}
		return nil
	})
}
