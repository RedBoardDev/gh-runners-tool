package config

import (
	"fmt"
	"math"
)

const (
	maxCPUWeight  = 10000
	milliPerCPU   = 1000
	cpuMilliScale = float64(milliPerCPU)
)

type ResourceAmount struct {
	Memory string  `yaml:"memory"`
	CPUs   float64 `yaml:"cpus"`
}

type ResourcesConfig struct {
	MemoryMax  string `yaml:"memory_max"`
	MemoryHigh string `yaml:"memory_high"`
	CPUWeight  int    `yaml:"cpu_weight"`
}

func (a ResourceAmount) IsZero() bool {
	return a.Memory == "" && a.CPUs == 0
}

func (a ResourceAmount) MemoryBytes() (int64, error) {
	if a.Memory == "" {
		return 0, nil
	}
	return ParseByteSize(a.Memory)
}

func (a ResourceAmount) CPUMilli() (int64, error) {
	if math.IsNaN(a.CPUs) || math.IsInf(a.CPUs, 0) || a.CPUs < 0 {
		return 0, fmt.Errorf("cpus must be a non-negative number, got %v", a.CPUs)
	}
	return int64(math.Round(a.CPUs * cpuMilliScale)), nil
}

func (r ResourcesConfig) IsZero() bool {
	return r.MemoryMax == "" && r.MemoryHigh == "" && r.CPUWeight == 0
}

func (r ResourcesConfig) MemoryMaxBytes() (int64, error) {
	return parseOptionalSize(r.MemoryMax)
}

func (r ResourcesConfig) MemoryHighBytes() (int64, error) {
	return parseOptionalSize(r.MemoryHigh)
}

func parseOptionalSize(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	return ParseByteSize(s)
}
