package cli

import (
	"fmt"
	"log/slog"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/cgroup"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/config"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/logging"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/runner"
)

func buildCgroupOptions(cfg *config.Config, logger *slog.Logger) ([]runner.ProcessOption, error) {
	if !cgroup.Supported {
		return nil, nil
	}
	limits, err := cgroupLimits(cfg)
	if err != nil {
		return nil, err
	}
	if len(limits) == 0 {
		return nil, nil
	}

	mgr := cgroup.NewManager(cgroup.DefaultRoot, logger)
	if err := mgr.Setup(needsCPUController(limits)); err != nil {
		return nil, fmt.Errorf("per-runner resource limits are configured but cgroups are unavailable: %w", err)
	}

	swept, err := mgr.SweepStale()
	if err != nil {
		logger.Warn("stale runner cgroup sweep incomplete", logging.KeyError, err)
	}
	if swept > 0 {
		logger.Info("removed stale runner cgroups", "count", swept)
	}

	return []runner.ProcessOption{runner.WithCgroups(mgr, limits)}, nil
}

func needsCPUController(limits map[string]cgroup.Limits) bool {
	for _, l := range limits {
		if l.CPUWeight > 0 {
			return true
		}
	}
	return false
}

func cgroupLimits(cfg *config.Config) (map[string]cgroup.Limits, error) {
	limits := make(map[string]cgroup.Limits)
	for i := range cfg.Groups {
		g := &cfg.Groups[i]
		if g.Resources.IsZero() {
			continue
		}
		memoryMax, err := g.Resources.MemoryMaxBytes()
		if err != nil {
			return nil, fmt.Errorf("group %q resources.memory_max: %w", g.Name, err)
		}
		memoryHigh, err := g.Resources.MemoryHighBytes()
		if err != nil {
			return nil, fmt.Errorf("group %q resources.memory_high: %w", g.Name, err)
		}
		limits[g.Name] = cgroup.Limits{
			MemoryMax:  memoryMax,
			MemoryHigh: memoryHigh,
			CPUWeight:  g.Resources.CPUWeight,
		}
	}
	return limits, nil
}
