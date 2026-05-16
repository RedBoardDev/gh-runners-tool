package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/logging"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/runner"
)

func (s *MacOSScaler) startRunner(ctx context.Context) error {
	randBytes := make([]byte, 4)
	if _, err := rand.Read(randBytes); err != nil {
		return fmt.Errorf("generate runner ID: %w", err)
	}
	name := fmt.Sprintf("%s-%s", s.groupName, hex.EncodeToString(randBytes))

	jitConfig, err := s.client.GenerateJITConfig(ctx, s.scaleSetID, name)
	if err != nil {
		return fmt.Errorf("generate JIT config for %q: %w", name, err)
	}

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

	s.idle[name] = proc

	s.logger.InfoContext(ctx, "runner provisioned",
		logging.KeyRunner, name,
		logging.KeyGroup, s.groupName,
		logging.KeyPID, proc.PID,
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

	stopErr := s.process.Stop(ctx, proc)
	if stopErr != nil {
		s.logger.WarnContext(ctx, "failed to stop runner during kill",
			logging.KeyRunner, runnerName,
			logging.KeyError, stopErr,
		)
	}

	cleanupErr := s.process.Cleanup(proc)
	if cleanupErr != nil {
		return fmt.Errorf("cleanup runner %q: %w", runnerName, cleanupErr)
	}

	if logsErr := s.logMgr.RemoveRunnerLogs(s.groupName, runnerName); logsErr != nil {
		s.logger.WarnContext(ctx, "failed to remove runner log dir",
			logging.KeyRunner, runnerName,
			logging.KeyError, logsErr,
		)
	}

	s.logger.InfoContext(ctx, "killed runner", logging.KeyRunner, runnerName, logging.KeyGroup, s.groupName)
	return nil
}

func (s *MacOSScaler) Shutdown(ctx context.Context) {
	s.mu.Lock()
	allProcs := make([]*runner.Process, 0, len(s.idle)+len(s.busy))
	for _, p := range s.idle {
		allProcs = append(allProcs, p)
	}
	for _, p := range s.busy {
		allProcs = append(allProcs, p)
	}
	s.idle = make(map[string]*runner.Process)
	s.busy = make(map[string]*runner.Process)
	s.mu.Unlock()

	for _, proc := range allProcs {
		stopErr := s.process.Stop(ctx, proc)
		if stopErr != nil {
			s.logger.WarnContext(ctx, "failed to stop runner during shutdown",
				logging.KeyRunner, proc.Name,
				logging.KeyError, stopErr,
			)
		}
		cleanupErr := s.process.Cleanup(proc)
		if cleanupErr != nil {
			s.logger.WarnContext(ctx, "failed to cleanup runner during shutdown",
				logging.KeyRunner, proc.Name,
				logging.KeyError, cleanupErr,
			)
		}
		if logsErr := s.logMgr.RemoveRunnerLogs(s.groupName, proc.Name); logsErr != nil {
			s.logger.WarnContext(ctx, "failed to remove runner log dir during shutdown",
				logging.KeyRunner, proc.Name,
				logging.KeyError, logsErr,
			)
		}
	}
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
		})
	}
	return snapshots
}
