package controller

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/logging"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/runner"
	"github.com/actions/scaleset"
)

type MacOSScaler struct {
	client     scaleSetClient
	process    *runner.ProcessManager
	logMgr     *logging.LogManager
	notifier   notifier
	scaleSetID int
	groupName  string
	maxRunners int
	minRunners int
	cachedDir  string
	logger     *slog.Logger

	mu   sync.Mutex
	idle map[string]*runner.Process
	busy map[string]*runner.Process
}

func NewMacOSScaler(
	client scaleSetClient,
	process *runner.ProcessManager,
	logMgr *logging.LogManager,
	notifier notifier,
	scaleSetID int,
	groupName string,
	maxRunners int,
	minRunners int,
	cachedDir string,
	logger *slog.Logger,
) *MacOSScaler {
	return &MacOSScaler{
		client:     client,
		process:    process,
		logMgr:     logMgr,
		notifier:   notifier,
		scaleSetID: scaleSetID,
		groupName:  groupName,
		maxRunners: maxRunners,
		minRunners: minRunners,
		cachedDir:  cachedDir,
		logger:     logger,
		idle:       make(map[string]*runner.Process),
		busy:       make(map[string]*runner.Process),
	}
}

func (s *MacOSScaler) HandleDesiredRunnerCount(ctx context.Context, count int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.minRunners + count
	if target > s.maxRunners {
		target = s.maxRunners
	}

	current := len(s.idle) + len(s.busy)
	for i := 0; i < target-current; i++ {
		if err := s.startRunner(ctx); err != nil {
			s.logger.ErrorContext(ctx, "failed to start runner",
				"group", s.groupName,
				"error", err,
			)
		}
	}

	return len(s.idle) + len(s.busy), nil
}

func (s *MacOSScaler) HandleJobStarted(ctx context.Context, jobInfo *scaleset.JobStarted) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	proc, ok := s.idle[jobInfo.RunnerName]
	if !ok {
		s.logger.WarnContext(ctx, "job started for unknown runner",
			"runner", jobInfo.RunnerName,
			"group", s.groupName,
		)
		return nil
	}

	delete(s.idle, jobInfo.RunnerName)
	s.busy[jobInfo.RunnerName] = proc

	s.logger.InfoContext(ctx, "job started",
		"runner", jobInfo.RunnerName,
		"group", s.groupName,
		"job", jobInfo.JobDisplayName,
	)

	s.notifier.Notify(ctx, model.Event{
		Type:      "runner.started",
		Level:     model.LevelInfo,
		Group:     s.groupName,
		Runner:    jobInfo.RunnerName,
		Message:   fmt.Sprintf("Job started: %s", jobInfo.JobDisplayName),
		Timestamp: time.Now(),
	})

	return nil
}

func (s *MacOSScaler) HandleJobCompleted(ctx context.Context, jobInfo *scaleset.JobCompleted) error {
	s.mu.Lock()
	proc := s.busy[jobInfo.RunnerName]
	if proc == nil {
		proc = s.idle[jobInfo.RunnerName]
	}
	delete(s.busy, jobInfo.RunnerName)
	delete(s.idle, jobInfo.RunnerName)
	s.mu.Unlock()

	if proc != nil {
		stopErr := s.process.Stop(ctx, proc)
		if stopErr != nil {
			s.logger.WarnContext(ctx, "failed to stop runner",
				"runner", jobInfo.RunnerName,
				"error", stopErr,
			)
		}
		cleanupErr := s.process.Cleanup(proc)
		if cleanupErr != nil {
			s.logger.WarnContext(ctx, "failed to cleanup runner",
				"runner", jobInfo.RunnerName,
				"error", cleanupErr,
			)
		}
	} else {
		s.logger.WarnContext(ctx, "job completed for unknown runner",
			"runner", jobInfo.RunnerName,
			"group", s.groupName,
		)
	}

	eventType := "runner.completed"
	if jobInfo.Result != "succeeded" {
		eventType = "runner.failed"
	}

	s.logger.InfoContext(ctx, "job completed",
		"runner", jobInfo.RunnerName,
		"group", s.groupName,
		"result", jobInfo.Result,
	)

	s.notifier.Notify(ctx, model.Event{
		Type:      eventType,
		Level:     model.LevelInfo,
		Group:     s.groupName,
		Runner:    jobInfo.RunnerName,
		Message:   fmt.Sprintf("Job completed: %s", jobInfo.Result),
		Timestamp: time.Now(),
	})

	return nil
}
