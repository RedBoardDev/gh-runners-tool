package health

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

type RunnerStateProvider interface {
	Snapshots() map[string][]model.RunnerSnapshot
}

type Notifier interface {
	Notify(ctx context.Context, event *model.Event)
}

type Reporter interface {
	ReportDaemonHealth(ctx context.Context, groups int, totalActual int, totalDesired int, checkDuration time.Duration)
	ReportGroupHealth(ctx context.Context, group string, actual int, desired int)
}

type RunnerKiller interface {
	KillRunner(ctx context.Context, group string, runner string) error
	// KillIdleRunner must refuse to kill a runner that has picked up a job
	// since the health snapshot was taken.
	KillIdleRunner(ctx context.Context, group string, runner string) error
}

type MonitorConfig struct {
	Enabled                bool
	CheckInterval          time.Duration
	RunnerTimeout          time.Duration
	IdleTimeout            time.Duration
	DivergenceTimeout      time.Duration
	MaxConsecutiveFailures int
	FailureCooldown        time.Duration
	MinDiskSpace           int64
	GroupMinRunners        map[string]int
}

type Monitor struct {
	cfg       MonitorConfig
	logger    *slog.Logger
	notifier  Notifier
	runners   RunnerStateProvider
	reporters []Reporter
	killer    RunnerKiller

	mu        sync.RWMutex
	lastCheck time.Time
	issues    []model.HealthIssue
	groups    map[string]*groupState
}

func NewMonitor(
	cfg MonitorConfig,
	notifier Notifier,
	runners RunnerStateProvider,
	reporters []Reporter,
	killer RunnerKiller,
	logger *slog.Logger,
) *Monitor {
	return &Monitor{
		cfg:       cfg,
		logger:    logger,
		notifier:  notifier,
		runners:   runners,
		reporters: reporters,
		killer:    killer,
		groups:    make(map[string]*groupState),
	}
}

func (m *Monitor) Run(ctx context.Context) error {
	if !m.cfg.Enabled {
		return nil
	}

	ticker := time.NewTicker(m.cfg.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			m.runChecks(ctx)
		}
	}
}

func (m *Monitor) Status() HealthStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	copied := make([]model.HealthIssue, len(m.issues))
	copy(copied, m.issues)

	return HealthStatus{
		LastCheck: m.lastCheck,
		Issues:    copied,
	}
}
