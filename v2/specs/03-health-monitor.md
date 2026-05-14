# Spec 03 — Health Monitor

## Overview

The health monitor runs as a background goroutine inside the daemon. It periodically checks the health of all runners and groups, detects anomalies, takes corrective action, and reports issues to the notification system.

---

## Health checks

### Runner-level checks

**1. Process liveness**
- Verify that the PID from the runner state is still alive (`kill -0 pid`)
- If process is dead but runner is still in the state map → **zombie runner**
- Action: remove from state, cleanup workdir, log error, notify

**2. Runner timeout**
- If a runner has been in "busy" state longer than `health.runner_timeout` → **stuck runner**
- Default timeout: 2h (configurable per group or global)
- Action: SIGTERM → wait 10s → SIGKILL, cleanup workdir, log, notify
- The listener will receive a `HandleDesiredRunnerCount` on the next poll and create a replacement

**3. Idle runner timeout**
- If a runner has been in "idle" state longer than `health.idle_timeout` → **stale idle runner**
- Default: no timeout (idle runners are expected when minRunners > 0)
- Optional: configurable per group for cost-conscious setups
- Action: kill process, cleanup workdir (HandleDesiredRunnerCount will recreate if needed)

### Group-level checks

**4. Desired vs actual divergence**
- Compare `TotalAssignedJobs` (from latest statistics) with actual running runner count
- If divergence persists for more than `health.divergence_timeout` (default: 5min) → **group degraded**
- This catches the v1 bug: GitHub deregistered runners but local processes kept running
- Action: log warning, notify, mark group as "degraded" in status

**5. Scale set connectivity**
- If the listener for a group has been disconnected (session expired, API errors) for more than `health.reconnect_timeout` (default: 2min) → **group disconnected**
- Action: log error, notify, attempt restart of the group's listener

**6. Repeated start failures**
- Track consecutive startRunner failures per group
- If > `health.max_consecutive_failures` (default: 5) → **group failing**
- Action: log error, notify, pause the group for `health.failure_cooldown` (default: 1min) before retrying

### Daemon-level checks

**7. Disk space**
- Check available disk space on the workdir_base and cache_dir partitions
- If below `health.min_disk_space` (default: 1GB) → **disk warning**
- Action: log warning, notify, attempt cleanup of old cached runner versions

**8. Process count**
- Count total runner processes vs expected (sum of all group runner counts)
- Detect orphaned processes (running but not in any state map)
- Action: kill orphans, log

---

## Implementation

### HealthMonitor struct

```go
type HealthMonitor struct {
    logger     *slog.Logger
    notifier   notifier           // unexported interface (see below)
    runners    runnerStateProvider // consumer-side interface (see spec 00)
    reporters  []reporter         // consumer-side interface (see spec 00)
    interval   time.Duration
    config     HealthConfig

    mu         sync.RWMutex
    groups     map[string]*GroupHealth
    lastCheck  time.Time
    issues     []HealthIssue
}

// Consumer-side interfaces — no import of notification or controller packages.
type notifier interface {
    Notify(ctx context.Context, event model.Event) error
}

type GroupHealth struct {
    name              string
    stats             GroupStats      // local stats struct, no scaleset SDK dependency
    lastStatsUpdate   time.Time
    consecutiveFailures int
    degradedSince     *time.Time
    disconnectedSince *time.Time
}

// Local stats struct — decouples from *scaleset.RunnerScaleSetStatistic.
type GroupStats struct {
    TotalAssignedJobs int
    TotalRunning      int
    TotalIdle         int
}

type HealthIssue struct {
    Level     model.EventLevel // model.LevelWarning, model.LevelError, model.LevelCritical
    Group     string           // group name or "" for daemon-level
    Runner    string           // runner name or "" for group/daemon-level
    Type      string           // "zombie", "stuck", "degraded", "disconnected", "disk", etc.
    Message   string
    DetectedAt time.Time
    ResolvedAt *time.Time
}
```

### Check loop

```go
func (h *HealthMonitor) Run(ctx context.Context) {
    ticker := time.NewTicker(h.interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            h.runChecks(ctx)
        }
    }
}

func (h *HealthMonitor) runChecks(ctx context.Context) {
    h.mu.Lock()
    defer h.mu.Unlock()

    h.lastCheck = time.Now()
    h.issues = h.issues[:0]  // reset

    for _, group := range h.groups {
        h.checkRunnerLiveness(group)
        h.checkRunnerTimeouts(group)
        h.checkGroupDivergence(group)
        h.checkConsecutiveFailures(group)
    }
    h.checkDiskSpace()
    h.checkOrphanedProcesses()

    // Notify new issues (convert HealthIssue to model.Event)
    for _, issue := range h.issues {
        h.notifier.Notify(ctx, issue.ToEvent())
    }
}
```

### Integration with the Scaler

The health monitor needs access to each scaler's internal state (idle/busy maps). Two approaches:

**Option A — Scaler exposes a snapshot method:**
```go
func (s *MacOSScaler) HealthSnapshot() RunnerSnapshot {
    s.mu.Lock()
    defer s.mu.Unlock()
    // return copy of idle/busy maps with timestamps
}
```

**Option B — Scaler reports to health monitor via callbacks:**
```go
type RunnerEvent struct {
    Type      string    // "started", "busy", "completed", "failed"
    Group     string
    Runner    string
    PID       int
    Timestamp time.Time
}
```

Recommendation: **Option A** (simpler, health monitor pulls state on its schedule).

### Statistics update

The health monitor needs the latest `RunnerScaleSetStatistic` for divergence checks. The `MetricsRecorder` interface is the natural hook:

```go
type healthMetricsRecorder struct {
    monitor *HealthMonitor
    group   string
}

func (r *healthMetricsRecorder) RecordStatistics(stats *scaleset.RunnerScaleSetStatistic) {
    r.monitor.updateGroupStats(r.group, stats)
}
```

Each group's listener gets a `healthMetricsRecorder` via `listener.WithMetricsRecorder()`.

---

## Config schema

```yaml
health:
  enabled: true                          # default: true
  check_interval: 30s                    # default: 30s
  runner_timeout: 2h                     # default: 2h, max time a runner can be busy
  idle_timeout: 0                        # default: 0 (disabled), max idle time
  divergence_timeout: 5m                 # default: 5m, how long to tolerate desired!=actual
  reconnect_timeout: 2m                  # default: 2m, how long before flagging disconnection
  max_consecutive_failures: 5            # default: 5, per group
  failure_cooldown: 1m                   # default: 1m, pause after max failures
  min_disk_space: 1GB                    # default: 1GB
```

Per-group override (optional):
```yaml
groups:
  - name: backend-runners
    max_runners: 6
    health:
      runner_timeout: 30m                # shorter timeout for deploy runners
```

---

## Status integration

The health monitor exposes its state to `ghr status`:

```go
func (h *HealthMonitor) Status() HealthStatus {
    h.mu.RLock()
    defer h.mu.RUnlock()
    return HealthStatus{
        LastCheck: h.lastCheck,
        Issues:    append([]HealthIssue{}, h.issues...),
        Groups:    h.groupHealthSummaries(),
    }
}
```

This is displayed in the "Health" section of `ghr status`.

---

## Notification integration

Health issues are sent to the notification service (spec 05). Each issue maps to a notification event:

| Issue type | Event | Severity |
|---|---|---|
| Zombie runner | `health.zombie_runner` | error |
| Stuck runner (timeout) | `health.runner_timeout` | warning |
| Group degraded | `health.group_degraded` | warning |
| Group disconnected | `health.group_disconnected` | error |
| Group failing (repeated) | `health.group_failing` | critical |
| Disk space low | `health.disk_low` | warning |
| Orphaned process killed | `health.orphan_killed` | info |
