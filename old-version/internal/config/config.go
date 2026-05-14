package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

type GitHubScope string

const (
	ScopeOrg  GitHubScope = "org"
	ScopeRepo GitHubScope = "repo"
)

type GitHubConfig struct {
	Scope GitHubScope `yaml:"scope"`
	Owner string      `yaml:"owner"`
	Repo  string      `yaml:"repo,omitempty"`
}

type RunnerDefaults struct {
	WorkdirBase string `yaml:"workdir_base"`
	CacheDir    string `yaml:"cache_dir"`
	Version     string `yaml:"version"` // e.g. "2.319.1" or "latest"
}

type GroupSpec struct {
	Name        string   `yaml:"name"`
	Count       int      `yaml:"count"`
	Ephemeral   bool     `yaml:"ephemeral"`
	Labels      []string `yaml:"labels"`
	WorkdirBase string   `yaml:"workdir_base,omitempty"`
	Version     string   `yaml:"version,omitempty"`
}

type Config struct {
	GitHub   GitHubConfig   `yaml:"github"`
	Defaults RunnerDefaults `yaml:"defaults"`
	Groups   []GroupSpec    `yaml:"groups"`
}

// Load loads configuration from YAML and .env (env is mandatory for tokens).
func Load(path string) (*Config, error) {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading .env: %w", err)
	}

	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(bytes, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := validate(cfg); err != nil {
		return nil, err
	}

	if cfg.Defaults.WorkdirBase == "" {
		cfg.Defaults.WorkdirBase = "/var/lib/ghr/groups"
	}
	if cfg.Defaults.CacheDir == "" {
		cfg.Defaults.CacheDir = "/var/lib/ghr/cache"
	}
	if cfg.Defaults.Version == "" {
		cfg.Defaults.Version = "latest"
	}

	for i := range cfg.Groups {
		if cfg.Groups[i].WorkdirBase == "" {
			cfg.Groups[i].WorkdirBase = filepath.Join(cfg.Defaults.WorkdirBase, cfg.Groups[i].Name)
		}
		if cfg.Groups[i].Version == "" {
			cfg.Groups[i].Version = cfg.Defaults.Version
		}
	}

	return cfg, nil
}

func validate(cfg *Config) error {
	if cfg.GitHub.Scope != ScopeOrg && cfg.GitHub.Scope != ScopeRepo {
		return fmt.Errorf("github.scope must be 'org' or 'repo'")
	}
	if cfg.GitHub.Owner == "" {
		return fmt.Errorf("github.owner is required")
	}
	if cfg.GitHub.Scope == ScopeRepo && cfg.GitHub.Repo == "" {
		return fmt.Errorf("github.repo is required when scope=repo")
	}
	if len(cfg.Groups) == 0 {
		return fmt.Errorf("at least one group is required")
	}
	for _, g := range cfg.Groups {
		if g.Name == "" {
			return fmt.Errorf("group.name is required")
		}
		if g.Count < 0 {
			return fmt.Errorf("group.count must be >= 0")
		}
	}
	return nil
}
