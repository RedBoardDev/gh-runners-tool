package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"time"
)

var labelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// unsafeWorkdirBase enumerates directories ghr must refuse to claim as its
// runner workdir root. Substring matches by tools like `pgrep -f` would
// otherwise sweep up arbitrary user processes.
var unsafeWorkdirBase = map[string]struct{}{
	"/":     {},
	"/tmp":  {},
	"/var":  {},
	"/usr":  {},
	"/etc":  {},
	"/home": {},
	"/root": {},
	"/opt":  {},
	"/bin":  {},
	"/sbin": {},
	"/dev":  {},
}

func validate(cfg *Config) error {
	var errs []error

	if len(cfg.Groups) == 0 {
		errs = append(errs, errors.New("at least one group is required"))
	}

	seenNames := make(map[string]bool, len(cfg.Groups))

	for i, g := range cfg.Groups {
		prefix := fmt.Sprintf("groups[%d]", i)

		switch {
		case g.Name == "":
			errs = append(errs, fmt.Errorf("%s: name is required", prefix))
		case seenNames[g.Name]:
			errs = append(errs, fmt.Errorf("%s: duplicate group name %q", prefix, g.Name))
		default:
			seenNames[g.Name] = true
		}

		if g.MaxRunners < 1 {
			errs = append(errs, fmt.Errorf("%s (%s): max_runners must be >= 1", prefix, g.Name))
		}

		if g.MinRunners < 0 {
			errs = append(errs, fmt.Errorf("%s (%s): min_runners must be >= 0", prefix, g.Name))
		}

		if g.MinRunners > g.MaxRunners {
			errs = append(errs, fmt.Errorf("%s (%s): min_runners (%d) must be <= max_runners (%d)", prefix, g.Name, g.MinRunners, g.MaxRunners))
		}

		for j, label := range g.Labels {
			switch {
			case label == "":
				errs = append(errs, fmt.Errorf("%s (%s): labels[%d] must not be empty", prefix, g.Name, j))
			case !labelPattern.MatchString(label):
				errs = append(errs, fmt.Errorf("%s (%s): labels[%d] %q must match %s", prefix, g.Name, j, label, labelPattern.String()))
			}
		}
	}

	if cfg.Runner.WorkdirBase != "" {
		clean := filepath.Clean(cfg.Runner.WorkdirBase)
		switch {
		case !filepath.IsAbs(cfg.Runner.WorkdirBase):
			errs = append(errs, fmt.Errorf("runner.workdir_base must be absolute, got %q", cfg.Runner.WorkdirBase))
		default:
			if _, banned := unsafeWorkdirBase[clean]; banned {
				errs = append(errs, fmt.Errorf("runner.workdir_base must not be a top-level system directory, got %q", cfg.Runner.WorkdirBase))
			} else if len(clean) < 8 {
				errs = append(errs, fmt.Errorf("runner.workdir_base %q is too short (orphan-process matching would be unsafe)", cfg.Runner.WorkdirBase))
			}
		}
	}

	if cfg.Health.CheckInterval.Duration > 0 && cfg.Health.CheckInterval.Duration < 5*time.Second {
		errs = append(errs, fmt.Errorf("health.check_interval must be >= 5s, got %s", cfg.Health.CheckInterval.Duration))
	}
	if cfg.Health.RunnerTimeout.Duration > 0 && cfg.Health.RunnerTimeout.Duration < 1*time.Minute {
		errs = append(errs, fmt.Errorf("health.runner_timeout must be >= 1m, got %s", cfg.Health.RunnerTimeout.Duration))
	}
	if cfg.Daemon.ShutdownTimeout.Duration > 0 && cfg.Daemon.ShutdownTimeout.Duration < 5*time.Second {
		errs = append(errs, fmt.Errorf("daemon.shutdown_timeout must be >= 5s, got %s", cfg.Daemon.ShutdownTimeout.Duration))
	}

	if cfg.Health.MinDiskSpace != "" {
		if _, parseErr := ParseByteSize(cfg.Health.MinDiskSpace); parseErr != nil {
			errs = append(errs, fmt.Errorf("health.min_disk_space: %w", parseErr))
		}
	}

	switch cfg.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("logging.level must be one of: debug, info, warn, error; got %q", cfg.Logging.Level))
	}

	switch cfg.Logging.Format {
	case "text", "json":
	default:
		errs = append(errs, fmt.Errorf("logging.format must be one of: text, json; got %q", cfg.Logging.Format))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func isHealthZero(h HealthConfig) bool {
	return !h.Enabled &&
		h.CheckInterval.Duration == 0 &&
		h.RunnerTimeout.Duration == 0 &&
		h.IdleTimeout.Duration == 0 &&
		h.DivergenceTimeout.Duration == 0 &&
		h.MaxConsecutiveFailures == 0 &&
		h.FailureCooldown.Duration == 0 &&
		h.MinDiskSpace == ""
}
