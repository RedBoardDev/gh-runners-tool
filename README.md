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
