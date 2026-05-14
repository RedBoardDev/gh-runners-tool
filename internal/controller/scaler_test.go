package controller

import (
	"context"
	"testing"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/runner"
	"github.com/actions/scaleset"
)

type mockNotifier struct {
	events []model.Event
}

func (m *mockNotifier) Notify(_ context.Context, event *model.Event) {
	m.events = append(m.events, *event)
}

func newTestScaler(opts ...func(*MacOSScaler)) *MacOSScaler {
	s := &MacOSScaler{
		groupName:  "test-group",
		maxRunners: 5,
		minRunners: 0,
		logger:     testLogger(),
		notifier:   &mockNotifier{},
		idle:       make(map[string]*runner.Process),
		busy:       make(map[string]*runner.Process),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func TestSnapshots(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		idle     map[string]*runner.Process
		busy     map[string]*runner.Process
		wantLen  int
		wantIdle int
		wantBusy int
	}{
		{
			name:     "empty maps",
			idle:     map[string]*runner.Process{},
			busy:     map[string]*runner.Process{},
			wantLen:  0,
			wantIdle: 0,
			wantBusy: 0,
		},
		{
			name: "one idle one busy",
			idle: map[string]*runner.Process{
				"r-idle": {Name: "r-idle", Group: "test-group", PID: 100, StartedAt: now},
			},
			busy: map[string]*runner.Process{
				"r-busy": {Name: "r-busy", Group: "test-group", PID: 200, StartedAt: now},
			},
			wantLen:  2,
			wantIdle: 1,
			wantBusy: 1,
		},
		{
			name: "all idle",
			idle: map[string]*runner.Process{
				"r-1": {Name: "r-1", Group: "test-group", PID: 100, StartedAt: now},
				"r-2": {Name: "r-2", Group: "test-group", PID: 101, StartedAt: now},
			},
			busy:     map[string]*runner.Process{},
			wantLen:  2,
			wantIdle: 2,
			wantBusy: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestScaler(func(scaler *MacOSScaler) {
				scaler.idle = tt.idle
				scaler.busy = tt.busy
			})

			snapshots := s.Snapshots()
			if len(snapshots) != tt.wantLen {
				t.Fatalf("expected %d snapshots, got %d", tt.wantLen, len(snapshots))
			}

			idleCount := 0
			busyCount := 0
			for _, snap := range snapshots {
				switch snap.State {
				case "idle":
					idleCount++
				case "busy":
					busyCount++
				default:
					t.Fatalf("unexpected state %q", snap.State)
				}
				if snap.Group != "test-group" {
					t.Fatalf("expected group test-group, got %q", snap.Group)
				}
			}
			if idleCount != tt.wantIdle {
				t.Fatalf("expected %d idle, got %d", tt.wantIdle, idleCount)
			}
			if busyCount != tt.wantBusy {
				t.Fatalf("expected %d busy, got %d", tt.wantBusy, busyCount)
			}
		})
	}
}

func TestHandleDesiredRunnerCount_Noop(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	s := newTestScaler(func(scaler *MacOSScaler) {
		scaler.minRunners = 0
		scaler.maxRunners = 5
		scaler.idle = map[string]*runner.Process{
			"r-1": {Name: "r-1", Group: "test-group", PID: 100, StartedAt: now},
			"r-2": {Name: "r-2", Group: "test-group", PID: 101, StartedAt: now},
		}
	})

	got, err := s.HandleDesiredRunnerCount(context.Background(), 2)
	if err != nil {
		t.Fatalf("HandleDesiredRunnerCount: %v", err)
	}

	if got != 2 {
		t.Fatalf("expected current count 2, got %d", got)
	}
}

func TestHandleDesiredRunnerCount_CappedByMax(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	s := newTestScaler(func(scaler *MacOSScaler) {
		scaler.minRunners = 0
		scaler.maxRunners = 3
		scaler.idle = map[string]*runner.Process{
			"r-1": {Name: "r-1", Group: "test-group", PID: 100, StartedAt: now},
			"r-2": {Name: "r-2", Group: "test-group", PID: 101, StartedAt: now},
			"r-3": {Name: "r-3", Group: "test-group", PID: 102, StartedAt: now},
		}
	})

	got, err := s.HandleDesiredRunnerCount(context.Background(), 10)
	if err != nil {
		t.Fatalf("HandleDesiredRunnerCount: %v", err)
	}

	if got != 3 {
		t.Fatalf("expected count 3 (capped by max), got %d", got)
	}
}

func TestHandleJobStarted_NotFound(t *testing.T) {
	s := newTestScaler()

	err := s.HandleJobStarted(context.Background(), &scaleset.JobStarted{
		RunnerName: "unknown-runner",
	})
	if err != nil {
		t.Fatalf("expected nil error for unknown runner, got %v", err)
	}
}

func TestHandleJobStarted_MovesToBusy(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	proc := &runner.Process{Name: "r-1", Group: "test-group", PID: 100, StartedAt: now}

	s := newTestScaler(func(scaler *MacOSScaler) {
		scaler.idle = map[string]*runner.Process{"r-1": proc}
	})

	err := s.HandleJobStarted(context.Background(), &scaleset.JobStarted{
		RunnerName: "r-1",
	})
	if err != nil {
		t.Fatalf("HandleJobStarted: %v", err)
	}

	if _, ok := s.idle["r-1"]; ok {
		t.Fatal("expected runner to be removed from idle")
	}
	if _, ok := s.busy["r-1"]; !ok {
		t.Fatal("expected runner to be in busy")
	}
}

func TestHandleJobCompleted_NotFound(t *testing.T) {
	s := newTestScaler()

	err := s.HandleJobCompleted(context.Background(), &scaleset.JobCompleted{
		RunnerName: "unknown-runner",
		Result:     "succeeded",
	})
	if err != nil {
		t.Fatalf("expected nil error for unknown runner, got %v", err)
	}
}

func TestHandleJobCompleted_NotifiesEvent(t *testing.T) {
	n := &mockNotifier{}
	s := newTestScaler(func(scaler *MacOSScaler) {
		scaler.notifier = n
	})

	err := s.HandleJobCompleted(context.Background(), &scaleset.JobCompleted{
		RunnerName: "unknown-runner",
		Result:     "failed",
	})
	if err != nil {
		t.Fatalf("HandleJobCompleted: %v", err)
	}

	if len(n.events) != 1 {
		t.Fatalf("expected 1 notification event, got %d", len(n.events))
	}
	if n.events[0].Type != model.EventRunnerFailed {
		t.Fatalf("expected event type %q, got %q", model.EventRunnerFailed, n.events[0].Type)
	}
}
