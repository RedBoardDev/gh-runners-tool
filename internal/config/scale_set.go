package config

import "fmt"

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
	if !labelPattern.MatchString(cfg.HostID) {
		return []error{fmt.Errorf("host_id %q must match %s", cfg.HostID, labelPattern.String())}
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
