package controller

import (
	"log/slog"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/capacity"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/config"
)

type groupCapacity interface {
	capacityBudget
	Register(group string, w capacity.Waker)
	Unregister(group string)
}

type Option func(*GroupController)

func WithCapacity(budget groupCapacity) Option {
	return func(c *GroupController) {
		c.budget = budget
	}
}

func (c *GroupController) SetGroupStats(sink groupStatsSink) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats = sink
}

// Building a fresh scaler on each listener restart would orphan the running
// runners and leak their leases, shrinking the budget for good.
func (c *GroupController) scalerFor(group *config.GroupConfig, scaleSetID int, cachedDir string, logger *slog.Logger) *MacOSScaler {
	if c.budget != nil {
		c.mu.Lock()
		prev := c.retained[group.Name]
		c.mu.Unlock()
		if prev != nil {
			prev.attach(scaleSetID)
			return prev
		}
	}

	c.mu.Lock()
	stats := c.stats
	c.mu.Unlock()

	scaler := NewMacOSScaler(
		c.client, c.process, c.logMgr, c.notifier,
		scaleSetID, group.Name, group.MaxRunners, group.MinRunners,
		cachedDir, logger,
		WithCapacityBudget(c.budget),
		WithGroupStats(stats),
	)
	if c.budget != nil {
		c.mu.Lock()
		c.retained[group.Name] = scaler
		c.mu.Unlock()
	}
	return scaler
}

func (c *GroupController) forgetScaler(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.retained, name)
}
