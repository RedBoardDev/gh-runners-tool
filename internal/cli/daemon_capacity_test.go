package cli

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/cgroup"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/config"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func capacityConfig() *config.Config {
	return &config.Config{
		Capacity: &config.ResourceAmount{Memory: "48G", CPUs: 40},
		Groups: []config.GroupConfig{
			{
				Name: "heavy", MaxRunners: 4, MinRunners: 1, Priority: 50,
				Reserve: config.ResourceAmount{Memory: "9G", CPUs: 6},
			},
			{Name: "light", MaxRunners: 10},
		},
	}
}

func TestBuildCapacity(t *testing.T) {
	t.Run("absent capacity builds nothing", func(t *testing.T) {
		alloc, err := buildCapacity(&config.Config{Groups: []config.GroupConfig{{Name: "g"}}}, quietLogger())
		if err != nil || alloc != nil {
			t.Fatalf("buildCapacity() = %v, %v; want nil, nil", alloc, err)
		}
	})

	t.Run("configured capacity reaches the allocator", func(t *testing.T) {
		alloc, err := buildCapacity(capacityConfig(), quietLogger())
		if err != nil {
			t.Fatalf("buildCapacity() = %v", err)
		}

		st := alloc.Status()
		if st.Total.MemoryBytes != 48<<30 || st.Total.CPUMilli != 40_000 {
			t.Errorf("total = %+v, want 48GiB / 40 cpus", st.Total)
		}
		heavy := st.Groups["heavy"]
		if heavy.Priority != 50 || heavy.Reserve.MemoryBytes != 9<<30 || heavy.Reserve.CPUMilli != 6000 {
			t.Errorf("heavy = %+v", heavy)
		}
		if light := st.Groups["light"]; !light.Reserve.IsZero() || light.Priority != 0 {
			t.Errorf("light = %+v, want zero defaults", light)
		}
	})

	t.Run("an unparseable amount is an error, not a silent zero", func(t *testing.T) {
		cfg := capacityConfig()
		cfg.Groups[0].Reserve.Memory = "lots"
		if _, err := buildCapacity(cfg, quietLogger()); err == nil {
			t.Fatal("buildCapacity() succeeded with an invalid reserve")
		}
	})
}

func TestBuildHostLimits(t *testing.T) {
	t.Run("without capacity no option is installed", func(t *testing.T) {
		host, err := buildHostLimits(&config.Config{Groups: []config.GroupConfig{{Name: "g"}}}, quietLogger())
		if err != nil {
			t.Fatalf("buildHostLimits() = %v", err)
		}
		if host.budget != nil || len(host.controller)+len(host.health)+len(host.api) != 0 {
			t.Fatalf("host = %+v, want no capacity wiring", host)
		}
	})

	t.Run("with capacity every consumer is wired to the same budget", func(t *testing.T) {
		host, err := buildHostLimits(capacityConfig(), quietLogger())
		if err != nil {
			t.Fatalf("buildHostLimits() = %v", err)
		}
		if host.budget == nil || len(host.controller) != 1 || len(host.health) != 1 || len(host.api) != 1 {
			t.Fatalf("host = %+v, want budget plus one option each", host)
		}
	})

	t.Run("config warnings are logged", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))
		cfg := &config.Config{
			Groups:   []config.GroupConfig{{Name: "g"}},
			Warnings: []string{"groups[0] (g): reserve is ignored because capacity is not set"},
		}

		if _, err := buildHostLimits(cfg, logger); err != nil {
			t.Fatalf("buildHostLimits() = %v", err)
		}
		if !strings.Contains(buf.String(), "reserve is ignored") {
			t.Fatalf("warning not logged: %q", buf.String())
		}
	})
}

func TestCgroupLimits(t *testing.T) {
	cfg := &config.Config{Groups: []config.GroupConfig{
		{Name: "limited", Resources: config.ResourcesConfig{MemoryMax: "12G", MemoryHigh: "11G", CPUWeight: 100}},
		{Name: "weight-only", Resources: config.ResourcesConfig{CPUWeight: 50}},
		{Name: "plain"},
	}}

	limits, err := cgroupLimits(cfg)
	if err != nil {
		t.Fatalf("cgroupLimits() = %v", err)
	}

	if len(limits) != 2 {
		t.Fatalf("limits = %v, want only the groups that set resources", limits)
	}
	want := cgroup.Limits{MemoryMax: 12 << 30, MemoryHigh: 11 << 30, CPUWeight: 100}
	if limits["limited"] != want {
		t.Errorf("limited = %+v, want %+v", limits["limited"], want)
	}
	if limits["weight-only"] != (cgroup.Limits{CPUWeight: 50}) {
		t.Errorf("weight-only = %+v", limits["weight-only"])
	}

	t.Run("invalid size is an error", func(t *testing.T) {
		bad := &config.Config{Groups: []config.GroupConfig{{Name: "g", Resources: config.ResourcesConfig{MemoryMax: "plenty"}}}}
		if _, err := cgroupLimits(bad); err == nil {
			t.Fatal("cgroupLimits() succeeded with an invalid memory_max")
		}
	})
}

func TestBuildCgroupOptions_IgnoredWhereCgroupsAreUnsupported(t *testing.T) {
	if cgroup.Supported {
		t.Skip("on Linux this would try to reconfigure the test runner's own cgroup")
	}
	cfg := &config.Config{Groups: []config.GroupConfig{{Name: "g", Resources: config.ResourcesConfig{MemoryMax: "1G"}}}}

	opts, err := buildCgroupOptions(cfg, quietLogger())

	if err != nil || len(opts) != 0 {
		t.Fatalf("buildCgroupOptions() = %v, %v; want no options and no error off Linux", opts, err)
	}
}
