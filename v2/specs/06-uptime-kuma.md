# Spec 06 — Uptime Kuma Integration

## Overview

Push-based health monitoring via Uptime Kuma's push monitor API. Reports daemon heartbeat and per-group health status. No Socket.IO dependency in the daemon — pure HTTP pushes.

---

## How Uptime Kuma push monitors work

**Endpoint:** `GET /api/push/<pushToken>?status=<up|down>&msg=<text>&ping=<ms>`

- **Dead man's switch**: if no push arrives within the configured heartbeat interval, the monitor goes DOWN
- **Only 2 states**: `up` or `down` (no native "degraded")
- **`msg`** max ~250 chars, only recorded on status change
- **`ping`** displayed as a graph in the dashboard — we use it for health ratio visualization

---

## Monitor strategy

### Hierarchy

```
Uptime Kuma Dashboard
├── [PUSH] ghr-daemon                 # Global daemon heartbeat
├── [PUSH] ghr-qa-runners             # Group health
├── [PUSH] ghr-frontend-runners       # Group health
├── [PUSH] ghr-backend-runners        # Group health
└── [GROUP] ghr (optional)            # Status page group
```

**1 daemon monitor** — proves the daemon is alive. If ghr crashes or the Mac reboots and launchd doesn't restart it, this goes DOWN.

**1 monitor per group** — reports group health. Catches:
- All runners dead in a group (status=down)
- Partial failure / degradation (status depends on threshold)
- Runners not being created (desired > 0, actual = 0)

**No per-runner monitors** — too many monitors, too noisy, not useful (groups are the right granularity).

---

## Status logic

### Daemon monitor

Pushed on every health check interval (default 30s):

```
Always status=up (the push itself proves liveness)
msg = "groups=3 runners=12/18 healthy"
ping = health check duration in ms
```

If the daemon crashes, the push stops, and Uptime Kuma marks it DOWN after the heartbeat interval. That's the whole point of the dead man's switch.

### Group monitor

Pushed on every health check interval:

```go
func groupStatus(actual, desired int, threshold float64) (status string, msg string) {
    if desired == 0 {
        return "up", "idle (0 desired)"
    }
    if actual == 0 {
        return "down", fmt.Sprintf("0/%d runners (outage)", desired)
    }

    ratio := float64(actual) / float64(desired)
    if ratio < threshold {
        return "down", fmt.Sprintf("%d/%d runners (critical)", actual, desired)
    }
    if actual < desired {
        return "up", fmt.Sprintf("%d/%d runners (degraded)", actual, desired)
    }
    return "up", fmt.Sprintf("%d/%d runners", actual, desired)
}
```

**The `degraded_threshold`** controls when "partial failure" becomes "down":
- `0.5` (default): DOWN when less than half the runners are active
- `1.0`: DOWN if any runner is missing (strict)
- `0.0`: DOWN only on total outage (lenient)

### Using `ping` for health visualization

Encode the health ratio as a percentage in the `ping` field:

```go
ping = (float64(actual) / float64(desired)) * 100  // 0-100
```

Uptime Kuma graphs this as "response time". The user sees a "health curve" per group:
- 100 = all runners active
- 60 = 60% of desired runners active
- 0 = no runners

This gives visual trend data even when the status remains "up".

---

## Push implementation

### UptimeKumaPusher

```go
type UptimeKumaPusher struct {
    baseURL    string
    httpClient *http.Client
    logger     *slog.Logger
}

func NewPusher(baseURL string, logger *slog.Logger) *UptimeKumaPusher {
    return &UptimeKumaPusher{
        baseURL:    strings.TrimRight(baseURL, "/"),
        httpClient: &http.Client{Timeout: 10 * time.Second},
        logger:     logger,
    }
}

func (p *UptimeKumaPusher) Push(ctx context.Context, token, status, msg string, ping float64) error {
    u := fmt.Sprintf("%s/api/push/%s?status=%s&msg=%s",
        p.baseURL, token, status, url.QueryEscape(truncate(msg, 250)))
    if ping >= 0 {
        u += fmt.Sprintf("&ping=%.1f", ping)
    }

    req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
    if err != nil {
        return err
    }

    resp, err := p.httpClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        return fmt.Errorf("push failed: HTTP %d", resp.StatusCode)
    }
    return nil
}
```

### UptimeKumaReporter

Integrates with the health monitor. Called on every health check cycle.

```go
type UptimeKumaReporter struct {
    pusher    *UptimeKumaPusher
    config    UptimeKumaConfig
    logger    *slog.Logger
}

// ReportDaemonHealth matches the reporter interface defined in health/monitor.go (spec 00).
func (r *UptimeKumaReporter) ReportDaemonHealth(ctx context.Context, groups int, totalActual, totalDesired int, checkDuration time.Duration) {
    token := r.resolveDaemonToken()
    if token == "" {
        return
    }
    msg := fmt.Sprintf("groups=%d runners=%d/%d", groups, totalActual, totalDesired)

    if err := r.pusher.Push(ctx, token, "up", msg, float64(checkDuration.Milliseconds())); err != nil {
        r.logger.Warn("uptime-kuma daemon push failed", "error", err)
    }
}

// ReportGroupHealth matches the reporter interface defined in health/monitor.go (spec 00).
func (r *UptimeKumaReporter) ReportGroupHealth(ctx context.Context, group string, actual, desired int) {
    token := r.resolveGroupToken(group)
    if token == "" {
        return
    }

    status, msg := groupStatus(actual, desired, r.config.DegradedThreshold)
    ping := -1.0
    if r.config.ReportHealthAsPing && desired > 0 {
        ping = (float64(actual) / float64(desired)) * 100
    }

    if err := r.pusher.Push(ctx, token, status, msg, ping); err != nil {
        r.logger.Warn("uptime-kuma group push failed", "group", group, "error", err)
    }
}

// resolveDaemonToken returns the daemon push token from GHR_UPTIME_KUMA_DAEMON_TOKEN env var.
func (r *UptimeKumaReporter) resolveDaemonToken() string {
    return os.Getenv("GHR_UPTIME_KUMA_DAEMON_TOKEN")
}

// resolveGroupToken returns the push token for a group from GHR_UPTIME_KUMA_TOKEN_{GROUP} env var
// (group name uppercased, hyphens replaced with underscores).
func (r *UptimeKumaReporter) resolveGroupToken(group string) string {
    envKey := "GHR_UPTIME_KUMA_TOKEN_" + strings.ToUpper(strings.ReplaceAll(group, "-", "_"))
    return os.Getenv(envKey)
}
```

---

## Monitor setup

### Manual setup (recommended for MVP)

1. In Uptime Kuma UI, create push monitors:
   - "ghr-daemon" (heartbeat interval = health check interval + buffer, e.g., 60s)
   - "ghr-qa-runners" (same interval)
   - "ghr-backend-runners"
   - etc.
2. Copy each monitor's push token
3. Add tokens to `.env` file or set as environment variables (see below)

### `ghr setup-monitoring` command (future)

Auto-provisions monitors via Uptime Kuma's Socket.IO API:

```bash
ghr setup-monitoring --config config.yaml
# Connects to Uptime Kuma via Socket.IO
# Creates monitors for daemon + each group
# Writes push tokens back to config.yaml or to a separate tokens file
```

Uses `github.com/nobbs/uptime-kuma-api` Go library. Not in the daemon itself — just a one-shot setup command.

---

## Config schema

Push tokens and the base URL are secrets and must NOT be in config.yaml. They are provided exclusively via environment variables.

config.yaml only contains non-secret configuration:
```yaml
monitoring:
  uptime_kuma:
    enabled: true
    degraded_threshold: 0.5                # 0.0-1.0, default 0.5
    report_health_as_ping: true            # encode health% in ping field
```

### Environment variables

| Env var | Purpose |
|---|---|
| `GHR_UPTIME_KUMA_URL` | Uptime Kuma base URL (required if enabled) |
| `GHR_UPTIME_KUMA_DAEMON_TOKEN` | Daemon heartbeat push token (optional) |
| `GHR_UPTIME_KUMA_TOKEN_{GROUP}` | Per-group push token (group name uppercased, hyphens→underscores) |

`.env` example:
```
GHR_UPTIME_KUMA_URL=https://status.example.com
GHR_UPTIME_KUMA_DAEMON_TOKEN=abc123
GHR_UPTIME_KUMA_TOKEN_QA_RUNNERS=tok_qa_xxxx
GHR_UPTIME_KUMA_TOKEN_BACKEND_RUNNERS=tok_backend_xxxx
```

---

## Limitations

| Limitation | Impact | Mitigation |
|---|---|---|
| No "degraded" status | Can't visually distinguish partial from full outage | `degraded_threshold` + descriptive `msg` |
| `msg` only recorded on status change | Repeated "OK" messages not in history | Use `ping` for continuous health graph |
| ~250 char msg limit | Can't send rich data | Keep msg concise, details in ghr logs |
| No per-runner granularity | Can't monitor individual runners | Group-level is sufficient; use `ghr status` for runner details |
