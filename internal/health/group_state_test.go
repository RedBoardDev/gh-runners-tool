package health

import (
	"testing"
)

func TestUpdateGroupStats(t *testing.T) {
	m := newTestMonitor(nil, nil, nil)

	m.UpdateGroupStats("group-a", 3)

	m.mu.RLock()
	gs, ok := m.groups["group-a"]
	m.mu.RUnlock()

	if !ok {
		t.Fatal("expected group-a to exist in groups map")
	}
	if gs.lastDesiredCount != 3 {
		t.Errorf("expected lastDesiredCount=3, got %d", gs.lastDesiredCount)
	}
}

func TestRecordStartFailure(t *testing.T) {
	m := newTestMonitor(nil, nil, nil)

	m.RecordStartFailure("group-a")
	m.RecordStartFailure("group-a")
	m.RecordStartFailure("group-a")

	m.mu.RLock()
	gs := m.groups["group-a"]
	m.mu.RUnlock()

	if gs.consecutiveFailures != 3 {
		t.Errorf("expected 3 consecutive failures, got %d", gs.consecutiveFailures)
	}
}

func TestRecordStartSuccess_ResetsFailures(t *testing.T) {
	m := newTestMonitor(nil, nil, nil)

	m.RecordStartFailure("group-a")
	m.RecordStartFailure("group-a")
	m.RecordStartSuccess("group-a")

	m.mu.RLock()
	gs := m.groups["group-a"]
	m.mu.RUnlock()

	if gs.consecutiveFailures != 0 {
		t.Errorf("expected 0 consecutive failures after success, got %d", gs.consecutiveFailures)
	}
}

func TestGetOrCreateGroup_CreatesIfMissing(t *testing.T) {
	m := newTestMonitor(nil, nil, nil)

	m.mu.Lock()
	gs := m.getOrCreateGroup("new-group")
	m.mu.Unlock()

	if gs == nil {
		t.Fatal("expected non-nil groupState")
	}
	if gs.consecutiveFailures != 0 {
		t.Errorf("expected 0 consecutive failures for new group, got %d", gs.consecutiveFailures)
	}
}

func TestGetOrCreateGroup_ReturnsExisting(t *testing.T) {
	m := newTestMonitor(nil, nil, nil)

	m.RecordStartFailure("group-a")

	m.mu.Lock()
	gs := m.getOrCreateGroup("group-a")
	m.mu.Unlock()

	if gs.consecutiveFailures != 1 {
		t.Errorf("expected 1 consecutive failure for existing group, got %d", gs.consecutiveFailures)
	}
}

func newTestMonitor(runners RunnerStateProvider, killer RunnerKiller, reporters []Reporter) *Monitor {
	return NewMonitor(
		MonitorConfig{Enabled: true},
		&noopNotifier{},
		runners,
		reporters,
		killer,
		noopLogger(),
	)
}
