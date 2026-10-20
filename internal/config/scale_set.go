package config

import (
	"fmt"
	"regexp"
	"strings"
)

// A dash in host_id makes <group>-<host_id> ambiguous across hosts: "ci-x" on
// host "y" and "ci" on host "x-y" would share the scale set "ci-x-y".
var hostIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._]{0,63}$`)

func ScaleSetName(group, hostID string) string {
	if hostID == "" {
		return group
	}
	return group + "-" + hostID
}

func validateHostID(cfg *Config) []error {
	if cfg.HostID == "" {
		return nil
	}
	if !hostIDPattern.MatchString(cfg.HostID) {
		return []error{fmt.Errorf("host_id %q must match %s", cfg.HostID, hostIDPattern.String())}
	}

	var errs []error
	for i := range cfg.Groups {
		g := &cfg.Groups[i]
		if name := ScaleSetName(g.Name, cfg.HostID); !labelPattern.MatchString(name) {
			errs = append(errs, fmt.Errorf("groups[%d] (%s): scale set name %q must match %s", i, g.Name, name, labelPattern.String()))
		}
	}
	return errs
}

func validateScaleSetNames(cfg *Config) []error {
	var errs []error
	seen := make(map[string]int, len(cfg.Groups))
	for i := range cfg.Groups {
		g := &cfg.Groups[i]
		name := ScaleSetName(g.Name, cfg.HostID)
		first, ok := seen[strings.ToLower(name)]
		if !ok {
			seen[strings.ToLower(name)] = i
			continue
		}
		if cfg.Groups[first].Name != g.Name {
			errs = append(errs, fmt.Errorf("groups[%d] (%s): scale set name %q collides with groups[%d] (%s) ignoring case",
				i, g.Name, name, first, cfg.Groups[first].Name))
		}
	}
	return errs
}
