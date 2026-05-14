# Spec 07 — Configuration

## Overview

Single YAML config file + environment variables for secrets. Full validation at startup with clear error messages.

---

## Complete config schema

```yaml
# ─── GitHub ──────────────────────────────────────────────────────────
# Authentication is handled by 'ghr login' (see spec 08-auth.md).
# URL resolution: credentials file URL > config.yaml URL.
# If --token is used without 'ghr login', config.yaml URL is required.
github:
  url: "https://github.com/my-org"          # optional, fallback when not in credentials file
  runner_group: "default"                   # optional, default "default"

# ─── Runner binary ───────────────────────────────────────────────────
runner:
  version: "latest"                         # "latest" or pinned e.g. "2.330.0"
  cache_dir: "/var/lib/ghr/cache"           # where to store downloaded runner archives
  workdir_base: "/var/lib/ghr/runners"      # base dir for runner workdirs

# ─── Groups ──────────────────────────────────────────────────────────
groups:
  - name: "qa-runners"                     # required, unique, becomes scale set name
    max_runners: 10                         # required, >= 1
    min_runners: 2                          # optional, default 0, >= 0, <= max_runners
    labels:                                 # optional, additional labels
      - "qa"
      - "macos"
    runner_group: "custom-group"            # optional, overrides global runner_group
    version: "2.320.0"                      # optional, overrides global runner.version
    health:                                 # optional, per-group health overrides
      runner_timeout: "30m"

  - name: "backend-runners"
    max_runners: 6
    labels: ["backend", "macos"]

  - name: "frontend-runners"
    max_runners: 2
    min_runners: 0
    labels: ["frontend", "macos"]

# ─── Health monitor ──────────────────────────────────────────────────
health:
  enabled: true                             # default true
  check_interval: "30s"                     # default 30s
  runner_timeout: "2h"                      # default 2h, global
  idle_timeout: "0"                         # default 0 (disabled)
  divergence_timeout: "5m"                  # default 5m
  max_consecutive_failures: 5               # default 5
  failure_cooldown: "1m"                    # default 1m
  min_disk_space: "1GB"                     # default 1GB

# ─── Logging ─────────────────────────────────────────────────────────
logging:
  level: "info"                             # debug, info, warn, error
  format: "text"                            # text (console) + json (files), or json (both)
  dir: "/var/log/ghr"                       # log directory
  retention_days: 30                        # 0 = keep forever, default 30
  runner_output: true                       # capture runner stdout/stderr, default true

# ─── Notifications ───────────────────────────────────────────────────
# Secrets (webhook URLs, tokens) must NOT be in config.yaml.
# Use environment variables or .env file (see "Environment variables" below).
notifications:
  discord:
    enabled: false
    # webhook_url via GHR_DISCORD_WEBHOOK_URL env var
    events: []                              # empty = all events
    username: "ghr"
    mentions:
      error: ""                             # Discord role/user mention string
      critical: ""

# ─── Monitoring ──────────────────────────────────────────────────────
# Secrets (base URL, push tokens) must NOT be in config.yaml.
# Use environment variables or .env file (see "Environment variables" below).
monitoring:
  uptime_kuma:
    enabled: false
    # base_url via GHR_UPTIME_KUMA_URL env var
    # push tokens via GHR_UPTIME_KUMA_DAEMON_TOKEN / GHR_UPTIME_KUMA_TOKEN_{GROUP} env vars
    degraded_threshold: 0.5                 # 0.0-1.0
    report_health_as_ping: true

# ─── Daemon ──────────────────────────────────────────────────────────
daemon:
  state_dir: "/var/lib/ghr/state"           # PID file, state file
  shutdown_timeout: "30s"                   # max wait for graceful shutdown
```

---

## Validation rules

### Required fields
- Credentials must exist (via `ghr login` or env var `GITHUB_TOKEN`)
- At least one group
- Each group must have a unique, non-empty `name`
- Each group must have `max_runners >= 1`

### Consistency checks
- `min_runners <= max_runners` per group
- `min_runners >= 0`

### Path validation
- `runner.cache_dir`, `runner.workdir_base`, `logging.dir`, `daemon.state_dir` are created at startup if they don't exist
- Error if the parent directory doesn't exist or isn't writable

### Duration parsing
- All duration fields accept Go duration strings: `30s`, `5m`, `2h`, `1h30m`
- Validation: `check_interval >= 5s`, `runner_timeout >= 1m`, `shutdown_timeout >= 5s`

### Byte size parsing
- `min_disk_space` accepts human-readable byte sizes: `"1GB"`, `"500MB"`, `"2TB"`
- Parsed using a simple parser that supports KB, MB, GB, TB suffixes (case-insensitive)
- Numeric-only values are interpreted as bytes

### Labels
- Group name is always added as a label automatically (like the scale set SDK does)
- Additional labels from the `labels` field are appended
- Duplicate labels are deduplicated
- Empty strings in labels array are rejected

---

## Environment variables

Secrets must NEVER be in the config file. Supported env vars:

| Env var | Purpose |
|---|---|
| `GITHUB_TOKEN` | PAT override (bypasses credentials file) |
| `GHR_CREDENTIALS_FILE` | Override credentials file path |
| `GHR_DISCORD_WEBHOOK_URL` | Discord webhook URL |
| `GHR_UPTIME_KUMA_URL` | Uptime Kuma base URL |
| `GHR_UPTIME_KUMA_DAEMON_TOKEN` | Uptime Kuma daemon push token |
| `GHR_UPTIME_KUMA_TOKEN_{GROUP}` | Uptime Kuma per-group push token (group name uppercase, hyphens→underscores) |

The `.env` file is loaded via `godotenv.Load()` if present.

Auth resolution order: `--token` flag → `GITHUB_TOKEN` env → credentials file → `.env` file → error.
See spec 08-auth.md for details.

---

## Defaults

Fields with sensible defaults that don't need to be in the config:

```yaml
github.runner_group: "default"
runner.version: "latest"
runner.cache_dir: "/var/lib/ghr/cache"     # or ~/.local/share/ghr/cache if not root
runner.workdir_base: "/var/lib/ghr/runners" # or ~/.local/share/ghr/runners if not root
health.enabled: true
health.check_interval: "30s"
health.runner_timeout: "2h"
health.divergence_timeout: "5m"
health.max_consecutive_failures: 5
health.failure_cooldown: "1m"
health.min_disk_space: "1GB"
logging.level: "info"
logging.format: "text"
logging.dir: "/var/log/ghr"                # or ~/.local/share/ghr/logs if not root
logging.retention_days: 30
logging.runner_output: true
daemon.state_dir: "/var/lib/ghr/state"     # or ~/.local/state/ghr if not root
daemon.shutdown_timeout: "30s"
```

### Root vs non-root path resolution

If running as root, use `/var/lib/ghr/` and `/var/log/ghr/`.
If running as a normal user, use `~/.local/share/ghr/` and `~/.local/share/ghr/logs/`.
Auto-detected at startup unless explicitly set in config.

---

## Minimal config example

The smallest valid config (after `ghr login`):

```yaml
groups:
  - name: "runners"
    max_runners: 5
```

That's it. Auth + GitHub URL come from `ghr login`. Everything else has defaults: latest runner version, default paths, health enabled, no notifications.

---

## Config loading

```go
func Load(path string) (*Config, error) {
    // 1. Load .env if present
    godotenv.Load()

    // 2. Read YAML
    bytes, _ := os.ReadFile(path)
    cfg := &Config{}
    yaml.Unmarshal(bytes, cfg)

    // 3. Apply defaults (root vs non-root aware)
    applyDefaults(cfg)

    // 4. Resolve env var references (uptime kuma tokens, etc.)
    resolveEnvVars(cfg)

    // 5. Validate
    if err := validate(cfg); err != nil {
        return nil, err
    }

    // NOTE: auth is NOT resolved here. It's handled separately by
    // the auth package (see spec 08-auth.md). The daemon loads
    // credentials via auth.Load() at startup.

    return cfg, nil
}
```
