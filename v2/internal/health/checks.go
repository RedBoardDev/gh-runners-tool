package health

import (
	"context"
	"fmt"
	"syscall"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

func (m *Monitor) runChecks(ctx context.Context) {
	start := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()

	m.issues = m.issues[:0]

	snapshots := m.runners.Snapshots()
	totalActual := 0
	totalDesired := 0

	for group, snaps := range snapshots {
		m.checkRunnerLiveness(group, snaps)
		m.checkRunnerTimeouts(group, snaps)
		totalActual += len(snaps)
		totalDesired += len(snaps)
	}

	m.checkDiskSpace()
	m.lastCheck = time.Now()
	checkDuration := time.Since(start)

	for _, r := range m.reporters {
		r.ReportDaemonHealth(ctx, len(snapshots), totalActual, totalDesired, checkDuration)
	}
	for group, snaps := range snapshots {
		for _, r := range m.reporters {
			r.ReportGroupHealth(ctx, group, len(snaps), len(snaps))
		}
	}

	for _, issue := range m.issues {
		m.notifier.Notify(ctx, model.Event{
			Type:      fmt.Sprintf("health.%s", issue.Type),
			Level:     issue.Level,
			Group:     issue.Group,
			Runner:    issue.Runner,
			Message:   issue.Message,
			Timestamp: issue.DetectedAt,
		})
	}
}

func (m *Monitor) checkRunnerLiveness(group string, snapshots []model.RunnerSnapshot) {
	for _, snap := range snapshots {
		if snap.PID <= 0 {
			continue
		}
		if err := syscall.Kill(snap.PID, 0); err != nil {
			m.issues = append(m.issues, model.HealthIssue{
				Level:      model.LevelError,
				Type:       "zombie_runner",
				Group:      group,
				Runner:     snap.Name,
				Message:    fmt.Sprintf("runner %s (pid %d) is no longer alive", snap.Name, snap.PID),
				DetectedAt: time.Now(),
			})
		}
	}
}

func (m *Monitor) checkRunnerTimeouts(group string, snapshots []model.RunnerSnapshot) {
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
		if now.Sub(snap.StartedAt) > m.cfg.RunnerTimeout {
			m.issues = append(m.issues, model.HealthIssue{
				Level:      model.LevelWarning,
				Type:       "runner_timeout",
				Group:      group,
				Runner:     snap.Name,
				Message:    fmt.Sprintf("runner %s has been busy for %s (timeout: %s)", snap.Name, now.Sub(snap.StartedAt).Round(time.Second), m.cfg.RunnerTimeout),
				DetectedAt: now,
			})
		}
	}
}

func (m *Monitor) checkDiskSpace() {
	if m.cfg.MinDiskSpace <= 0 {
		return
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		m.logger.Warn("failed to check disk space", "error", err)
		return
	}

	available := int64(stat.Bavail) * int64(stat.Bsize)
	if available < m.cfg.MinDiskSpace {
		m.issues = append(m.issues, model.HealthIssue{
			Level:      model.LevelWarning,
			Type:       "disk_low",
			Group:      "",
			Runner:     "",
			Message:    fmt.Sprintf("available disk space %d bytes is below minimum %d bytes", available, m.cfg.MinDiskSpace),
			DetectedAt: time.Now(),
		})
	}
}
