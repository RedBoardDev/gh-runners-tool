package health

import (
	"context"
	"fmt"
	"syscall"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/logging"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

func (m *Monitor) runChecks(ctx context.Context) {
	start := time.Now()

	m.mu.Lock()
	m.issues = m.issues[:0]

	snapshots := m.runners.Snapshots()
	totalActual := 0
	totalDesired := 0

	groupActuals := make(map[string]int, len(snapshots))
	groupDesireds := make(map[string]int, len(snapshots))

	for group, snaps := range snapshots {
		m.checkRunnerLiveness(ctx, group, snaps)
		m.checkRunnerTimeouts(ctx, group, snaps)
		m.checkIdleTimeouts(ctx, group, snaps)
		gs := m.getOrCreateGroup(group)
		m.checkGroupDivergence(group, len(snaps), gs)
		m.checkConsecutiveFailures(group, gs)
		totalActual += len(snaps)
		totalDesired += gs.lastDesiredCount
		groupActuals[group] = len(snaps)
		groupDesireds[group] = gs.lastDesiredCount
	}

	m.checkDiskSpace()
	m.lastCheck = time.Now()
	checkDuration := time.Since(start)

	issuesCopy := make([]model.HealthIssue, len(m.issues))
	copy(issuesCopy, m.issues)
	reporters := m.reporters
	notifier := m.notifier
	groupsCount := len(snapshots)
	m.mu.Unlock()

	go dispatchHealthReports(ctx, reporters, notifier, dispatchPayload{
		groupsCount:   groupsCount,
		totalActual:   totalActual,
		totalDesired:  totalDesired,
		checkDuration: checkDuration,
		groupActuals:  groupActuals,
		groupDesireds: groupDesireds,
		issues:        issuesCopy,
	})
}

type dispatchPayload struct {
	groupsCount   int
	totalActual   int
	totalDesired  int
	checkDuration time.Duration
	groupActuals  map[string]int
	groupDesireds map[string]int
	issues        []model.HealthIssue
}

func dispatchHealthReports(ctx context.Context, reporters []Reporter, notifier Notifier, p dispatchPayload) {
	for _, r := range reporters {
		r.ReportDaemonHealth(ctx, p.groupsCount, p.totalActual, p.totalDesired, p.checkDuration)
	}
	for group, actual := range p.groupActuals {
		desired := p.groupDesireds[group]
		for _, r := range reporters {
			r.ReportGroupHealth(ctx, group, actual, desired)
		}
	}
	for _, issue := range p.issues {
		notifier.Notify(ctx, &model.Event{
			Type:      issue.Type,
			Level:     issue.Level,
			Group:     issue.Group,
			Runner:    issue.Runner,
			Message:   issue.Message,
			Timestamp: issue.DetectedAt,
		})
	}
}

func (m *Monitor) checkRunnerLiveness(ctx context.Context, group string, snapshots []model.RunnerSnapshot) {
	for _, snap := range snapshots {
		if snap.PID <= 0 {
			continue
		}
		if err := syscall.Kill(int(snap.PID), 0); err != nil {
			m.issues = append(m.issues, model.HealthIssue{
				Level:      model.LevelError,
				Type:       model.EventHealthZombieRunner,
				Group:      group,
				Runner:     snap.Name,
				Message:    fmt.Sprintf("runner %s (pid %d) is no longer alive", snap.Name, snap.PID),
				DetectedAt: time.Now(),
			})
			if m.killer != nil {
				if killErr := m.killer.KillRunner(ctx, group, snap.Name); killErr != nil {
					m.logger.ErrorContext(ctx, "failed to kill zombie runner", logging.KeyGroup, group, logging.KeyRunner, snap.Name, logging.KeyError, killErr)
				}
			}
		}
	}
}

func (m *Monitor) checkRunnerTimeouts(ctx context.Context, group string, snapshots []model.RunnerSnapshot) {
	if m.cfg.RunnerTimeout <= 0 {
		return
	}

	now := time.Now()
	for _, snap := range snapshots {
		if snap.State != "busy" {
			continue
		}
		if snap.StartedAt.IsZero() {
			continue
		}
		if now.Sub(snap.StartedAt) <= m.cfg.RunnerTimeout {
			continue
		}
		m.issues = append(m.issues, model.HealthIssue{
			Level:      model.LevelWarning,
			Type:       model.EventHealthRunnerTimeout,
			Group:      group,
			Runner:     snap.Name,
			Message:    fmt.Sprintf("runner %s has been busy for %s (timeout: %s)", snap.Name, now.Sub(snap.StartedAt).Round(time.Second), m.cfg.RunnerTimeout),
			DetectedAt: now,
		})
		if m.killer != nil {
			if killErr := m.killer.KillRunner(ctx, group, snap.Name); killErr != nil {
				m.logger.ErrorContext(ctx, "failed to kill timed-out runner", logging.KeyGroup, group, logging.KeyRunner, snap.Name, logging.KeyError, killErr)
			}
		}
	}
}

func (m *Monitor) checkIdleTimeouts(ctx context.Context, group string, snapshots []model.RunnerSnapshot) {
	if m.cfg.IdleTimeout <= 0 {
		return
	}

	minRunners := 0
	if m.cfg.GroupMinRunners != nil {
		minRunners = m.cfg.GroupMinRunners[group]
	}

	now := time.Now()
	var timedOut []model.RunnerSnapshot
	for _, snap := range snapshots {
		if snap.State != "idle" || snap.StartedAt.IsZero() {
			continue
		}
		if now.Sub(snap.StartedAt) > m.cfg.IdleTimeout {
			timedOut = append(timedOut, snap)
		}
	}

	idleCount := 0
	for _, snap := range snapshots {
		if snap.State == "idle" {
			idleCount++
		}
	}

	killable := idleCount - minRunners
	for _, snap := range timedOut {
		if killable <= 0 {
			break
		}
		m.issues = append(m.issues, model.HealthIssue{
			Level:      model.LevelWarning,
			Type:       model.EventHealthIdleTimeout,
			Group:      group,
			Runner:     snap.Name,
			Message:    fmt.Sprintf("runner %s has been idle for %s (timeout: %s)", snap.Name, now.Sub(snap.StartedAt).Round(time.Second), m.cfg.IdleTimeout),
			DetectedAt: now,
		})
		if m.killer != nil {
			if killErr := m.killer.KillRunner(ctx, group, snap.Name); killErr != nil {
				m.logger.ErrorContext(ctx, "failed to kill idle runner", logging.KeyGroup, group, logging.KeyRunner, snap.Name, logging.KeyError, killErr)
			}
		}
		killable--
	}
}

func (m *Monitor) checkGroupDivergence(group string, actualCount int, gs *groupState) {
	if m.cfg.DivergenceTimeout <= 0 {
		return
	}
	if gs.lastDesiredCount == 0 {
		return
	}

	if actualCount == gs.lastDesiredCount {
		gs.degradedSince = nil
		return
	}

	now := time.Now()
	if gs.degradedSince == nil {
		gs.degradedSince = &now
		return
	}

	if now.Sub(*gs.degradedSince) < m.cfg.DivergenceTimeout {
		return
	}

	m.issues = append(m.issues, model.HealthIssue{
		Level:      model.LevelWarning,
		Type:       model.EventHealthGroupDegraded,
		Group:      group,
		Message:    fmt.Sprintf("group %s has %d runners but %d desired for %s", group, actualCount, gs.lastDesiredCount, now.Sub(*gs.degradedSince).Round(time.Second)),
		DetectedAt: now,
	})
}

func (m *Monitor) checkConsecutiveFailures(group string, gs *groupState) {
	if m.cfg.MaxConsecutiveFailures <= 0 {
		return
	}
	if gs.consecutiveFailures <= m.cfg.MaxConsecutiveFailures {
		return
	}

	m.issues = append(m.issues, model.HealthIssue{
		Level:      model.LevelCritical,
		Type:       model.EventHealthGroupFailing,
		Group:      group,
		Message:    fmt.Sprintf("group %s has %d consecutive start failures (threshold: %d)", group, gs.consecutiveFailures, m.cfg.MaxConsecutiveFailures),
		DetectedAt: time.Now(),
	})
}
