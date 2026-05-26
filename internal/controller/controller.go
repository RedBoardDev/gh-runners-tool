package controller

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/config"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/logging"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/runner"
	"github.com/actions/scaleset"
	"github.com/actions/scaleset/listener"
)

type scaleSetClient interface {
	GetScaleSet(ctx context.Context, runnerGroupID int, name string) (*scaleset.RunnerScaleSet, error)
	CreateScaleSet(ctx context.Context, name string, runnerGroupID int, labels []string) (*scaleset.RunnerScaleSet, error)
	DeleteScaleSet(ctx context.Context, id int) error
	GenerateJITConfig(ctx context.Context, scaleSetID int, runnerName string) (string, error)
	OpenSession(ctx context.Context, scaleSetID int, owner string) (*scaleset.MessageSessionClient, error)
	NewListener(session *scaleset.MessageSessionClient, scaleSetID int, maxRunners int) (*listener.Listener, error)
}

type notifier interface {
	Notify(ctx context.Context, event *model.Event)
}

type ControllerConfig struct {
	RunnerVersion string
	RunnerGroupID int
}

type GroupController struct {
	client    scaleSetClient
	binary    *runner.BinaryManager
	process   *runner.ProcessManager
	notifier  notifier
	logMgr    *logging.LogManager
	groups    []config.GroupConfig
	globalCfg ControllerConfig
	logger    *slog.Logger

	mu      sync.Mutex
	scalers map[string]*MacOSScaler
}

func New(
	client scaleSetClient,
	binary *runner.BinaryManager,
	process *runner.ProcessManager,
	notifier notifier,
	logMgr *logging.LogManager,
	groups []config.GroupConfig,
	globalCfg ControllerConfig,
	logger *slog.Logger,
) *GroupController {
	return &GroupController{
		client:    client,
		binary:    binary,
		process:   process,
		notifier:  notifier,
		logMgr:    logMgr,
		groups:    groups,
		globalCfg: globalCfg,
		logger:    logger,
		scalers:   make(map[string]*MacOSScaler),
	}
}

func (c *GroupController) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, len(c.groups))

	for i := range c.groups {
		group := &c.groups[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.runGroup(ctx, group); err != nil {
				errCh <- err
				cancel()
			}
		}()
	}

	<-ctx.Done()
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *GroupController) Shutdown(ctx context.Context) {
	c.mu.Lock()
	scalers := make(map[string]*MacOSScaler, len(c.scalers))
	for k, v := range c.scalers {
		scalers[k] = v
	}
	c.mu.Unlock()

	for name, s := range scalers {
		c.logger.InfoContext(ctx, "shutting down scaler", logging.KeyGroup, name)
		s.Shutdown(ctx)
	}
}

func (c *GroupController) Snapshots() map[string][]model.RunnerSnapshot {
	c.mu.Lock()
	scalers := make(map[string]*MacOSScaler, len(c.scalers))
	for k, v := range c.scalers {
		scalers[k] = v
	}
	c.mu.Unlock()

	result := make(map[string][]model.RunnerSnapshot, len(scalers))
	for name, s := range scalers {
		result[name] = s.Snapshots()
	}
	return result
}

func (c *GroupController) KillRunner(ctx context.Context, group, runnerName string) error {
	c.mu.Lock()
	s, ok := c.scalers[group]
	c.mu.Unlock()

	if !ok {
		return fmt.Errorf("kill runner %s: group %q not found", runnerName, group)
	}

	return s.killRunner(ctx, runnerName)
}

func (c *GroupController) registerScaler(name string, s *MacOSScaler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scalers[name] = s
}

func (c *GroupController) unregisterScaler(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.scalers, name)
}
