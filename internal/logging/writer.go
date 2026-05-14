package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var nowFunc = time.Now

type dateAwareWriter struct {
	mu      sync.Mutex
	dir     string
	current *os.File
	today   string
}

func newDateAwareWriter(dir string) (*dateAwareWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("logging: create dir %s: %w", dir, err)
	}
	w := &dateAwareWriter{dir: dir}
	if err := w.rotate(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *dateAwareWriter) rotate() error {
	today := nowFunc().Format("2006-01-02")
	if w.current != nil && w.today == today {
		return nil
	}
	if w.current != nil {
		w.current.Close()
	}
	path := filepath.Join(w.dir, today+".json")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("logging: open %s: %w", path, err)
	}
	w.current = f
	w.today = today
	return nil
}

func (w *dateAwareWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.rotate(); err != nil {
		return 0, err
	}
	return w.current.Write(p)
}

func (w *dateAwareWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.current != nil {
		return w.current.Close()
	}
	return nil
}
