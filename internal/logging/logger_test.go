package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// TestParseLevel
// ---------------------------------------------------------------------------

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  slog.Level
	}{
		{"debug lowercase", "debug", slog.LevelDebug},
		{"info lowercase", "info", slog.LevelInfo},
		{"warn lowercase", "warn", slog.LevelWarn},
		{"error lowercase", "error", slog.LevelError},
		{"DEBUG uppercase", "DEBUG", slog.LevelDebug},
		{"Info mixed case", "Info", slog.LevelInfo},
		{"unknown defaults to info", "unknown", slog.LevelInfo},
		{"empty defaults to info", "", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseLevel(tt.input)
			if got != tt.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestNew
// ---------------------------------------------------------------------------

func TestNew(t *testing.T) {
	t.Run("valid config creates dirs", func(t *testing.T) {
		dir := t.TempDir()
		cfg := LogConfig{
			Level:  "info",
			Format: "json",
			Dir:    dir,
		}

		mgr, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer mgr.Close()

		daemonDir := filepath.Join(dir, "daemon")
		groupsDir := filepath.Join(dir, "groups")

		if info, statErr := os.Stat(daemonDir); statErr != nil || !info.IsDir() {
			t.Errorf("daemon directory not created at %s", daemonDir)
		}
		if info, statErr := os.Stat(groupsDir); statErr != nil || !info.IsDir() {
			t.Errorf("groups directory not created at %s", groupsDir)
		}
	})

	t.Run("empty Dir returns error", func(t *testing.T) {
		cfg := LogConfig{Dir: ""}
		_, err := New(cfg)
		if err == nil {
			t.Fatal("New() with empty Dir should return error")
		}
		if !strings.Contains(err.Error(), "dir must not be empty") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// TestMultiHandler
// ---------------------------------------------------------------------------

func TestMultiHandler(t *testing.T) {
	t.Run("fans out to all handlers", func(t *testing.T) {
		var buf1, buf2 bytes.Buffer
		h1 := slog.NewJSONHandler(&buf1, &slog.HandlerOptions{Level: slog.LevelDebug})
		h2 := slog.NewJSONHandler(&buf2, &slog.HandlerOptions{Level: slog.LevelDebug})

		multi := NewMultiHandler(h1, h2)
		logger := slog.New(multi)
		logger.Info("hello multi")

		for i, buf := range []*bytes.Buffer{&buf1, &buf2} {
			content := buf.String()
			if content == "" {
				t.Errorf("buffer %d is empty, expected log output", i)
				continue
			}
			var entry map[string]interface{}
			if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &entry); err != nil {
				t.Errorf("buffer %d: failed to parse JSON: %v", i, err)
				continue
			}
			if msg, ok := entry["msg"].(string); !ok || msg != "hello multi" {
				t.Errorf("buffer %d: msg = %v, want %q", i, entry["msg"], "hello multi")
			}
		}
	})

	t.Run("WithAttrs propagates to all handlers", func(t *testing.T) {
		var buf1, buf2 bytes.Buffer
		h1 := slog.NewJSONHandler(&buf1, &slog.HandlerOptions{Level: slog.LevelDebug})
		h2 := slog.NewJSONHandler(&buf2, &slog.HandlerOptions{Level: slog.LevelDebug})

		multi := NewMultiHandler(h1, h2)
		withAttrs := multi.WithAttrs([]slog.Attr{slog.String("key", "val")})
		logger := slog.New(withAttrs)
		logger.Info("with attrs")

		for i, buf := range []*bytes.Buffer{&buf1, &buf2} {
			content := buf.String()
			var entry map[string]interface{}
			if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &entry); err != nil {
				t.Errorf("buffer %d: failed to parse JSON: %v", i, err)
				continue
			}
			if v, ok := entry["key"].(string); !ok || v != "val" {
				t.Errorf("buffer %d: key = %v, want %q", i, entry["key"], "val")
			}
		}
	})

	t.Run("Enabled returns true if any handler is enabled", func(t *testing.T) {
		// h1 only enabled at Error, h2 enabled at Debug
		h1 := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})
		h2 := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
		multi := NewMultiHandler(h1, h2)

		// Debug should be enabled because h2 accepts it
		if !multi.Enabled(nil, slog.LevelDebug) {
			t.Error("Enabled(Debug) = false, want true (h2 accepts Debug)")
		}
		// Info should be enabled because h2 accepts it
		if !multi.Enabled(nil, slog.LevelInfo) {
			t.Error("Enabled(Info) = false, want true")
		}
	})

	t.Run("Enabled returns false when no handler is enabled", func(t *testing.T) {
		h1 := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})
		h2 := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})
		multi := NewMultiHandler(h1, h2)

		if multi.Enabled(nil, slog.LevelDebug) {
			t.Error("Enabled(Debug) = true, want false (both require Error)")
		}
	})
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// newTestManager creates a LogManager in a temporary directory with debug level.
func newTestManager(t *testing.T) *LogManager {
	t.Helper()
	dir := t.TempDir()
	cfg := LogConfig{
		Level:        "debug",
		Format:       "json",
		Dir:          dir,
		RunnerOutput: true,
	}
	mgr, err := New(cfg)
	if err != nil {
		t.Fatalf("newTestManager: %v", err)
	}
	t.Cleanup(func() { mgr.Close() })
	return mgr
}

// readJSONLines reads a file and returns each line as a parsed JSON map.
func readJSONLines(t *testing.T, path string) []map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readJSONLines: read %s: %v", path, err)
	}
	var result []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("readJSONLines: parse line %q: %v", line, err)
		}
		result = append(result, entry)
	}
	return result
}

// todayFile returns the log filename for the current (possibly mocked) date.
func todayFile() string {
	return nowFunc().Format("2006-01-02") + ".json"
}

// ---------------------------------------------------------------------------
// TestDaemonLogger
// ---------------------------------------------------------------------------

func TestDaemonLogger(t *testing.T) {
	mgr := newTestManager(t)

	logger, err := mgr.DaemonLogger()
	if err != nil {
		t.Fatalf("DaemonLogger() error = %v", err)
	}

	logger.Info("daemon test message")

	// Flush: close the manager so files are flushed.
	if err := mgr.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	logFile := filepath.Join(mgr.rootDir, "daemon", todayFile())
	entries := readJSONLines(t, logFile)
	if len(entries) == 0 {
		t.Fatal("expected at least one log entry in daemon log")
	}

	found := false
	for _, e := range entries {
		if msg, ok := e["msg"].(string); ok && msg == "daemon test message" {
			found = true
			if comp, ok := e["component"].(string); !ok || comp != "daemon" {
				t.Errorf("component = %v, want %q", e["component"], "daemon")
			}
		}
	}
	if !found {
		t.Error("did not find 'daemon test message' in daemon log file")
	}
}

// ---------------------------------------------------------------------------
// TestGroupLogger
// ---------------------------------------------------------------------------

func TestGroupLogger(t *testing.T) {
	mgr := newTestManager(t)

	logger, err := mgr.GroupLogger("test-group")
	if err != nil {
		t.Fatalf("GroupLogger() error = %v", err)
	}

	logger.Info("group test message")

	if err := mgr.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Check group log file.
	groupFile := filepath.Join(mgr.rootDir, "groups", "test-group", todayFile())
	groupEntries := readJSONLines(t, groupFile)
	found := false
	for _, e := range groupEntries {
		if msg, ok := e["msg"].(string); ok && msg == "group test message" {
			found = true
			if comp, ok := e["component"].(string); !ok || comp != "group" {
				t.Errorf("component = %v, want %q", e["component"], "group")
			}
			if g, ok := e["group"].(string); !ok || g != "test-group" {
				t.Errorf("group = %v, want %q", e["group"], "test-group")
			}
		}
	}
	if !found {
		t.Errorf("did not find 'group test message' in group log file %s", groupFile)
	}

	// Check propagation to daemon log.
	daemonFile := filepath.Join(mgr.rootDir, "daemon", todayFile())
	daemonEntries := readJSONLines(t, daemonFile)
	found = false
	for _, e := range daemonEntries {
		if msg, ok := e["msg"].(string); ok && msg == "group test message" {
			found = true
		}
	}
	if !found {
		t.Error("group message did not propagate to daemon log file")
	}
}

// ---------------------------------------------------------------------------
// TestRunnerLogger
// ---------------------------------------------------------------------------

func TestRunnerLogger(t *testing.T) {
	mgr := newTestManager(t)

	logger, err := mgr.RunnerLogger("test-group", "runner-abc")
	if err != nil {
		t.Fatalf("RunnerLogger() error = %v", err)
	}

	logger.Info("runner test message")

	if err := mgr.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	today := todayFile()

	// Verify message in runner log.
	runnerFile := filepath.Join(mgr.rootDir, "groups", "test-group", "runners", "runner-abc", today)
	runnerEntries := readJSONLines(t, runnerFile)
	found := false
	for _, e := range runnerEntries {
		if msg, ok := e["msg"].(string); ok && msg == "runner test message" {
			found = true
			if comp, ok := e["component"].(string); !ok || comp != "runner" {
				t.Errorf("runner log: component = %v, want %q", e["component"], "runner")
			}
			if g, ok := e["group"].(string); !ok || g != "test-group" {
				t.Errorf("runner log: group = %v, want %q", e["group"], "test-group")
			}
			if r, ok := e["runner"].(string); !ok || r != "runner-abc" {
				t.Errorf("runner log: runner = %v, want %q", e["runner"], "runner-abc")
			}
		}
	}
	if !found {
		t.Errorf("did not find 'runner test message' in runner log file %s", runnerFile)
	}

	// Verify propagation to group log.
	groupFile := filepath.Join(mgr.rootDir, "groups", "test-group", today)
	groupEntries := readJSONLines(t, groupFile)
	found = false
	for _, e := range groupEntries {
		if msg, ok := e["msg"].(string); ok && msg == "runner test message" {
			found = true
		}
	}
	if !found {
		t.Error("runner message did not propagate to group log file")
	}

	// Verify propagation to daemon log.
	daemonFile := filepath.Join(mgr.rootDir, "daemon", today)
	daemonEntries := readJSONLines(t, daemonFile)
	found = false
	for _, e := range daemonEntries {
		if msg, ok := e["msg"].(string); ok && msg == "runner test message" {
			found = true
		}
	}
	if !found {
		t.Error("runner message did not propagate to daemon log file")
	}
}

// ---------------------------------------------------------------------------
// TestDateRotation
// ---------------------------------------------------------------------------

func TestDateRotation(t *testing.T) {
	orig := nowFunc
	defer func() { nowFunc = orig }()

	day1 := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	day2 := time.Date(2024, 1, 16, 12, 0, 0, 0, time.UTC)

	nowFunc = func() time.Time { return day1 }

	dir := t.TempDir()
	w, err := newDateAwareWriter(dir)
	if err != nil {
		t.Fatalf("newDateAwareWriter() error = %v", err)
	}
	defer w.Close()

	// Write on day 1.
	_, err = w.Write([]byte("day1 line\n"))
	if err != nil {
		t.Fatalf("Write day1: %v", err)
	}

	file1 := filepath.Join(dir, "2024-01-15.json")
	if _, statErr := os.Stat(file1); statErr != nil {
		t.Errorf("expected file %s to exist after day1 write", file1)
	}

	// Advance to day 2.
	nowFunc = func() time.Time { return day2 }

	_, err = w.Write([]byte("day2 line\n"))
	if err != nil {
		t.Fatalf("Write day2: %v", err)
	}

	file2 := filepath.Join(dir, "2024-01-16.json")
	if _, statErr := os.Stat(file2); statErr != nil {
		t.Errorf("expected file %s to exist after day2 write", file2)
	}

	// Verify contents.
	data1, err := os.ReadFile(file1)
	if err != nil {
		t.Fatalf("ReadFile day1: %v", err)
	}
	if !strings.Contains(string(data1), "day1 line") {
		t.Errorf("day1 file content = %q, want to contain %q", data1, "day1 line")
	}

	data2, err := os.ReadFile(file2)
	if err != nil {
		t.Fatalf("ReadFile day2: %v", err)
	}
	if !strings.Contains(string(data2), "day2 line") {
		t.Errorf("day2 file content = %q, want to contain %q", data2, "day2 line")
	}
}

// ---------------------------------------------------------------------------
// TestRunnerOutputFile
// ---------------------------------------------------------------------------

func TestRunnerOutputFile(t *testing.T) {
	mgr := newTestManager(t)

	wc, err := mgr.RunnerOutputFile("group", "runner")
	if err != nil {
		t.Fatalf("RunnerOutputFile() error = %v", err)
	}

	payload := []byte("some runner output\n")
	n, err := wc.Write(payload)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len(payload) {
		t.Errorf("Write() wrote %d bytes, want %d", n, len(payload))
	}

	outFile := filepath.Join(mgr.rootDir, "groups", "group", "runners", "runner", todayFile())
	if _, statErr := os.Stat(outFile); statErr != nil {
		t.Errorf("expected output file at %s", outFile)
	}

	if err := wc.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "some runner output") {
		t.Errorf("output file content = %q, want to contain %q", data, "some runner output")
	}
}

// ---------------------------------------------------------------------------
// TestCleanupOldLogs
// ---------------------------------------------------------------------------

func TestCleanupOldLogs(t *testing.T) {
	dir := t.TempDir()
	cfg := LogConfig{
		Level:         "info",
		Format:        "json",
		Dir:           dir,
		RetentionDays: 1,
	}
	mgr, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer mgr.Close()

	daemonDir := filepath.Join(dir, "daemon")

	// Create an old log file (modification time 3 days ago).
	oldFile := filepath.Join(daemonDir, "2024-01-10.json")
	if err := os.WriteFile(oldFile, []byte(`{"msg":"old"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile old: %v", err)
	}
	oldTime := time.Now().AddDate(0, 0, -3)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes old: %v", err)
	}

	// Create a fresh log file (modification time is now).
	freshFile := filepath.Join(daemonDir, "2024-01-15.json")
	if err := os.WriteFile(freshFile, []byte(`{"msg":"fresh"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile fresh: %v", err)
	}

	if err := mgr.CleanupOldLogs(); err != nil {
		t.Fatalf("CleanupOldLogs() error = %v", err)
	}

	// Old file should be deleted.
	if _, statErr := os.Stat(oldFile); !os.IsNotExist(statErr) {
		t.Errorf("old file %s should have been deleted", oldFile)
	}

	// Fresh file should remain.
	if _, statErr := os.Stat(freshFile); statErr != nil {
		t.Errorf("fresh file %s should still exist: %v", freshFile, statErr)
	}
}

// ---------------------------------------------------------------------------
// TestCleanupOldLogs_Disabled
// ---------------------------------------------------------------------------

func TestCleanupOldLogs_Disabled(t *testing.T) {
	dir := t.TempDir()
	cfg := LogConfig{
		Level:         "info",
		Format:        "json",
		Dir:           dir,
		RetentionDays: 0, // disabled
	}
	mgr, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer mgr.Close()

	daemonDir := filepath.Join(dir, "daemon")

	// Create an old file.
	oldFile := filepath.Join(daemonDir, "2020-01-01.json")
	if err := os.WriteFile(oldFile, []byte(`{"msg":"ancient"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	oldTime := time.Now().AddDate(-4, 0, 0)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	if err := mgr.CleanupOldLogs(); err != nil {
		t.Fatalf("CleanupOldLogs() error = %v", err)
	}

	// File should NOT be deleted when RetentionDays=0.
	if _, statErr := os.Stat(oldFile); statErr != nil {
		t.Errorf("old file %s should NOT have been deleted (RetentionDays=0): %v", oldFile, statErr)
	}
}

// ---------------------------------------------------------------------------
// TestClose
// ---------------------------------------------------------------------------

func TestClose(t *testing.T) {
	mgr := newTestManager(t)

	// Create several loggers to open multiple writers.
	if _, err := mgr.DaemonLogger(); err != nil {
		t.Fatalf("DaemonLogger: %v", err)
	}
	if _, err := mgr.GroupLogger("group-a"); err != nil {
		t.Fatalf("GroupLogger: %v", err)
	}
	if _, err := mgr.RunnerLogger("group-a", "runner-1"); err != nil {
		t.Fatalf("RunnerLogger: %v", err)
	}

	mgr.mu.Lock()
	writerCount := len(mgr.writers)
	mgr.mu.Unlock()
	if writerCount == 0 {
		t.Error("expected writers to be tracked before Close()")
	}

	if err := mgr.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	mgr.mu.Lock()
	writersAfter := mgr.writers
	mgr.mu.Unlock()
	if writersAfter != nil {
		t.Errorf("expected writers to be nil after Close(), got len=%d", len(writersAfter))
	}
}
