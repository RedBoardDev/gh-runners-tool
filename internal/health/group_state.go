package health

import "time"

type groupState struct {
	consecutiveFailures int
	degradedSince       *time.Time
	lastDesiredCount    int
}

func (m *Monitor) getOrCreateGroup(name string) *groupState {
	gs, ok := m.groups[name]
	if !ok {
		gs = &groupState{}
		m.groups[name] = gs
	}
	return gs
}

func (m *Monitor) UpdateGroupStats(group string, desired int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	gs := m.getOrCreateGroup(group)
	gs.lastDesiredCount = desired
}

func (m *Monitor) RecordStartFailure(group string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	gs := m.getOrCreateGroup(group)
	gs.consecutiveFailures++
}

func (m *Monitor) RecordStartSuccess(group string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	gs := m.getOrCreateGroup(group)
	gs.consecutiveFailures = 0
}
