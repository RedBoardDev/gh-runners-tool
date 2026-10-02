package config

import (
	"errors"
	"fmt"
)

type resolvedAmount struct {
	memory int64
	cpu    int64
}

func resolveAmount(a ResourceAmount) (resolvedAmount, error) {
	memory, err := a.MemoryBytes()
	if err != nil {
		return resolvedAmount{}, fmt.Errorf("memory: %w", err)
	}
	cpu, err := a.CPUMilli()
	if err != nil {
		return resolvedAmount{}, err
	}
	return resolvedAmount{memory: memory, cpu: cpu}, nil
}

func validateCapacity(cfg *Config) []error {
	var errs []error

	reserves := make([]resolvedAmount, len(cfg.Groups))
	reserveOK := make([]bool, len(cfg.Groups))
	for i := range cfg.Groups {
		g := &cfg.Groups[i]
		resolved, err := resolveAmount(g.Reserve)
		if err != nil {
			errs = append(errs, fmt.Errorf("groups[%d] (%s): reserve: %w", i, g.Name, err))
			continue
		}
		reserves[i] = resolved
		reserveOK[i] = true
	}

	if cfg.Capacity == nil {
		return errs
	}

	if cfg.Capacity.IsZero() {
		return append(errs, errors.New("capacity: set at least one of memory or cpus"))
	}
	total, err := resolveAmount(*cfg.Capacity)
	if err != nil {
		return append(errs, fmt.Errorf("capacity: %w", err))
	}
	if cfg.Capacity.Memory != "" && total.memory == 0 {
		errs = append(errs, errors.New("capacity.memory must be greater than 0"))
	}
	if cfg.Capacity.CPUs != 0 && total.cpu == 0 {
		errs = append(errs, errors.New("capacity.cpus must be at least 0.001"))
	}

	var floor resolvedAmount
	floorComplete := true
	for i := range cfg.Groups {
		g := &cfg.Groups[i]
		if !reserveOK[i] {
			floorComplete = false
			continue
		}
		errs = append(errs, reserveFitErrors(i, g, reserves[i], total)...)
		floor.memory += int64(g.MinRunners) * reserves[i].memory
		floor.cpu += int64(g.MinRunners) * reserves[i].cpu
	}

	if floorComplete {
		errs = append(errs, floorFitErrors(cfg, floor, total)...)
	}
	return errs
}

func reserveFitErrors(index int, g *GroupConfig, reserve, total resolvedAmount) []error {
	var errs []error
	if total.memory > 0 && reserve.memory > total.memory {
		errs = append(errs, fmt.Errorf("groups[%d] (%s): reserve.memory %q does not fit in capacity.memory",
			index, g.Name, g.Reserve.Memory))
	}
	if total.cpu > 0 && reserve.cpu > total.cpu {
		errs = append(errs, fmt.Errorf("groups[%d] (%s): reserve.cpus %v does not fit in capacity.cpus %v",
			index, g.Name, g.Reserve.CPUs, cpusOf(total.cpu)))
	}
	return errs
}

func floorFitErrors(cfg *Config, floor, total resolvedAmount) []error {
	var errs []error
	if total.memory > 0 && floor.memory > total.memory {
		errs = append(errs, fmt.Errorf(
			"sum of min_runners * reserve.memory over all groups (%d bytes) does not fit in capacity.memory %q (%d bytes)",
			floor.memory, cfg.Capacity.Memory, total.memory))
	}
	if total.cpu > 0 && floor.cpu > total.cpu {
		errs = append(errs, fmt.Errorf(
			"sum of min_runners * reserve.cpus over all groups (%v) does not fit in capacity.cpus %v",
			cpusOf(floor.cpu), cfg.Capacity.CPUs))
	}
	return errs
}

func cpusOf(milli int64) float64 {
	return float64(milli) / cpuMilliScale
}
