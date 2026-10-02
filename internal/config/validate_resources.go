package config

import (
	"fmt"
)

func validateResources(cfg *Config) []error {
	var errs []error
	for i, g := range cfg.Groups {
		errs = append(errs, validateGroupResources(i, g)...)
	}
	return errs
}

func validateGroupResources(index int, g GroupConfig) []error {
	prefix := fmt.Sprintf("groups[%d] (%s): resources", index, g.Name)
	var errs []error

	memoryMax, maxErr := g.Resources.MemoryMaxBytes()
	if maxErr != nil {
		errs = append(errs, fmt.Errorf("%s.memory_max: %w", prefix, maxErr))
	}
	if maxErr == nil && g.Resources.MemoryMax != "" && memoryMax == 0 {
		errs = append(errs, fmt.Errorf("%s.memory_max must be greater than 0", prefix))
	}

	memoryHigh, highErr := g.Resources.MemoryHighBytes()
	if highErr != nil {
		errs = append(errs, fmt.Errorf("%s.memory_high: %w", prefix, highErr))
	}
	if highErr == nil && g.Resources.MemoryHigh != "" && memoryHigh == 0 {
		errs = append(errs, fmt.Errorf("%s.memory_high must be greater than 0", prefix))
	}

	if maxErr == nil && highErr == nil && memoryMax > 0 && memoryHigh > memoryMax {
		errs = append(errs, fmt.Errorf("%s.memory_high (%s) must be <= memory_max (%s)",
			prefix, g.Resources.MemoryHigh, g.Resources.MemoryMax))
	}

	if w := g.Resources.CPUWeight; w < 0 || w > maxCPUWeight {
		errs = append(errs, fmt.Errorf("%s.cpu_weight must be between 1 and %d, got %d", prefix, maxCPUWeight, w))
	}
	return errs
}
