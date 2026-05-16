package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/health"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

type mockController struct {
	snapshots map[string][]model.RunnerSnapshot
}

func (m *mockController) Snapshots() map[string][]model.RunnerSnapshot {
	return m.snapshots
}

type mockHealth struct {
	status health.HealthStatus
}

func (m *mockHealth) Status() health.HealthStatus {
	return m.status
}

func testServer(ctrl *mockController, h *mockHealth) *Server {
	return &Server{
		controller: ctrl,
		health:     h,
		logger:     slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1})),
	}
}

func TestHandleStatus(t *testing.T) {
	now := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)

	ctrl := &mockController{
		snapshots: map[string][]model.RunnerSnapshot{
			"group-a": {
				{Name: "group-a-1", Group: "group-a", State: "idle", PID: 1234, StartedAt: now},
				{Name: "group-a-2", Group: "group-a", State: "busy", PID: 5678, StartedAt: now},
			},
		},
	}
	h := &mockHealth{
		status: health.HealthStatus{
			LastCheck: now,
			Issues:    []model.HealthIssue{},
		},
	}

	s := testServer(ctrl, h)
	srv := httptest.NewServer(s.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatalf("GET /status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}

	var body statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	runners, ok := body.Groups["group-a"]
	if !ok {
		t.Fatal("expected group-a in response")
	}
	if len(runners) != 2 {
		t.Fatalf("expected 2 runners in group-a, got %d", len(runners))
	}
}

func TestHandleHealth(t *testing.T) {
	now := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)

	ctrl := &mockController{
		snapshots: map[string][]model.RunnerSnapshot{},
	}
	h := &mockHealth{
		status: health.HealthStatus{
			LastCheck: now,
			Issues: []model.HealthIssue{
				{
					Level:      model.LevelWarning,
					Type:       "health.disk_low",
					Message:    "disk space below threshold",
					DetectedAt: now,
				},
			},
		},
	}

	s := testServer(ctrl, h)
	srv := httptest.NewServer(s.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var body healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(body.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(body.Issues))
	}
	if body.Issues[0].Type != "health.disk_low" {
		t.Fatalf("expected issue type health.disk_low, got %q", body.Issues[0].Type)
	}
}

func TestRoutes_NotFound(t *testing.T) {
	ctrl := &mockController{
		snapshots: map[string][]model.RunnerSnapshot{},
	}
	h := &mockHealth{
		status: health.HealthStatus{},
	}

	s := testServer(ctrl, h)
	srv := httptest.NewServer(s.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/unknown")
	if err != nil {
		t.Fatalf("GET /unknown: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestHandleStatus_EmptyGroups(t *testing.T) {
	ctrl := &mockController{
		snapshots: map[string][]model.RunnerSnapshot{},
	}
	h := &mockHealth{
		status: health.HealthStatus{
			Issues: []model.HealthIssue{},
		},
	}

	s := testServer(ctrl, h)
	srv := httptest.NewServer(s.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatalf("GET /status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var body statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(body.Groups) != 0 {
		t.Fatalf("expected 0 groups, got %d", len(body.Groups))
	}
}

func TestServer_Run_SocketPermissions(t *testing.T) {
	// Unix domain sockets on macOS cap at ~104 chars, so avoid t.TempDir() which
	// returns long /var/folders/... paths. Use a short directory under /tmp.
	stateDir, err := os.MkdirTemp("/tmp", "ghr-sock-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(stateDir) })

	srv := NewServer(stateDir, &mockController{}, &mockHealth{},
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1})))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	socket := filepath.Join(stateDir, "ghr.sock")
	deadline := time.Now().Add(2 * time.Second)
	var info os.FileInfo
	for time.Now().Before(deadline) {
		if info, err = os.Stat(socket); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err != nil {
		cancel()
		<-done
		t.Fatalf("stat socket: %v", err)
	}
	mode := info.Mode().Perm()

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("server run: %v", err)
	}

	if mode != 0o600 {
		t.Fatalf("socket perm = %#o, want 0600", mode)
	}
}
