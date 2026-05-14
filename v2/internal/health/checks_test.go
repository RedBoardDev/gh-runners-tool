package health

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

type noopNotifier struct {
	events []model.Event
}

func (n *noopNotifier) Notify(_ context.Context, event model.Event) {
	n.events = append(n.events, event)
}

type fakeRunnerState struct {
	snapshots map[string][]model.RunnerSnapshot
}

func (f *fakeRunnerState) Snapshots() map[string][]model.RunnerSnapshot {
	return f.snapshots
}

type fakeKiller struct {
	killed []string
	err    error
}

func (f *fakeKiller) KillRunner(_ context.Context, group string, runner string) error {
	f.killed = append(f.killed, fmt.Sprintf("%s/%s", group, runner))
	return f.err
}

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

func TestCheckIdleTimeouts(t *testing.T) {
	tests := []struct {
		name        string
		idleTimeout time.Duration
		snapshots   []model.RunnerSnapshot
		wantIssues  int
	}{
		{
			name:        "disabled when timeout is zero",
			idleTimeout: 0,
			snapshots: []model.RunnerSnapshot{
				{Name: "r1", State: "idle", StartedAt: time.Now().Add(-1 * time.Hour)},
			},
			wantIssues: 0,
		},
		{
			name:        "no issue when under timeout",
			idleTimeout: 30 * time.Minute,
			snapshots: []model.RunnerSnapshot{
				{Name: "r1", State: "idle", StartedAt: time.Now().Add(-10 * time.Minute)},
			},
			wantIssues: 0,
		},
		{
			name:        "issue when over timeout",
			idleTimeout: 30 * time.Minute,
			snapshots: []model.RunnerSnapshot{
				{Name: "r1", State: "idle", StartedAt: time.Now().Add(-1 * time.Hour)},
			},
			wantIssues: 1,
		},
		{
			name:        "busy runners are skipped",
			idleTimeout: 30 * time.Minute,
			snapshots: []model.RunnerSnapshot{
				{Name: "r1", State: "busy", StartedAt: time.Now().Add(-1 * time.Hour)},
			},
			wantIssues: 0,
		},
		{
			name:        "zero StartedAt is skipped",
			idleTimeout: 30 * time.Minute,
			snapshots: []model.RunnerSnapshot{
				{Name: "r1", State: "idle"},
			},
			wantIssues: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestMonitor(nil, nil, nil)
			m.cfg.IdleTimeout = tt.idleTimeout
			m.issues = m.issues[:0]

			m.checkIdleTimeouts("test-group", tt.snapshots)

			if len(m.issues) != tt.wantIssues {
				t.Errorf("expected %d issues, got %d", tt.wantIssues, len(m.issues))
			}
			if tt.wantIssues > 0 && m.issues[0].Type != model.EventHealthIdleTimeout {
				t.Errorf("expected type %s, got %s", model.EventHealthIdleTimeout, m.issues[0].Type)
			}
		})
	}
}

func TestCheckGroupDivergence(t *testing.T) {
	tests := []struct {
		name              string
		divergenceTimeout time.Duration
		actualCount       int
		desiredCount      int
		degradedSince     *time.Time
		wantIssues        int
		wantDegraded      bool
	}{
		{
			name:              "disabled when timeout is zero",
			divergenceTimeout: 0,
			actualCount:       1,
			desiredCount:      3,
			wantIssues:        0,
		},
		{
			name:              "no issue when counts match",
			divergenceTimeout: 5 * time.Minute,
			actualCount:       3,
			desiredCount:      3,
			wantIssues:        0,
		},
		{
			name:              "no issue when desired is zero",
			divergenceTimeout: 5 * time.Minute,
			actualCount:       1,
			desiredCount:      0,
			wantIssues:        0,
		},
		{
			name:              "first divergence sets degradedSince",
			divergenceTimeout: 5 * time.Minute,
			actualCount:       1,
			desiredCount:      3,
			wantIssues:        0,
			wantDegraded:      true,
		},
		{
			name:              "issue after timeout exceeded",
			divergenceTimeout: 5 * time.Minute,
			actualCount:       1,
			desiredCount:      3,
			degradedSince:     timePtr(time.Now().Add(-10 * time.Minute)),
			wantIssues:        1,
		},
		{
			name:              "no issue before timeout",
			divergenceTimeout: 5 * time.Minute,
			actualCount:       1,
			desiredCount:      3,
			degradedSince:     timePtr(time.Now().Add(-2 * time.Minute)),
			wantIssues:        0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestMonitor(nil, nil, nil)
			m.cfg.DivergenceTimeout = tt.divergenceTimeout
			m.issues = m.issues[:0]

			gs := &groupState{
				lastDesiredCount: tt.desiredCount,
				degradedSince:    tt.degradedSince,
			}

			m.checkGroupDivergence("test-group", tt.actualCount, gs)

			if len(m.issues) != tt.wantIssues {
				t.Errorf("expected %d issues, got %d", tt.wantIssues, len(m.issues))
			}
			if tt.wantDegraded && gs.degradedSince == nil {
				t.Error("expected degradedSince to be set")
			}
			if tt.wantIssues > 0 && m.issues[0].Type != model.EventHealthGroupDegraded {
				t.Errorf("expected type %s, got %s", model.EventHealthGroupDegraded, m.issues[0].Type)
			}
		})
	}
}

func TestCheckConsecutiveFailures(t *testing.T) {
	tests := []struct {
		name        string
		maxFailures int
		failures    int
		wantIssues  int
	}{
		{
			name:        "disabled when max is zero",
			maxFailures: 0,
			failures:    10,
			wantIssues:  0,
		},
		{
			name:        "no issue at threshold",
			maxFailures: 5,
			failures:    5,
			wantIssues:  0,
		},
		{
			name:        "issue above threshold",
			maxFailures: 5,
			failures:    6,
			wantIssues:  1,
		},
		{
			name:        "no issue below threshold",
			maxFailures: 5,
			failures:    3,
			wantIssues:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestMonitor(nil, nil, nil)
			m.cfg.MaxConsecutiveFailures = tt.maxFailures
			m.issues = m.issues[:0]

			gs := &groupState{consecutiveFailures: tt.failures}
			m.checkConsecutiveFailures("test-group", gs)

			if len(m.issues) != tt.wantIssues {
				t.Errorf("expected %d issues, got %d", tt.wantIssues, len(m.issues))
			}
			if tt.wantIssues > 0 {
				if m.issues[0].Type != model.EventHealthGroupFailing {
					t.Errorf("expected type %s, got %s", model.EventHealthGroupFailing, m.issues[0].Type)
				}
				if m.issues[0].Level != model.LevelCritical {
					t.Errorf("expected level %s, got %s", model.LevelCritical, m.issues[0].Level)
				}
			}
		})
	}
}

func TestCheckRunnerTimeouts_KillsRunner(t *testing.T) {
	killer := &fakeKiller{}
	m := NewMonitor(
		MonitorConfig{
			Enabled:       true,
			RunnerTimeout: 1 * time.Hour,
		},
		&noopNotifier{},
		nil,
		nil,
		killer,
		noopLogger(),
	)

	snaps := []model.RunnerSnapshot{
		{Name: "r1", State: "busy", PID: 1, StartedAt: time.Now().Add(-2 * time.Hour)},
	}

	m.checkRunnerTimeouts(context.Background(), "group-a", snaps)

	if len(killer.killed) != 1 {
		t.Fatalf("expected 1 kill call, got %d", len(killer.killed))
	}
	if killer.killed[0] != "group-a/r1" {
		t.Errorf("expected kill group-a/r1, got %s", killer.killed[0])
	}
}

func TestRunChecks_IntegrationWithNotifier(t *testing.T) {
	notif := &noopNotifier{}
	state := &fakeRunnerState{
		snapshots: map[string][]model.RunnerSnapshot{
			"group-a": {
				{Name: "r1", State: "idle", PID: 99999999, StartedAt: time.Now().Add(-2 * time.Hour)},
			},
		},
	}

	m := NewMonitor(
		MonitorConfig{
			Enabled:       true,
			CheckInterval: time.Second,
			IdleTimeout:   30 * time.Minute,
		},
		notif,
		state,
		nil,
		nil,
		noopLogger(),
	)

	m.runChecks(context.Background())

	foundIdle := false
	for _, e := range notif.events {
		if e.Type == model.EventHealthIdleTimeout {
			foundIdle = true
		}
	}
	if !foundIdle {
		t.Error("expected idle timeout event to be notified")
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}
