package config

import "fmt"

func warnings(cfg *Config, goos string) []string {
	var out []string
	for i := range cfg.Groups {
		g := &cfg.Groups[i]
		prefix := fmt.Sprintf("groups[%d] (%s)", i, g.Name)
		if cfg.Capacity == nil && !g.Reserve.IsZero() {
			out = append(out, prefix+": reserve is ignored because capacity is not set")
		}
		if cfg.Capacity == nil && g.Priority != 0 {
			out = append(out, prefix+": priority is ignored because capacity is not set")
		}
		if goos != "linux" && !g.Resources.IsZero() {
			out = append(out, fmt.Sprintf("%s: resources are ignored on %s (per-runner cgroup limits need Linux with cgroup v2)", prefix, goos))
		}
	}
	return out
}
