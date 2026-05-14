# Spec 05 — Notification Service

## Overview

A pluggable notification system that sends alerts when events occur. Designed with a provider interface so adding new destinations (Discord, Slack, generic HTTP, Uptime Kuma, etc.) is trivial.

---

## Architecture

```
Health Monitor / Scaler / Controller
         │
         ▼
   NotificationService
         │
         ├── DiscordProvider
         └── (future: SlackProvider, GenericWebhookProvider, etc.)
```

> **Note:** Uptime Kuma is NOT a notification provider. It is a monitoring reporter
> (push-based health monitoring) and is covered in spec 06. The notification service
> handles event-driven alerts; Uptime Kuma handles periodic heartbeat pushes.

### NotificationService

```go
type NotificationService struct {
    logger    *slog.Logger
    providers []Provider
    filter    EventFilter
}

type Provider interface {
    Name() string
    Send(ctx context.Context, event model.Event) error
}
// No SupportedEvents() method. Event filtering is config-driven,
// handled by the Service based on each provider's configured event list.

// Event is model.Event from internal/model/ (defined once, shared across packages).
// See spec 00 for the Event struct definition.

type EventFilter struct {
    Events []string  // if non-empty, only send these event types
}
```

### Event routing

1. An event is created by any component (scaler, health monitor, controller)
2. `NotificationService.Notify(ctx, event)` is called
3. For each provider:
   - Check if the event matches the provider's configured event filter (config-driven, not provider-driven)
   - Call `provider.Send(ctx, event)`
4. Errors are logged but never propagated (notifications must not crash the daemon)

---

## Discord Provider

### Config

The webhook URL is a secret and must NOT be in config.yaml. It is provided via environment variable:
- `GHR_DISCORD_WEBHOOK_URL` — required if Discord notifications are enabled

config.yaml only contains non-secret configuration:
```yaml
notifications:
  discord:
    enabled: true
    events:                           # optional, empty = all events
      - "health.*"                    # all health events
      - "daemon.start"
      - "daemon.stop"
      - "runner.failed"
    username: "ghr"                   # optional, bot display name
    avatar_url: ""                    # optional
    mentions:                         # optional
      error: "<@&ROLE_ID>"           # mention a role on errors
      critical: "@everyone"          # mention everyone on critical
```

`.env` example:
```
GHR_DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/xxx/yyy
```

### Implementation

Discord webhook accepts a POST with JSON body:

```go
type discordPayload struct {
    Username  string          `json:"username,omitempty"`
    AvatarURL string          `json:"avatar_url,omitempty"`
    Content   string          `json:"content,omitempty"`    // text before embed, for mentions
    Embeds    []discordEmbed  `json:"embeds"`
}

type discordEmbed struct {
    Title       string         `json:"title"`
    Description string         `json:"description"`
    Color       int            `json:"color"`               // decimal color
    Fields      []discordField `json:"fields,omitempty"`
    Timestamp   string         `json:"timestamp,omitempty"` // ISO8601
    Footer      *discordFooter `json:"footer,omitempty"`
}
```

Color mapping:
- info → `0x3498DB` (blue)
- warning → `0xF39C12` (orange)
- error → `0xE74C3C` (red)
- critical → `0x992D22` (dark red)

Example embed for a health timeout event:

```json
{
  "username": "ghr",
  "embeds": [{
    "title": "Runner Timeout",
    "description": "Runner killed after exceeding 2h timeout",
    "color": 15844367,
    "fields": [
      {"name": "Group", "value": "backend-runners", "inline": true},
      {"name": "Runner", "value": "runner-z3w4m5n6", "inline": true},
      {"name": "Uptime", "value": "2h 3m 12s", "inline": true},
      {"name": "Action", "value": "Killed and cleaned up. Replacement will be created on next demand.", "inline": false}
    ],
    "timestamp": "2024-01-15T14:15:00Z",
    "footer": {"text": "ghr v2.0.0"}
  }]
}
```

### Rate limiting

Discord webhooks have rate limits (5 requests per 2 seconds per webhook). Implementation:
- Queue events and batch-send with a 500ms debounce
- If rate limited (HTTP 429), respect the `Retry-After` header
- Drop events if the queue exceeds 100 (log a warning)

---

## Generic Webhook Provider (future)

For custom integrations:

```yaml
notifications:
  webhook:
    enabled: true
    url: "https://hooks.example.com/ghr"
    method: POST                         # default POST
    headers:
      Authorization: "Bearer xxx"
    events: ["*"]                        # all events
```

Payload: the `Event` struct serialized as JSON.

---

## Event types

### Daemon events
| Event | Level | When |
|---|---|---|
| `daemon.start` | info | Daemon starts successfully |
| `daemon.stop` | info | Daemon stops gracefully |
| `daemon.crash` | critical | Daemon exits with error (sent on next start) |

### Group events
| Event | Level | When |
|---|---|---|
| `group.created` | info | Scale set created for a group |
| `group.deleted` | info | Scale set deleted |
| `group.scale_up` | info | Runners added to meet demand |
| `group.scale_down` | info | Runners removed after job completion |

### Runner events
| Event | Level | When |
|---|---|---|
| `runner.started` | info | Runner process launched |
| `runner.completed` | info | Job completed successfully |
| `runner.failed` | warning | Job completed with failure |
| `runner.timeout` | warning | Runner killed by health monitor (timeout) |

### Health events
| Event | Level | When |
|---|---|---|
| `health.zombie_runner` | error | Process dead but still tracked |
| `health.runner_timeout` | warning | Runner exceeded max run time |
| `health.group_degraded` | warning | Actual runners < desired for too long |
| `health.group_disconnected` | error | Listener lost connection to GitHub |
| `health.group_failing` | critical | Repeated start failures |
| `health.disk_low` | warning | Disk space below threshold |
| `health.orphan_killed` | info | Orphaned process found and killed |

### Event filtering syntax

- Exact match: `"daemon.start"`
- Wildcard: `"health.*"` (all health events)
- By level: `"*:error"` (all error-level events)
- Combination: `["health.*", "daemon.*", "runner.failed"]`

---

## Config schema

```yaml
notifications:
  discord:
    enabled: true
    # webhook_url is NOT in config — use GHR_DISCORD_WEBHOOK_URL env var
    events: ["health.*", "daemon.*", "runner.failed", "runner.timeout"]
    username: "ghr"
    mentions:
      error: "<@&123456>"
      critical: "@everyone"

  # Future providers follow the same pattern:
  # slack:
  #   enabled: true
  #   events: [...]
  #   # webhook_url via GHR_SLACK_WEBHOOK_URL env var
```

### Environment variables for secrets

| Env var | Purpose |
|---|---|
| `GHR_DISCORD_WEBHOOK_URL` | Discord webhook URL (required if discord.enabled) |
