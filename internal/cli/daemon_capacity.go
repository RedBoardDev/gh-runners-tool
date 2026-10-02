package cli

import (
	"fmt"
	"log/slog"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/api"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/capacity"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/config"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/controller"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/health"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/runner"
)

type hostLimits struct {
	process    []runner.ProcessOption
	budget     *capacity.Allocator
	controller []controller.Option
	health     []health.MonitorOption
	api        []api.ServerOption
}

func buildHostLimits(cfg *config.Config, logger *slog.Logger) (hostLimits, error) {
	for _, w := range cfg.Warnings {
		logger.Warn("config warning", "detail", w)
	}

	process, err := buildCgroupOptions(cfg, logger)
	if err != nil {
		return hostLimits{}, err
	}
	budget, err := buildCapacity(cfg, logger)
	if err != nil {
		return hostLimits{}, err
	}

	host := hostLimits{process: process, budget: budget}
	if budget != nil {
		host.controller = []controller.Option{controller.WithCapacity(budget)}
		host.health = []health.MonitorOption{health.WithCapacity(budget)}
		host.api = []api.ServerOption{api.WithCapacity(budget)}
	}
	return host, nil
}

func buildCapacity(cfg *config.Config, logger *slog.Logger) (*capacity.Allocator, error) {
	if cfg.Capacity == nil {
		return nil, nil
	}

	total, err := toResources(*cfg.Capacity)
	if err != nil {
		return nil, fmt.Errorf("capacity: %w", err)
	}

	specs := make([]capacity.GroupSpec, 0, len(cfg.Groups))
	for _, g := range cfg.Groups {
		reserve, err := toResources(g.Reserve)
		if err != nil {
			return nil, fmt.Errorf("group %q reserve: %w", g.Name, err)
		}
		specs = append(specs, capacity.GroupSpec{
			Name:       g.Name,
			Priority:   g.Priority,
			MinRunners: g.MinRunners,
			Reserve:    reserve,
		})
	}

	logger.Info("host capacity budget enabled", "total", total.String(), "groups", len(specs))
	return capacity.New(total, specs, logger), nil
}

func toResources(a config.ResourceAmount) (capacity.Resources, error) {
	memory, err := a.MemoryBytes()
	if err != nil {
		return capacity.Resources{}, err
	}
	cpu, err := a.CPUMilli()
	if err != nil {
		return capacity.Resources{}, err
	}
	return capacity.Resources{MemoryBytes: memory, CPUMilli: cpu}, nil
}
