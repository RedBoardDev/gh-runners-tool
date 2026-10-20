package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/capacity"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/logging"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/runner"
)

func (s *MacOSScaler) startRunner(ctx context.Context, lease *capacity.Lease) (err error) {
	defer func() {
		if err != nil {
			lease.Cancel()
		}
	}()

	randBytes := make([]byte, 4)
	if _, randErr := rand.Read(randBytes); randErr != nil {
		return fmt.Errorf("generate runner ID: %w", randErr)
	}
	name := fmt.Sprintf("%s-%s", s.groupName, hex.EncodeToString(randBytes))

	jitConfig, runnerID, err := s.client.GenerateJITConfig(ctx, s.scaleSetID, name)
	if err != nil {
		return fmt.Errorf("generate JIT config for %q: %w", name, err)
	}

	defer func() {
		if err != nil {
			s.deregisterRunner(ctx, name, runnerID)
		}
	}()

	instance := model.RunnerInstance{
		Name:  name,
		Group: s.groupName,
	}

	workdir, err := s.process.Prepare(ctx, &instance, s.cachedDir)
	if err != nil {
		return fmt.Errorf("prepare runner %q: %w", name, err)
	}

	logFile, err := s.logMgr.RunnerOutputFile(s.groupName, name)
	if err != nil {
		return fmt.Errorf("open runner log for %q: %w", name, err)
	}

	proc, err := s.process.Start(ctx, &instance, workdir, jitConfig, logFile)
	if err != nil {
		return fmt.Errorf("start runner %q: %w", name, err)
	}
	proc.RunnerID = runnerID

	s.idle[name] = proc
	s.trackLease(name, lease)
	s.watchIdleExit(ctx, name, proc)

	s.logger.InfoContext(ctx, "runner provisioned",
		logging.KeyRunner, name,
		logging.KeyGroup, s.groupName,
		logging.KeyPID, proc.PID,
	)

	return nil
}

func (s *MacOSScaler) deregisterRunner(ctx context.Context, name string, runnerID int) {
	regCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()

	if err := s.removeRegistration(regCtx, name, runnerID); err != nil {
		s.logger.WarnContext(ctx, "failed to deregister runner from GitHub",
			logging.KeyRunner, name,
			logging.KeyGroup, s.groupName,
			logging.KeyError, err,
		)
	}
}

func (s *MacOSScaler) removeRegistration(ctx context.Context, name string, runnerID int) error {
	if runnerID == 0 {
		return nil
	}
	if err := s.client.RemoveRunner(ctx, runnerID); err != nil {
		return err
	}
	s.logger.InfoContext(ctx, "runner deregistered",
		logging.KeyRunner, name,
		logging.KeyGroup, s.groupName,
	)
	return nil
}

func (s *MacOSScaler) killRunner(ctx context.Context, runnerName string) error {
	s.mu.Lock()
	proc := s.idle[runnerName]
	if proc == nil {
		proc = s.busy[runnerName]
	}
	delete(s.idle, runnerName)
	delete(s.busy, runnerName)
	s.mu.Unlock()

	if proc == nil {
		return fmt.Errorf("runner %q not found in group %q", runnerName, s.groupName)
	}

	return s.teardownRunner(ctx, runnerName, proc)
}

// killIdleRunner tears down a runner only if it is still idle: between the
// health snapshot and the kill, the runner may have picked up a job, and
// killing it then would destroy the job's workspace mid-run.
func (s *MacOSScaler) killIdleRunner(ctx context.Context, runnerName string) error {
	s.mu.Lock()
	proc, isIdle := s.idle[runnerName]
	if !isIdle {
		_, isBusy := s.busy[runnerName]
		s.mu.Unlock()
		if isBusy {
			s.logger.InfoContext(ctx, "skipping idle kill, runner picked up a job",
				logging.KeyRunner, runnerName,
				logging.KeyGroup, s.groupName,
			)
			return nil
		}
		return fmt.Errorf("runner %q not found in group %q", runnerName, s.groupName)
	}
	delete(s.idle, runnerName)
	s.mu.Unlock()

	return s.teardownRunner(ctx, runnerName, proc)
}

func (s *MacOSScaler) teardownRunner(ctx context.Context, runnerName string, proc *runner.Process) error {
	if err := s.teardownProcess(ctx, runnerName, proc); err != nil {
		return err
	}

	s.deregisterRunner(ctx, runnerName, proc.RunnerID)

	s.logger.InfoContext(ctx, "killed runner", logging.KeyRunner, runnerName, logging.KeyGroup, s.groupName)
	return nil
}

func (s *MacOSScaler) teardownProcess(ctx context.Context, runnerName string, proc *runner.Process) error {
	stopErr := s.process.Stop(ctx, proc)
	if stopErr != nil {
		s.logger.WarnContext(ctx, "failed to stop runner during kill",
			logging.KeyRunner, runnerName,
			logging.KeyError, stopErr,
		)
	}

	cleanupErr := s.process.Cleanup(proc)
	s.releaseLease(runnerName)
	if cleanupErr != nil {
		return fmt.Errorf("cleanup runner %q: %w", runnerName, cleanupErr)
	}

	if logsErr := s.logMgr.RemoveRunnerLogs(s.groupName, runnerName); logsErr != nil {
		s.logger.WarnContext(ctx, "failed to remove runner log dir",
			logging.KeyRunner, runnerName,
			logging.KeyError, logsErr,
		)
	}
	return nil
}

// Detaching deregistration from ctx lets shutdown outlive systemd's
// TimeoutStopSec, whose SIGKILL then leaves every pending runner registered.
func (s *MacOSScaler) Shutdown(ctx context.Context) {
	s.mu.Lock()
	all := make(map[string]*runner.Process, len(s.idle)+len(s.busy))
	maps.Copy(all, s.idle)
	maps.Copy(all, s.busy)
	s.idle = make(map[string]*runner.Process)
	s.busy = make(map[string]*runner.Process)
	s.stopped = true
	s.mu.Unlock()

	var (
		wg      sync.WaitGroup
		leftMu  sync.Mutex
		left    []string
		leftErr []error
	)
	for name, proc := range all {
		wg.Go(func() {
			if err := s.shutdownRunner(ctx, name, proc); err != nil {
				leftMu.Lock()
				left = append(left, name)
				leftErr = append(leftErr, err)
				leftMu.Unlock()
			}
		})
	}
	wg.Wait()

	if len(left) > 0 {
		slices.Sort(left)
		s.logger.WarnContext(ctx, "runners left registered on GitHub after shutdown",
			logging.KeyGroup, s.groupName,
			"runners", left,
			logging.KeyError, errors.Join(leftErr...),
		)
	}
}

func (s *MacOSScaler) shutdownRunner(ctx context.Context, name string, proc *runner.Process) error {
	if stopErr := s.process.Stop(ctx, proc); stopErr != nil {
		s.logger.WarnContext(ctx, "failed to stop runner during shutdown",
			logging.KeyRunner, name,
			logging.KeyError, stopErr,
		)
	}
	cleanupErr := s.process.Cleanup(proc)
	s.releaseLease(name)
	if cleanupErr != nil {
		s.logger.WarnContext(ctx, "failed to cleanup runner during shutdown",
			logging.KeyRunner, name,
			logging.KeyError, cleanupErr,
		)
	}
	removeErr := s.removeRegistration(ctx, name, proc.RunnerID)
	if logsErr := s.logMgr.RemoveRunnerLogs(s.groupName, name); logsErr != nil {
		s.logger.WarnContext(ctx, "failed to remove runner log dir during shutdown",
			logging.KeyRunner, name,
			logging.KeyError, logsErr,
		)
	}
	return removeErr
}

func (s *MacOSScaler) Snapshots() []model.RunnerSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshots := make([]model.RunnerSnapshot, 0, len(s.idle)+len(s.busy))
	for name, proc := range s.idle {
		snapshots = append(snapshots, model.RunnerSnapshot{
			Name:      name,
			Group:     s.groupName,
			State:     "idle",
			PID:       proc.PID,
			StartedAt: proc.StartedAt,
		})
	}
	for name, proc := range s.busy {
		snapshots = append(snapshots, model.RunnerSnapshot{
			Name:      name,
			Group:     s.groupName,
			State:     "busy",
			PID:       proc.PID,
			StartedAt: proc.StartedAt,
			BusySince: proc.BusySince,
		})
	}
	return snapshots
}
