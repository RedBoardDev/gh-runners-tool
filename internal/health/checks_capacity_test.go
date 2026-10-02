package health

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

type fakeCapacityWaiter struct {
	waiting map[string]int
}

func (f fakeCapacityWaiter) WaitingRunners() map[string]int {
	return f.waiting
}

func issuesOfType(issues []model.HealthIssue, eventType string) []model.HealthIssue {
	var out []model.HealthIssue
	for _, issue := range issues {
		if issue.Type == eventType {
			out = append(out, issue)
		}
	}
	return out
}

func TestCheckGroupDivergence_DeferredDemand(t *testing.T) {
	tests := []struct {
		name          string
		desired       int
		actual        int
		deferred      int
		degradedSince *time.Time
		wantDegraded  int
		wantWaiting   int
		wantMessage   string
		wantReset     bool
	}{
		{
			name:    "runners match what could be started",
			desired: 5, actual: 3, deferred: 2,
			degradedSince: timePtr(time.Now().Add(-time.Hour)),
			wantWaiting:   1,
			wantReset:     true,
		},
		{
			name:    "all demand deferred",
			desired: 2, actual: 0, deferred: 2,
			degradedSince: timePtr(time.Now().Add(-time.Hour)),
			wantWaiting:   1,
			wantReset:     true,
		},
		{
			name:    "deferral reported even when desired is not tracked",
			desired: 0, actual: 0, deferred: 3,
			wantWaiting: 1,
		},
		{
			name:    "real shortfall is still degraded and counts only non-deferred demand",
			desired: 5, actual: 1, deferred: 2,
			degradedSince: timePtr(time.Now().Add(-time.Hour)),
			wantDegraded:  1,
			wantWaiting:   1,
			wantMessage:   "1 runners but 3 desired",
		},
		{
			name:    "without deferral the legacy check is unchanged",
			desired: 5, actual: 3, deferred: 0,
			degradedSince: timePtr(time.Now().Add(-time.Hour)),
			wantDegraded:  1,
			wantMessage:   "3 runners but 5 desired",
		},
		{
			name:    "deferral does not start a degraded clock",
			desired: 5, actual: 3, deferred: 2,
			wantWaiting: 1,
			wantReset:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestMonitor(nil, nil, nil)
			m.cfg.DivergenceTimeout = 5 * time.Minute
			m.issues = m.issues[:0]
			gs := &groupState{lastDesiredCount: tt.desired, degradedSince: tt.degradedSince}

			m.checkGroupDivergence("group-a", tt.actual, tt.deferred, gs)

			degraded := issuesOfType(m.issues, model.EventHealthGroupDegraded)
			if len(degraded) != tt.wantDegraded {
				t.Fatalf("degraded issues = %d, want %d (%+v)", len(degraded), tt.wantDegraded, m.issues)
			}
			if tt.wantMessage != "" && !strings.Contains(degraded[0].Message, tt.wantMessage) {
				t.Errorf("message %q lacks %q", degraded[0].Message, tt.wantMessage)
			}

			waiting := issuesOfType(m.issues, model.EventHealthGroupWaitingCapacity)
			if len(waiting) != tt.wantWaiting {
				t.Fatalf("waiting-for-capacity issues = %d, want %d", len(waiting), tt.wantWaiting)
			}
			if tt.wantWaiting > 0 {
				if waiting[0].Level != model.LevelInfo {
					t.Errorf("level = %s, want info", waiting[0].Level)
				}
				if waiting[0].Group != "group-a" {
					t.Errorf("group = %q, want group-a", waiting[0].Group)
				}
			}
			if tt.wantReset && gs.degradedSince != nil {
				t.Error("degradedSince should have been reset")
			}
		})
	}
}

func TestRunChecks_DeferredDemandIsStatusOnly(t *testing.T) {
	notif := &noopNotifier{}
	state := &fakeRunnerState{
		snapshots: map[string][]model.RunnerSnapshot{
			"group-a": {{Name: "r1", State: "idle", PID: 0, StartedAt: time.Now()}},
		},
	}
	m := NewMonitor(
		MonitorConfig{Enabled: true, CheckInterval: time.Second, DivergenceTimeout: time.Minute},
		notif, state, nil, nil, noopLogger(),
		WithCapacity(fakeCapacityWaiter{waiting: map[string]int{"group-a": 2}}),
	)
	m.UpdateGroupStats("group-a", 3)
	m.mu.Lock()
	m.groups["group-a"].degradedSince = timePtr(time.Now().Add(-time.Hour))
	m.mu.Unlock()

	m.runChecks(context.Background())

	issues := m.Status().Issues
	if got := issuesOfType(issues, model.EventHealthGroupDegraded); len(got) != 0 {
		t.Fatalf("deferred demand reported as degraded: %+v", got)
	}
	waiting := issuesOfType(issues, model.EventHealthGroupWaitingCapacity)
	if len(waiting) != 1 || !strings.Contains(waiting[0].Message, "waiting for capacity") {
		t.Fatalf("status should expose the waiting group, got %+v", issues)
	}
}

func TestRunChecks_WithoutCapacityReportsNothingExtra(t *testing.T) {
	state := &fakeRunnerState{
		snapshots: map[string][]model.RunnerSnapshot{
			"group-a": {{Name: "r1", State: "idle", StartedAt: time.Now()}},
		},
	}
	m := NewMonitor(MonitorConfig{Enabled: true, CheckInterval: time.Second}, &noopNotifier{}, state, nil, nil, noopLogger())

	m.runChecks(context.Background())

	if got := m.Status().Issues; len(got) != 0 {
		t.Fatalf("issues = %+v, want none", got)
	}
}

func TestDispatchHealthReports_InfoIssuesAreNotNotified(t *testing.T) {
	notif := &noopNotifier{}
	issues := []model.HealthIssue{
		{Level: model.LevelInfo, Type: model.EventHealthGroupWaitingCapacity, Group: "a", DetectedAt: time.Now()},
		{Level: model.LevelWarning, Type: model.EventHealthGroupDegraded, Group: "a", DetectedAt: time.Now()},
	}

	dispatchHealthReports(context.Background(), nil, notif, dispatchPayload{issues: issues})

	events := notif.snapshot()
	if len(events) != 1 || events[0].Type != model.EventHealthGroupDegraded {
		t.Fatalf("notified events = %+v, want only the degraded warning", events)
	}
}
