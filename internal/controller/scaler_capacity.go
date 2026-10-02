package controller

import (
	"context"
	"sort"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/capacity"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/logging"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/runner"
)

type capacityBudget interface {
	Acquire(group string, want int) capacity.Grant
}

type groupStatsSink interface {
	UpdateGroupStats(group string, desired int)
}

type ScalerOption func(*MacOSScaler)

func WithGroupStats(sink groupStatsSink) ScalerOption {
	return func(s *MacOSScaler) {
		s.stats = sink
	}
}

func WithCapacityBudget(budget capacityBudget) ScalerOption {
	return func(s *MacOSScaler) {
		s.budget = budget
	}
}

func (s *MacOSScaler) Wake(ctx context.Context) {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()

	if !s.hasCount {
		return
	}
	s.reconcile(ctx, s.lastCount)
}

func (s *MacOSScaler) attach(scaleSetID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scaleSetID = scaleSetID
}

func (s *MacOSScaler) reconcile(ctx context.Context, count int) int {
	s.lastCount, s.hasCount = count, true

	target := s.minRunners + count
	if target > s.maxRunners {
		target = s.maxRunners
	}

	// Calling this after s.mu is taken inverts the lock order with the health monitor, which snapshots scalers while holding its own lock.
	if s.stats != nil {
		s.stats.UpdateGroupStats(s.groupName, target)
	}

	if s.budget != nil {
		s.trimSurplusIdle(ctx, target)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	need := target - len(s.idle) - len(s.busy)
	if s.budget != nil {
		s.provisionWithinBudget(ctx, need)
	} else {
		for i := 0; i < need; i++ {
			s.startAndLog(ctx, nil)
		}
	}

	return len(s.idle) + len(s.busy)
}

func (s *MacOSScaler) startAndLog(ctx context.Context, lease *capacity.Lease) {
	if err := s.startRunner(ctx, lease); err != nil {
		s.logger.ErrorContext(ctx, "failed to start runner",
			logging.KeyGroup, s.groupName,
			logging.KeyError, err,
		)
	}
}

func (s *MacOSScaler) provisionWithinBudget(ctx context.Context, need int) {
	if s.stopped {
		return
	}
	grant := s.budget.Acquire(s.groupName, need)
	for _, lease := range grant.Leases {
		s.startAndLog(ctx, lease)
	}
}

func (s *MacOSScaler) trimSurplusIdle(ctx context.Context, target int) {
	for _, name := range s.surplusIdle(target) {
		if err := s.releaseSurplusRunner(ctx, name); err != nil {
			s.logger.WarnContext(ctx, "failed to release surplus idle runner",
				logging.KeyRunner, name,
				logging.KeyGroup, s.groupName,
				logging.KeyError, err,
			)
		}
	}
}

// Stopping the process before GitHub accepts the removal would kill a runner
// that was just assigned a job whose JobStarted message has not arrived yet.
func (s *MacOSScaler) releaseSurplusRunner(ctx context.Context, name string) error {
	s.mu.Lock()
	proc, ok := s.idle[name]
	s.mu.Unlock()
	if !ok {
		return nil
	}

	if proc.RunnerID != 0 {
		if err := s.removeFromGitHub(ctx, proc.RunnerID); err != nil {
			s.logger.InfoContext(ctx, "keeping idle runner, GitHub refused its removal",
				logging.KeyRunner, name,
				logging.KeyGroup, s.groupName,
				logging.KeyError, err,
			)
			return nil
		}
	}

	s.mu.Lock()
	_, stillIdle := s.idle[name]
	delete(s.idle, name)
	s.mu.Unlock()
	if !stillIdle {
		return nil
	}

	s.logger.InfoContext(ctx, "releasing surplus idle runner",
		logging.KeyRunner, name,
		logging.KeyGroup, s.groupName,
	)
	return s.teardownProcess(ctx, name, proc)
}

func (s *MacOSScaler) removeFromGitHub(ctx context.Context, runnerID int) error {
	regCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	return s.client.RemoveRunner(regCtx, runnerID)
}

func (s *MacOSScaler) watchIdleExit(ctx context.Context, name string, proc *runner.Process) {
	exited := proc.Exited()
	if s.budget == nil || exited == nil {
		return
	}
	bgCtx := context.WithoutCancel(ctx)
	go func() {
		<-exited
		if err := s.killIdleRunner(bgCtx, name); err != nil {
			s.logger.DebugContext(bgCtx, "exited runner already gone",
				logging.KeyRunner, name,
				logging.KeyError, err,
			)
		}
	}()
}

func (s *MacOSScaler) surplusIdle(target int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	keep := max(s.minRunners, target-len(s.busy))
	surplus := len(s.idle) - keep
	if surplus <= 0 {
		return nil
	}

	names := make([]string, 0, len(s.idle))
	for name := range s.idle {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := s.idle[names[i]], s.idle[names[j]]
		if !a.StartedAt.Equal(b.StartedAt) {
			return a.StartedAt.After(b.StartedAt)
		}
		return names[i] < names[j]
	})
	return names[:surplus]
}

func (s *MacOSScaler) trackLease(name string, lease *capacity.Lease) {
	if lease == nil {
		return
	}
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	if s.leases == nil {
		s.leases = make(map[string]*capacity.Lease)
	}
	s.leases[name] = lease
}

func (s *MacOSScaler) releaseLease(name string) {
	s.leaseMu.Lock()
	lease := s.leases[name]
	delete(s.leases, name)
	s.leaseMu.Unlock()

	lease.Release()
}
