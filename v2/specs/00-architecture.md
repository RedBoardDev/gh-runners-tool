# Spec 00 — Architecture & Design Principles

## Overview

ghr v2 follows **idiomatic Go** patterns: package-by-feature, consumer-side interfaces, explicit dependency injection, and `oklog/run` for goroutine lifecycle management. No DDD layers, no central `ports.go`, no over-abstraction.

The architecture is informed by how real-world Go projects of similar scale are structured (Prometheus, Caddy, actions-runner-controller) and by the Go community's consensus against importing Java/C# patterns.

---

## Principles

### 1. Package-by-feature, not package-by-layer

Each package owns a feature. No `domain/`, `app/`, `infra/` layers. Navigation is horizontal (one level under `internal/`), not vertical (3 levels deep).

### 2. Accept interfaces, return structs

Interfaces are defined where they are **consumed**, not where they are implemented. Each package defines the minimal interface it needs from its dependencies. No central `ports.go`.

```go
// internal/controller/controller.go
// The controller defines what it needs from the GitHub client.
type scaleSetClient interface {
    CreateScaleSet(ctx context.Context, opts CreateOpts) (int, error)
    DeleteScaleSet(ctx context.Context, id int) error
    GenerateJITConfig(ctx context.Context, scaleSetID int, name string) (string, error)
}
```

### 3. Structs with exported fields, not getter interfaces

```go
// YES — Go idiomatic
type RunnerProcess struct {
    Name      string
    Group     string
    WorkDir   string
    PID       int
    StartedAt time.Time
    Cmd       *exec.Cmd
}

// NO — Java-style getter interface
type RunnerProcess interface {
    Name() string
    Group() string
    PID() int
}
```

### 4. Shared types in a small `model` package

Types used by multiple packages (Event, Group, etc.) live in `internal/model/`. This package contains ONLY data structs and enums. No interfaces. No logic. Under 100 LOC.

### 5. Goroutine lifecycle via `oklog/run`

The daemon top-level uses `oklog/run.Group` for managing concurrent actors (controller, health monitor, signal handler). When any actor exits, all others are interrupted. Clean, deterministic shutdown. Used in production by Prometheus and Thanos.

### 6. No hardcoded values

Group names, labels, paths — everything from config. The code operates on abstractions from config, never on concrete values.

---

## Package structure

```
gh-runners-tool/v2/
├── cmd/ghr/
│   └── main.go                          # wiring, DI, CLI bootstrap
│
├── internal/
│   ├── model/                           # shared data types ONLY (no interfaces, no logic)
│   │   ├── event.go                     # Event, EventType, EventLevel
│   │   ├── group.go                     # Group, RunnerInstance
│   │   └── health.go                    # HealthStatus, HealthIssue
│   │
│   ├── cli/                             # Cobra commands (thin, delegates immediately)
│   │   ├── root.go
│   │   ├── start.go
│   │   ├── stop.go
│   │   ├── restart.go
│   │   ├── run.go
│   │   ├── status.go
│   │   ├── purge.go
│   │   ├── login.go                     # interactive auth wizard
│   │   └── auth.go                      # ghr auth status
│   │
│   ├── auth/                            # credentials: login wizard, load, save, validate
│   │   └── auth.go
│
│   ├── api/                             # Unix socket JSON API for IPC
│   │   └── server.go                    # serves /status and /health on {state_dir}/ghr.sock
│   │
│   ├── config/                          # YAML + env loading, validation
│   │   ├── loader.go
│   │   └── types.go
│   │
│   ├── controller/                      # orchestration: manages N groups + scaler
│   │   ├── controller.go                # GroupController, starts/stops groups
│   │   └── scaler.go                    # MacOSScaler (implements listener.Scaler)
│   │
│   ├── runner/                          # runner binary + process management
│   │   ├── binary.go                    # download, cache, version resolution
│   │   └── process.go                   # exec.Cmd wrapper, PID, start/stop/cleanup
│   │
│   ├── github/                          # Scale Set API adapter
│   │   └── client.go                    # wraps scaleset.Client for our needs
│   │
│   ├── health/                          # health monitoring
│   │   └── monitor.go                   # periodic checks, issue detection
│   │
│   ├── notification/                    # notification fan-out + providers
│   │   ├── service.go                   # Service (fan-out to providers)
│   │   ├── discord.go                   # Discord webhook provider
│   │   └── webhook.go                   # Generic HTTP webhook provider
│   │
│   ├── monitoring/                      # push-based monitoring backends
│   │   └── uptimekuma.go               # Uptime Kuma push monitor
│   │
│   ├── launchd/                         # macOS service management
│   │   └── service.go                   # plist generation, launchctl
│   │
│   └── logging/                         # slog-based structured logging
│       └── logger.go                    # multi-writer, rotation, per-runner files
│
├── config.example.yaml
├── env.example
├── go.mod
└── go.sum
```

**Total: 14 packages under `internal/`, each owning one feature.** No nesting beyond one level.

---

## Shared types (`internal/model/`)

This package is deliberately small. Structs only. No interfaces. No methods with logic.

```go
package model

import "time"

// ─── Group ──────────────────────────────────────────────────────────

type Group struct {
    Name        string
    MaxRunners  int
    MinRunners  int
    Labels      []string
    RunnerGroup string
}

// ─── Runner ─────────────────────────────────────────────────────────

type RunnerInstance struct {
    ID      string   // random hex (e.g., "a3f2b7c1")
    Name    string   // full name (e.g., "mygroup-a3f2b7c1")
    Group   string
    WorkDir string
    Version string
}

type RunnerSnapshot struct {
    Name      string
    Group     string
    State     string  // "idle", "busy"
    PID       int
    StartedAt time.Time
    JobName   string  // empty if idle
    JobID     string
}

// ─── Event ──────────────────────────────────────────────────────────

type EventLevel string
const (
    LevelInfo     EventLevel = "info"
    LevelWarning  EventLevel = "warning"
    LevelError    EventLevel = "error"
    LevelCritical EventLevel = "critical"
)

type Event struct {
    Type      string
    Level     EventLevel
    Group     string
    Runner    string
    Message   string
    Details   map[string]string
    Timestamp time.Time
}

// ─── Health ─────────────────────────────────────────────────────────

type GroupHealthStatus struct {
    Actual  int
    Desired int
    Max     int
    Min     int
    Healthy bool
    Issues  []HealthIssue
}

type HealthIssue struct {
    Level      EventLevel
    Type       string
    Group      string
    Runner     string
    Message    string
    DetectedAt time.Time
}
```

---

## Consumer-side interfaces

Each package defines **only the interface slice it needs**. No shared interface registry.

### controller/ needs to talk to GitHub

```go
// internal/controller/controller.go
package controller

type scaleSetClient interface {
    CreateScaleSet(ctx context.Context, group model.Group) (scaleSetID int, err error)
    DeleteScaleSet(ctx context.Context, scaleSetID int) error
    GenerateJITConfig(ctx context.Context, scaleSetID int, runnerName string) (string, error)
    OpenSession(ctx context.Context, scaleSetID int, owner string) (SessionClient, error)
}

type SessionClient interface {
    Close(ctx context.Context) error
    ListenerClient() any // returns *scaleset.MessageSessionClient
}
```

### controller/ needs to start runners

```go
// internal/controller/scaler.go
package controller

type runnerStarter interface {
    Prepare(ctx context.Context, instance model.RunnerInstance) error
    Start(ctx context.Context, instance model.RunnerInstance, jitConfig string) (*runner.Process, error)
    Stop(ctx context.Context, proc *runner.Process) error
    Cleanup(proc *runner.Process) error
}
```

### health/ needs runner state from controller

```go
// internal/health/monitor.go
package health

type runnerStateProvider interface {
    Snapshots() map[string][]model.RunnerSnapshot // group → runners
}
```

### health/ pushes to monitoring backends

```go
// internal/health/monitor.go
package health

type reporter interface {
    ReportDaemonHealth(ctx context.Context, groups int, totalActual, totalDesired int, checkDuration time.Duration)
    ReportGroupHealth(ctx context.Context, group string, actual, desired int)
}
```

### notification/service.go defines what a provider is

```go
// internal/notification/service.go
package notification

type Provider interface {
    Name() string
    Send(ctx context.Context, event model.Event) error
}
```

No `Accepts()` method. Event filtering is config-driven, handled by the Service, not the Provider.

---

## Daemon lifecycle with `oklog/run`

```go
// internal/cli/run.go (the actual daemon process)
package cli

import (
    "github.com/oklog/run"
)

func runDaemon(cfg *config.Config) error {
    // ... build all components via DI ...

    var g run.Group

    // Actor 1: controller (manages all scale set listeners)
    {
        ctx, cancel := context.WithCancel(context.Background())
        g.Add(
            func() error { return ctrl.Run(ctx) },
            func(error) { cancel() },
        )
    }

    // Actor 2: health monitor
    {
        ctx, cancel := context.WithCancel(context.Background())
        g.Add(
            func() error { return healthMon.Run(ctx) },
            func(error) { cancel() },
        )
    }

    // Actor 3: Unix socket API server
    {
        ctx, cancel := context.WithCancel(context.Background())
        g.Add(
            func() error { return apiServer.Run(ctx) },
            func(error) { cancel() },
        )
    }

    // Actor 4: signal handler (SIGINT, SIGTERM)
    {
        ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
        g.Add(
            func() error { <-ctx.Done(); return nil },
            func(error) { cancel() },
        )
    }

    // When ANY actor returns, ALL others are interrupted.
    err := g.Run()

    // Shutdown sequence
    shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Daemon.ShutdownTimeout)
    defer cancel()
    ctrl.Shutdown(shutdownCtx)

    return err
}
```

---

## Notification plugin system (idiomatic Go)

### Adding a new provider (e.g., Slack)

1. Create `internal/notification/slack.go`
2. Implement `notification.Provider` (defined in `service.go`):
   ```go
   type SlackProvider struct { webhookURL string; client *http.Client }
   func (s *SlackProvider) Name() string { return "slack" }
   func (s *SlackProvider) Send(ctx context.Context, event model.Event) error { ... }
   ```
3. Register in `cmd/ghr/main.go`:
   ```go
   if cfg.Notifications.Slack.Enabled {
       providers = append(providers, notification.NewSlack(cfg.Notifications.Slack))
   }
   ```
4. Done. Zero changes to any other package.

### Event flow

```
controller/scaler.go → notifier.Notify(event)
                             │
                             ▼
                    notification/service.go
                        for each provider:
                            if matchesFilter(event, provider.config.events):
                                provider.Send(event)
                                    │
                                    ├── discord.go → POST webhook
                                    ├── slack.go → POST webhook
                                    └── webhook.go → POST generic
```

### Adding a monitoring backend (e.g., Prometheus)

1. Create `internal/monitoring/prometheus.go`
2. Implement the `reporter` interface (defined in `health/monitor.go`)
3. Register in `cmd/ghr/main.go`
4. Done.

---

## Dependency injection (wiring)

All in `cmd/ghr/main.go`. Manual. No framework.

```go
func buildDaemon(cfg *config.Config) (*Daemon, error) {
    logger := logging.New(cfg.Logging)
    binary := runner.NewBinaryManager(cfg.Runner, logger)
    backend := runner.NewProcessManager(cfg.Runner, logger)
    creds, source, _ := auth.Load(auth.LoadOpts{TokenFlag: tokenFlag})
    logger.Info("authenticated", "method", creds.Method, "source", source)
    ghClient := github.NewClient(creds, cfg.GitHub, logger)

    var providers []notification.Provider
    if cfg.Notifications.Discord.Enabled {
        providers = append(providers, notification.NewDiscord(cfg.Notifications.Discord))
    }
    notifier := notification.New(providers, logger)

    var reporters []health.Reporter
    if cfg.Monitoring.UptimeKuma.Enabled {
        reporters = append(reporters, monitoring.NewUptimeKuma(cfg.Monitoring.UptimeKuma, logger))
    }

    healthMon := health.NewMonitor(cfg.Health, notifier, reporters, logger)
    ctrl := controller.New(ghClient, backend, binary, notifier, healthMon, cfg.Groups, logger)
    apiServer := api.NewServer(cfg.Daemon.StateDir, ctrl, healthMon, logger)

    return &Daemon{ctrl: ctrl, health: healthMon, api: apiServer, cfg: cfg}, nil
}
```

---

## Testing strategy

| Package | How to test |
|---|---|
| `model/` | No tests needed (pure data structs) |
| `controller/` | Mock `scaleSetClient` and `runnerStarter` (consumer-side interfaces) |
| `runner/` | Test binary download with HTTP test server. Test process lifecycle with real exec. |
| `github/` | Test with the scaleset SDK's `internal/testserver` pattern |
| `health/` | Mock `runnerStateProvider` and `reporter` |
| `notification/` | Test providers against `httptest.Server` |
| `monitoring/` | Test against `httptest.Server` |
| `config/` | Table-driven validation tests |
| `api/` | Test HTTP handlers with `httptest`, mock controller/health state |
| `cli/` | Thin layer, minimal tests |

---

## Key dependencies

| Package | Purpose |
|---|---|
| `github.com/actions/scaleset` | Scale Set API client + listener |
| `github.com/spf13/cobra` | CLI framework |
| `github.com/oklog/run` | Goroutine lifecycle management |
| `github.com/joho/godotenv` | .env loading |
| `gopkg.in/yaml.v3` | Config parsing |
| `log/slog` (stdlib) | Structured logging |
