package config

import (
	"strings"
	"testing"
)

const capacityGroupYAML = `
groups:
  - name: heavy
    max_runners: 4
    min_runners: 1
    priority: 50
    reserve:
      memory: 9G
      cpus: 6
    resources:
      memory_max: 12G
      memory_high: 11G
      cpu_weight: 100
  - name: light
    max_runners: 10
`

func TestLoad_CapacityAbsent_KeepsLegacyBehaviour(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
groups:
  - name: plain
    max_runners: 3
`))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	g := cfg.Groups[0]
	tests := []struct {
		name string
		ok   bool
	}{
		{"capacity is nil", cfg.Capacity == nil},
		{"priority defaults to 0", g.Priority == 0},
		{"reserve defaults to zero", g.Reserve.IsZero()},
		{"resources default to zero", g.Resources.IsZero()},
		{"no warnings", len(cfg.Warnings) == 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.ok {
				t.Fatalf("expectation %q not met: %+v", tt.name, cfg)
			}
		})
	}
}

func TestLoad_CapacityParsing(t *testing.T) {
	cfg, err := Load(writeConfig(t, "capacity:\n  memory: 48G\n  cpus: 40\n"+capacityGroupYAML))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.Capacity == nil {
		t.Fatal("capacity is nil, want parsed block")
	}

	total, err := resolveAmount(*cfg.Capacity)
	if err != nil {
		t.Fatalf("resolve capacity: %v", err)
	}
	if want := int64(48) << 30; total.memory != want {
		t.Errorf("capacity memory = %d, want %d", total.memory, want)
	}
	if total.cpu != 40_000 {
		t.Errorf("capacity cpu milli = %d, want 40000", total.cpu)
	}

	heavy := cfg.Groups[0]
	if heavy.Priority != 50 {
		t.Errorf("priority = %d, want 50", heavy.Priority)
	}
	reserve, err := resolveAmount(heavy.Reserve)
	if err != nil {
		t.Fatalf("resolve reserve: %v", err)
	}
	if want := int64(9) << 30; reserve.memory != want || reserve.cpu != 6000 {
		t.Errorf("reserve = %+v, want memory %d cpu 6000", reserve, want)
	}

	max, err := heavy.Resources.MemoryMaxBytes()
	if err != nil || max != int64(12)<<30 {
		t.Errorf("memory_max = %d (err %v), want %d", max, err, int64(12)<<30)
	}
	high, err := heavy.Resources.MemoryHighBytes()
	if err != nil || high != int64(11)<<30 {
		t.Errorf("memory_high = %d (err %v), want %d", high, err, int64(11)<<30)
	}
	if heavy.Resources.CPUWeight != 100 {
		t.Errorf("cpu_weight = %d, want 100", heavy.Resources.CPUWeight)
	}

	light := cfg.Groups[1]
	if light.Priority != 0 || !light.Reserve.IsZero() || !light.Resources.IsZero() {
		t.Errorf("light group should keep zero defaults, got %+v", light)
	}
}

func TestResourceAmount_FractionalCPUs(t *testing.T) {
	tests := []struct {
		name string
		cpus float64
		want int64
	}{
		{"whole", 4, 4000},
		{"half", 0.5, 500},
		{"fraction rounds to milli", 1.2345, 1235},
		{"zero", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResourceAmount{CPUs: tt.cpus}.CPUMilli()
			if err != nil || got != tt.want {
				t.Fatalf("CPUMilli() = %d, %v; want %d", got, err, tt.want)
			}
		})
	}
}

func TestLoad_CapacityValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantInErr string
	}{
		{
			name: "capacity block without any dimension",
			yaml: `
capacity: {}
groups:
  - name: g
    max_runners: 1`,
			wantInErr: "capacity: set at least one of memory or cpus",
		},
		{
			name: "invalid capacity memory",
			yaml: `
capacity: {memory: lots}
groups:
  - name: g
    max_runners: 1`,
			wantInErr: "capacity: memory",
		},
		{
			name: "negative capacity cpus",
			yaml: `
capacity: {memory: 8G, cpus: -2}
groups:
  - name: g
    max_runners: 1`,
			wantInErr: "cpus must be a non-negative number",
		},
		{
			name: "nan capacity cpus",
			yaml: `
capacity: {memory: 8G, cpus: .nan}
groups:
  - name: g
    max_runners: 1`,
			wantInErr: "cpus must be a non-negative number",
		},
		{
			name: "negative reserve memory",
			yaml: `
capacity: {memory: 8G}
groups:
  - name: g
    max_runners: 1
    reserve: {memory: -1G}`,
			wantInErr: "reserve: memory",
		},
		{
			name: "negative reserve cpus",
			yaml: `
capacity: {memory: 8G}
groups:
  - name: g
    max_runners: 1
    reserve: {cpus: -1}`,
			wantInErr: "reserve: cpus must be a non-negative number",
		},
		{
			name: "negative reserve without capacity is still rejected",
			yaml: `
groups:
  - name: g
    max_runners: 1
    reserve: {memory: -1G}`,
			wantInErr: "reserve: memory",
		},
		{
			name: "reserve memory larger than capacity",
			yaml: `
capacity: {memory: 8G, cpus: 8}
groups:
  - name: big
    max_runners: 1
    reserve: {memory: 9G}`,
			wantInErr: `reserve.memory "9G" does not fit in capacity.memory`,
		},
		{
			name: "reserve cpus larger than capacity",
			yaml: `
capacity: {memory: 8G, cpus: 4}
groups:
  - name: big
    max_runners: 1
    reserve: {cpus: 5}`,
			wantInErr: "reserve.cpus 5 does not fit in capacity.cpus 4",
		},
		{
			name: "min_runners reservations exceed memory",
			yaml: `
capacity: {memory: 10G, cpus: 100}
groups:
  - name: a
    max_runners: 4
    min_runners: 1
    reserve: {memory: 6G}
  - name: b
    max_runners: 4
    min_runners: 1
    reserve: {memory: 6G}`,
			wantInErr: "sum of min_runners * reserve.memory",
		},
		{
			name: "min_runners reservations exceed cpus",
			yaml: `
capacity: {memory: 100G, cpus: 8}
groups:
  - name: a
    max_runners: 4
    min_runners: 2
    reserve: {cpus: 3}
  - name: b
    max_runners: 4
    min_runners: 1
    reserve: {cpus: 3}`,
			wantInErr: "sum of min_runners * reserve.cpus",
		},
		{
			name: "invalid memory_max",
			yaml: `
groups:
  - name: g
    max_runners: 1
    resources: {memory_max: plenty}`,
			wantInErr: "resources.memory_max",
		},
		{
			name: "zero memory_max",
			yaml: `
groups:
  - name: g
    max_runners: 1
    resources: {memory_max: "0"}`,
			wantInErr: "resources.memory_max must be greater than 0",
		},
		{
			name: "invalid memory_high",
			yaml: `
groups:
  - name: g
    max_runners: 1
    resources: {memory_high: plenty}`,
			wantInErr: "resources.memory_high",
		},
		{
			name: "memory_high above memory_max",
			yaml: `
groups:
  - name: g
    max_runners: 1
    resources: {memory_max: 4G, memory_high: 5G}`,
			wantInErr: "memory_high (5G) must be <= memory_max (4G)",
		},
		{
			name: "cpu_weight below range",
			yaml: `
groups:
  - name: g
    max_runners: 1
    resources: {cpu_weight: -5}`,
			wantInErr: "cpu_weight must be between 1 and 10000",
		},
		{
			name: "cpu_weight above range",
			yaml: `
groups:
  - name: g
    max_runners: 1
    resources: {cpu_weight: 10001}`,
			wantInErr: "cpu_weight must be between 1 and 10000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tt.yaml))
			if err == nil {
				t.Fatal("Load() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantInErr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.wantInErr)
			}
		})
	}
}

func TestLoad_CapacityBoundaryValuesAccepted(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "reserve exactly equals capacity",
			yaml: `
capacity: {memory: 8G, cpus: 4}
groups:
  - name: g
    max_runners: 1
    reserve: {memory: 8G, cpus: 4}`,
		},
		{
			name: "min_runners reservations exactly fill capacity",
			yaml: `
capacity: {memory: 12G, cpus: 8}
groups:
  - name: a
    max_runners: 4
    min_runners: 1
    reserve: {memory: 6G, cpus: 4}
  - name: b
    max_runners: 4
    min_runners: 1
    reserve: {memory: 6G, cpus: 4}`,
		},
		{
			name: "memory only capacity leaves cpus unlimited",
			yaml: `
capacity: {memory: 8G}
groups:
  - name: g
    max_runners: 1
    reserve: {memory: 4G, cpus: 500}`,
		},
		{
			name: "memory_high equals memory_max",
			yaml: `
groups:
  - name: g
    max_runners: 1
    resources: {memory_max: 4G, memory_high: 4G, cpu_weight: 10000}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, tt.yaml)); err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
		})
	}
}

func TestWarnings(t *testing.T) {
	withCapacity := &ResourceAmount{Memory: "8G"}
	tests := []struct {
		name     string
		cfg      Config
		goos     string
		wantSubs []string
	}{
		{
			name: "reserve without capacity",
			cfg:  Config{Groups: []GroupConfig{{Name: "g", Reserve: ResourceAmount{Memory: "1G"}}}},
			goos: "linux",
			wantSubs: []string{
				"groups[0] (g): reserve is ignored because capacity is not set",
			},
		},
		{
			name: "priority without capacity",
			cfg:  Config{Groups: []GroupConfig{{Name: "g", Priority: 5}}},
			goos: "linux",
			wantSubs: []string{
				"groups[0] (g): priority is ignored because capacity is not set",
			},
		},
		{
			name: "reserve and priority with capacity",
			cfg: Config{
				Capacity: withCapacity,
				Groups:   []GroupConfig{{Name: "g", Priority: 5, Reserve: ResourceAmount{Memory: "1G"}}},
			},
			goos: "linux",
		},
		{
			name: "resources on darwin",
			cfg:  Config{Groups: []GroupConfig{{Name: "g", Resources: ResourcesConfig{MemoryMax: "1G"}}}},
			goos: "darwin",
			wantSubs: []string{
				"groups[0] (g): resources are ignored on darwin",
			},
		},
		{
			name: "resources on linux",
			cfg:  Config{Groups: []GroupConfig{{Name: "g", Resources: ResourcesConfig{MemoryMax: "1G"}}}},
			goos: "linux",
		},
		{
			name: "no extras",
			cfg:  Config{Groups: []GroupConfig{{Name: "g"}}},
			goos: "darwin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := warnings(&tt.cfg, tt.goos)
			if len(got) != len(tt.wantSubs) {
				t.Fatalf("warnings = %q, want %d entries", got, len(tt.wantSubs))
			}
			for i, sub := range tt.wantSubs {
				if !strings.Contains(got[i], sub) {
					t.Errorf("warning[%d] = %q, want substring %q", i, got[i], sub)
				}
			}
		})
	}
}

func TestLoad_ExposesWarnings(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
groups:
  - name: g
    max_runners: 1
    reserve: {memory: 1G}
`))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if len(cfg.Warnings) == 0 {
		t.Fatal("expected a warning for reserve without capacity")
	}
}
