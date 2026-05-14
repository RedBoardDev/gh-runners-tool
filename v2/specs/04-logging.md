# Spec 04 — Structured Logging

## Overview

ghr v2 uses Go's `log/slog` for structured logging. JSON format by default. Logs are organized by day and by runner for easy post-mortem debugging.

---

## Log structure on disk

```
{log_dir}/
  daemon/
    2024-01-15.json          # daemon-level events for that day
    2024-01-16.json
  groups/
    qa-runners/
      2024-01-15.json        # group-level events (scale up/down, health)
      runners/
        runner-a3f2b7c1/
          2024-01-15.json    # this runner's stdout/stderr + lifecycle events
        runner-b7c1d4e8/
          2024-01-15.json
    backend-runners/
      2024-01-15.json
      runners/
        runner-z3w4m5n6/
          2024-01-15.json
```

### Why this structure

- **By day**: easy rotation, easy cleanup (`find . -mtime +30 -delete`), grep a specific day
- **By runner**: when a job fails, you look at `runners/runner-a3f2/2024-01-15.json` and see exactly what happened to that runner — its startup, the job it ran, its output, its exit
- **By group**: group-level events (scaling decisions, health checks) without the noise of individual runner logs

---

## Log format

JSON Lines (one JSON object per line), slog format:

```json
{"time":"2024-01-15T14:32:15.123Z","level":"INFO","msg":"runner started","group":"qa-runners","runner":"runner-a3f2b7c1","pid":45678}
{"time":"2024-01-15T14:32:15.456Z","level":"INFO","msg":"job started","group":"qa-runners","runner":"runner-a3f2b7c1","job_id":"abc123","job_name":"build-api","workflow_run_id":1234,"repository":"my-org/my-repo"}
{"time":"2024-01-15T14:44:27.789Z","level":"INFO","msg":"job completed","group":"qa-runners","runner":"runner-a3f2b7c1","job_id":"abc123","result":"success","duration_s":732}
{"time":"2024-01-15T14:44:28.012Z","level":"INFO","msg":"runner stopped","group":"qa-runners","runner":"runner-a3f2b7c1","reason":"job_completed","exit_code":0}
```

### Standard fields

Every log entry includes:
- `time` (RFC3339 with milliseconds)
- `level` (DEBUG, INFO, WARN, ERROR)
- `msg` (human-readable message)

### Contextual fields (added by logger hierarchy)

- Daemon logger: `component="daemon"`
- Group logger: `component="group"`, `group="qa-runners"`
- Runner logger: `component="runner"`, `group="qa-runners"`, `runner="runner-a3f2b7c1"`
- Health logger: `component="health"`
- Notification logger: `component="notification"`

---

## Implementation

### Logger hierarchy

```go
// Root logger
rootLogger := slog.New(slog.NewJSONHandler(daemonFile, &slog.HandlerOptions{
    Level: configuredLevel,
}))

// Group logger (writes to group file + daemon file)
groupLogger := rootLogger.With("component", "group", "group", groupName)

// Runner logger (writes to runner file + group file + daemon file)
runnerLogger := groupLogger.With("component", "runner", "runner", runnerName)
```

### Multi-writer handler

A custom slog.Handler that writes to multiple destinations:

```go
type MultiHandler struct {
    handlers []slog.Handler
}

func (h *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
    for _, handler := range h.handlers {
        if err := handler.Handle(ctx, r); err != nil {
            // Log to stderr as fallback, don't propagate
        }
    }
    return nil
}
```

This allows a runner **lifecycle** log entry (started, completed, killed) to appear in:
1. The runner's own file (`runners/runner-a3f2/2024-01-15.json`)
2. The group's file (`qa-runners/2024-01-15.json`)
3. The daemon's file (`daemon/2024-01-15.json`)

**Important:** Runner stdout/stderr lines (raw output from `run.sh`) only appear in the runner's own log file. They do NOT propagate to group or daemon log files. Only lifecycle events (runner started, job started, job completed, runner stopped/killed) propagate upward.

### File rotation

- New file created at midnight (UTC or local, configurable)
- Old files are NOT automatically deleted (user controls retention)
- File names include the date: `2024-01-15.json`
- Rotation is handled by checking the date on each write and opening a new file if needed

### Runner process output capture

Runner stdout/stderr (from `run.sh`) is captured to the runner's log file:

```go
runnerLogFile := openLogFile(logDir, groupName, runnerName)
cmd.Stdout = runnerLogFile
cmd.Stderr = runnerLogFile
```

This raw output is separate from the structured JSON logs. Options:
- **Option A**: Write runner output directly to the runner's JSON log file (each line wrapped as a JSON entry with `"stream":"stdout"`)
- **Option B**: Separate file for runner output (`runner-a3f2.output.log`) alongside the JSON log

Recommendation: **Option A** — keeps everything in one file, easier to correlate lifecycle events with runner output.

```json
{"time":"...","level":"INFO","msg":"runner started","runner":"runner-a3f2"}
{"time":"...","level":"DEBUG","msg":"runner output","runner":"runner-a3f2","stream":"stdout","line":"Starting Runner listener with startup type: Jit"}
{"time":"...","level":"DEBUG","msg":"runner output","runner":"runner-a3f2","stream":"stdout","line":"Started session: abc123"}
{"time":"...","level":"INFO","msg":"job started","runner":"runner-a3f2","job_name":"build-api"}
{"time":"...","level":"DEBUG","msg":"runner output","runner":"runner-a3f2","stream":"stdout","line":"Running job: build-api"}
{"time":"...","level":"INFO","msg":"job completed","runner":"runner-a3f2","result":"success"}
```

---

## Console output (foreground mode)

When running `ghr run` in foreground, also log to stderr in human-readable format:

```
[ghr] 14:32:15 INFO  runner started           group=qa-runners runner=runner-a3f2 pid=45678
[ghr] 14:32:15 INFO  job started              group=qa-runners runner=runner-a3f2 job=build-api
[ghr] 14:44:27 INFO  job completed            group=qa-runners runner=runner-a3f2 result=success duration=12m12s
[ghr] 14:44:28 INFO  runner stopped           group=qa-runners runner=runner-a3f2 reason=job_completed
```

Controlled by config `logging.format`:
- `json` → JSON only (file + console)
- `text` → text on console, JSON in files
- When running via launchd, console goes to the daemon log file anyway

---

## Config schema

```yaml
logging:
  level: info                    # debug, info, warn, error
  format: text                   # text (console) + json (files), or json (both)
  dir: "/var/log/ghr"            # log directory
  retention_days: 30             # 0 = keep forever (default: 30)
  runner_output: true            # capture runner stdout/stderr in logs (default: true)
```

---

## Log retention

Optional cleanup of old logs:

```go
func (l *LogManager) CleanupOldLogs(retentionDays int) {
    cutoff := time.Now().AddDate(0, 0, -retentionDays)
    // Walk log dir, delete files older than cutoff
}
```

Called once at daemon startup and then once per day.

---

## Key log events

| Event | Level | Fields |
|---|---|---|
| Daemon started | INFO | version, config_path, groups_count |
| Daemon stopped | INFO | reason, uptime |
| Group created | INFO | group, scale_set_id, max_runners, min_runners |
| Group deleted | INFO | group, scale_set_id |
| Runner started | INFO | group, runner, pid |
| Runner output line | DEBUG | group, runner, stream, line |
| Job started | INFO | group, runner, job_id, job_name, workflow_run_id, repository |
| Job completed | INFO | group, runner, job_id, result, duration_s |
| Runner stopped | INFO | group, runner, reason, exit_code |
| Runner killed (health) | WARN | group, runner, reason, uptime |
| Health check passed | DEBUG | checks_run, issues_found |
| Health issue detected | WARN/ERROR | issue_type, group, runner, details |
| Scale up | INFO | group, from, to, reason |
| Scale down | INFO | group, from, to, reason |
| Config reloaded | INFO | changes |
| Notification sent | INFO | channel, event, target |
| Notification failed | ERROR | channel, event, error |
| Startup cleanup | INFO | stale_workdirs, stale_processes |
| Disk space warning | WARN | available, threshold |
