# ghr - GitHub Actions Runner Controller for macOS

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![GitHub Actions](https://img.shields.io/badge/GitHub%20Actions-Runner%20Controller-2088FF?logo=githubactions&logoColor=white)](https://github.com/features/actions)
[![macOS](https://img.shields.io/badge/macOS-Apple%20Silicon%20%7C%20Intel-000000?logo=apple&logoColor=white)](https://www.apple.com/macos/)

## Overview

**ghr** is a self-hosted GitHub Actions runner controller built on the official [`actions/scaleset`](https://github.com/actions/scaleset) Go SDK. It manages ephemeral runners via JIT configs, scale sets, and long-polling - targeting macOS (Apple Silicon and Intel).

Define runner groups with min/max scaling in a YAML config, and ghr handles binary downloads, runner registration, process lifecycle, health monitoring, and graceful shutdown. It integrates with macOS `launchd` for service management and supports Discord/webhook notifications and Uptime Kuma push monitoring.

### Key Features

- **Scale Set orchestration** - Runner groups with configurable min/max scaling via the official GitHub SDK
- **Ephemeral JIT runners** - Provisioned on-demand with just-in-time configs, cleaned up after each job
- **macOS native** - First-class `launchd` integration (`ghr start/stop/restart/status`)
- **YAML configuration** - Single config file with environment variables for secrets
- **Health monitoring** - Detection of stuck runners, resource issues, and connectivity problems
- **Notifications** - Discord and webhook alerts for runner events
- **Uptime Kuma** - Push-based monitoring integration
- **Structured logging** - `slog`-based with file rotation and per-runner log files
- **Host capacity budget** - Optional memory/CPU budget shared by all groups, with group priorities
- **Per-runner cgroups (Linux)** - Optional memory and CPU limits so one runaway job cannot take the daemon or the host down

## Getting Started

### Prerequisites

- **Go 1.25+** (required by the `actions/scaleset` SDK)
- **macOS** (Apple Silicon or Intel)
- A GitHub organization or repository with self-hosted runner access
- A GitHub PAT or App credentials with runner management permissions

### Build

```bash
go build -o ghr ./cmd/ghr
```

### Configuration

Create a `config.yaml`:

```yaml
github:
  url: "https://github.com/my-org"
  runner_group: "default"

runner:
  version: "latest"
  cache_dir: "/var/lib/ghr/cache"
  workdir_base: "/var/lib/ghr/runners"

groups:
  - name: "ci-runners"
    max_runners: 10
    min_runners: 2
    labels: ["ci", "macos"]

  - name: "deploy-runners"
    max_runners: 2
    labels: ["deploy", "macos"]
```

Authentication is handled via `ghr login`. Tokens are never stored in the config file - use environment variables or the credentials store.

### Usage

```bash
# Authenticate with GitHub
ghr login

# Start as a launchd service (daemon)
ghr start --config config.yaml

# Run in foreground (debug mode)
ghr run --config config.yaml

# Check status
ghr status

# Restart after config changes
ghr restart

# Stop the daemon
ghr stop

# Emergency reset (kill all runners, clean workdirs)
ghr purge
```

### Host capacity budget

Every group scales independently, so without a budget the sum of all groups can overcommit the host. Add an optional `capacity` block and give each group a `reserve` (what one runner of that group holds while it exists, idle or busy):

```yaml
capacity:            # optional; absent means groups are only bounded by max_runners
  memory: 48G
  cpus: 40           # may be fractional

groups:
  - name: "heavy"
    max_runners: 4
    min_runners: 0
    priority: 50     # optional, default 0, higher is served first
    reserve:         # optional, default zero
      memory: 9G
      cpus: 6
  - name: "light"
    max_runners: 10
    reserve:
      memory: 1G
      cpus: 1
```

- A runner is provisioned only if its group's reserve fits in what is left of the budget, on both memory and CPUs. The reservation is taken before the JIT config is generated and released when the runner is torn down (job completed, idle kill, health kill, failed start, shutdown). Demand that does not fit is deferred, not dropped.
- Priority is strict: while a higher-priority group has deferred demand, no lower-priority group may take capacity. Groups with the same priority are served in the order their deferred demand started. A group with a zero `reserve` is never blocked.
- `min_runners * reserve` of every group is protected: a group above its minimum cannot take capacity another group still needs for its own minimum. Validation rejects a configuration whose minimums do not fit in `capacity`.
- When capacity is released, waiting groups are re-evaluated immediately in priority order instead of at their next poll.
- With a budget configured, idle runners above `max(min_runners, demand - busy)` are torn down when queued jobs disappear, so they stop holding capacity. Without a budget nothing is torn down.
- A dimension left out of `capacity` is not limited. `reserve` and `priority` without `capacity` are ignored with a warning.
- Deferred demand is reported as an info-level "waiting for capacity" health issue, never as a degraded group. `ghr status` (and the JSON status API) shows the budget total, used and free, and per group the reserved and waiting runner counts. Waiting state changes are logged once per change.

Sizes accept `K`, `M`, `G`, `T` and `KiB`, `MiB`, `GiB`, `TiB` (powers of 1024, like systemd and Docker) as well as `KB`, `MB`, `GB`, `TB` (powers of 1000).

### Per-runner cgroup limits (Linux, cgroup v2)

```yaml
groups:
  - name: "heavy"
    max_runners: 4
    resources:
      memory_max: 12G    # memory.max
      memory_high: 11G   # optional, memory.high
      cpu_weight: 100    # optional, cpu.weight (1..10000)
```

Each runner is started directly inside its own cgroup (`clone3` with `CLONE_INTO_CGROUP`, Linux 5.7+) with `memory.oom.group=1`, so an out-of-memory kill takes down that runner's whole job tree and nothing else. When the runner is torn down, `cgroup.kill` removes any process a job left behind and the cgroup is deleted. Cgroups left by a crashed daemon are swept at startup.

When any group sets `resources`, ghr requires cgroup v2 and a delegated, writable cgroup, and refuses to start otherwise: it never silently skips enforcement. At startup it moves itself into a `daemon` leaf of its own cgroup (cgroup v2 forbids a cgroup that has controllers enabled for its children from holding processes itself) and enables the controllers it needs (`memory` always, `cpu` only when a group sets `cpu_weight`). `ghr doctor` verifies this against the running daemon. On other platforms `resources` is ignored with a warning, so the same config works on macOS.

Sample systemd unit:

```ini
[Unit]
Description=ghr GitHub Actions runner controller
After=network-online.target docker.service
Wants=network-online.target

[Service]
User=ghr
ExecStart=/usr/local/bin/ghr run --config /etc/ghr/config.yaml
Restart=on-failure
RestartSec=5
TimeoutStopSec=60
Delegate=yes
OOMPolicy=continue

[Install]
WantedBy=multi-user.target
```

- `Delegate=yes` hands the unit's cgroup subtree to ghr.
- Keep the default `KillMode=control-group`. With `process` or `mixed`, runner cgroups can survive a stop, and the next start then fails with `EBUSY` on cgroup v2's no-internal-processes rule.
- On systemd 254 or newer, `DelegateSubgroup=daemon` is an option: systemd then places the service in the `daemon` leaf itself.
- ghr refuses to start on kernels older than 5.7 when `resources` is set (`CLONE_INTO_CGROUP` is required).
- `OOMPolicy=continue` is required: systemd's OOM policy reacts to OOM kills anywhere in the unit's subtree, so with the default `stop` a single runner hitting its limit would still stop the whole daemon and every job on the host.
- Containers started by jobs (`container:` and `services:`) are created by the Docker daemon outside the runner cgroup and are not limited by it. Include their expected footprint in the group's `reserve` instead.
- `cgroup.kill` needs Linux 5.14+; on older kernels ghr falls back to killing the processes listed in the cgroup.

### Run Tests

```bash
go test ./...              # all tests
go test -race ./...        # with race detector
go vet ./...               # static analysis
golangci-lint run          # lint (if installed)
```

## Repository Structure

```
ghr/
├── cmd/ghr/main.go             # Entrypoint
├── internal/
│   ├── cli/                    # Cobra commands (start/stop/run/status/purge/login/...)
│   ├── auth/                   # Credentials, JWT signing, installation tokens, breaker
│   ├── config/                 # YAML + env loading, validation, defaults
│   ├── controller/             # Scale-set orchestration and per-group scaler
│   ├── capacity/               # Host-wide memory/CPU budget with group priorities
│   ├── cgroup/                 # Per-runner cgroup v2 limits (Linux)
│   ├── runner/                 # Binary download (with SHA-256 verify) and process lifecycle
│   ├── github/                 # scaleset SDK adapter
│   ├── health/                 # Health monitor and check functions
│   ├── notification/           # Discord + webhook providers with filtering
│   ├── monitoring/             # Uptime Kuma push reporter
│   ├── api/                    # Unix-socket JSON API exposing status/health
│   ├── launchd/                # macOS service install/uninstall via bootstrap/bootout
│   ├── state/                  # Centralized daemon-state file paths (pid/sock/state)
│   ├── model/                  # Shared data structs (no logic)
│   └── logging/                # Structured logging, rotation, tagged runner output
├── go.mod
└── go.sum
```

## Key Dependencies

| Package | Purpose |
|---------|---------|
| [`actions/scaleset`](https://github.com/actions/scaleset) | Official GitHub Scale Set API + listener |
| [`spf13/cobra`](https://github.com/spf13/cobra) | CLI framework |
| [`oklog/run`](https://github.com/oklog/run) | Goroutine lifecycle management |
| [`joho/godotenv`](https://github.com/joho/godotenv) | `.env` file loading |
| `gopkg.in/yaml.v3` | YAML config parsing |
| `log/slog` (stdlib) | Structured logging |

## Reporting Issues

[GitHub Issues](https://github.com/RedBoardDev/gh-runners-tool/issues)

## License

Proprietary. All rights reserved.

## Contact

- GitHub: [@RedBoardDev](https://github.com/RedBoardDev)
