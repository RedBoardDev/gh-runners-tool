# Spec 02 — CLI Commands

## Overview

ghr v2 uses the hybrid model: `start/stop/restart/status` for service management, `run` for foreground/debug mode, `purge` for emergency reset.

CLI framework: `spf13/cobra` (same as v1).

---

## Commands

### `ghr start`

Install the launchd service and start the daemon. Idempotent — if already running, report it.

```bash
ghr start --config /path/to/config.yaml
```

**Flow:**
1. Validate config
2. Check if already running (read PID file, check process alive)
   - If running → print "ghr is already running (pid=X)" and exit 0
3. Determine service type:
   - Running as root → LaunchDaemon (`/Library/LaunchDaemons/com.ghr.daemon.plist`)
   - Running as user → LaunchAgent (`~/Library/LaunchAgents/com.ghr.daemon.plist`)
4. Generate plist from template (embed config path, binary path, log paths)
5. Write plist to disk
6. `launchctl load {plist_path}`
7. `launchctl start com.ghr.daemon`
8. Wait up to 5s for PID file to appear
9. Print summary:
   ```
   ghr started (pid=12345)
   Service: com.ghr.daemon (LaunchDaemon)
   Config:  /path/to/config.yaml
   Groups:  3 (qa-runners, frontend-runners, backend-runners)
   Logs:    /var/log/ghr/
   ```

**Flags:**
- `--config` (string, required): path to config.yaml
- `--foreground` (bool): equivalent to `ghr run` (don't use launchd, stay in foreground)

**Plist template:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.ghr.daemon</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.BinaryPath}}</string>
        <string>run</string>
        <string>--config</string>
        <string>{{.ConfigPath}}</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>
    <key>StandardOutPath</key>
    <string>{{.LogDir}}/daemon.log</string>
    <key>StandardErrorPath</key>
    <string>{{.LogDir}}/daemon.err</string>
    <key>WorkingDirectory</key>
    <string>{{.StateDir}}</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
    </dict>
</dict>
</plist>
```

Note: `KeepAlive.SuccessfulExit = false` means launchd restarts only on non-zero exit (crash). A clean `ghr stop` exits 0 and stays stopped.

---

### `ghr stop`

Stop the daemon gracefully and unload the launchd service.

```bash
ghr stop
```

**Flow:**
1. Check if running (PID file + process alive)
   - If not running → print "ghr is not running" and exit 0
2. Send SIGTERM to the daemon process
3. Wait up to 30s for process to exit (polls PID every 500ms)
   - If timeout → send SIGKILL
4. `launchctl unload {plist_path}` (if plist exists)
5. Remove plist file
6. Print "ghr stopped"

**Flags:**
- `--timeout` (duration, default 30s): max wait for graceful shutdown
- `--force` (bool): skip SIGTERM, go straight to SIGKILL

---

### `ghr restart`

Stop then start. Convenience wrapper.

```bash
ghr restart [--config /path/to/config.yaml]
```

**Flow:**
1. `ghr stop` (with same timeout)
2. `ghr start` (with provided or previously stored config path)

The config path is optional — if not provided, reads it from the existing plist or state file.

---

### `ghr run`

Foreground mode. The actual daemon process. Used by:
- launchd (via the plist ProgramArguments)
- Developers for testing/debugging

```bash
ghr run --config config.yaml
```

**Flow:**
1. Validate config
2. Write PID file
3. Start the core engine (GroupController)
4. Block on signal (SIGTERM/SIGINT)
5. Shutdown
6. Remove PID file
7. Exit 0 (clean) or 1 (error)

stdout/stderr go to the terminal (when run manually) or to the log files (when run via launchd).

**Flags:**
- `--config` (string, required): path to config.yaml

---

### `ghr status`

Display comprehensive real-time status. Queries both local state AND GitHub API.

```bash
ghr status [--config /path/to/config.yaml]
ghr status --json
ghr status --watch
```

**Output format (human-readable):**

```
Service
  Status:    running
  PID:       12345
  Uptime:    3d 14h 22m
  Service:   com.ghr.daemon (LaunchDaemon)
  Config:    /etc/ghr/config.yaml
  Auth:      GitHub App (org: my-org)
  Runner:    v2.330.0 (cached)

Groups
  ┌──────────────────┬─────┬─────┬──────────┬─────────┬──────┬────────┐
  │ Name             │ Max │ Min │ Assigned │ Running │ Idle │ Health │
  ├──────────────────┼─────┼─────┼──────────┼─────────┼──────┼────────┤
  │ qa-runners       │  10 │   2 │        4 │       4 │    2 │ OK     │
  │ frontend-runners │   2 │   0 │        1 │       1 │    0 │ OK     │
  │ backend-runners  │   6 │   0 │        0 │       0 │    0 │ OK     │
  └──────────────────┴─────┴─────┴──────────┴─────────┴──────┴────────┘
  Total: assigned=5  running=5  idle=2

Runners
  ┌───────────────────────────────┬─────────┬────────────────────────────┬─────────┬───────┐
  │ Runner                        │ Status  │ Job                        │ Uptime  │ PID   │
  ├───────────────────────────────┼─────────┼────────────────────────────┼─────────┼───────┤
  │ qa-runners/runner-a3f2        │ busy    │ build-api #1234            │ 12m 3s  │ 45678 │
  │ qa-runners/runner-b7c1        │ busy    │ test-suite #1235           │ 3m 41s  │ 45679 │
  │ qa-runners/runner-d4e8        │ busy    │ lint #1236                 │ 1m 12s  │ 45680 │
  │ qa-runners/runner-f9a0        │ busy    │ deploy-staging #1237       │ 8m 55s  │ 45681 │
  │ qa-runners/runner-1c3d        │ idle    │ —                          │ 45s     │ 45682 │
  │ qa-runners/runner-2e5f        │ idle    │ —                          │ 45s     │ 45683 │
  │ frontend-runners/runner-7g8h  │ busy    │ build-web #1238            │ 5m 20s  │ 45684 │
  └───────────────────────────────┴─────────┴────────────────────────────┴─────────┴───────┘

Health
  Last check:  30s ago
  Next check:  in 0s
  Issues:      none

Recent Events (last 1h)
  14:32:15  qa-runners/runner-x1y2      completed   build-api #1230        4m 12s   success
  14:28:03  backend-runners/runner-z3w4  completed   deploy-prod #1229     12m 44s   success
  14:15:00  qa-runners/runner-m5n6      killed      — (timeout: exceeded 2h limit)
```

**`--watch` mode:**
- Clears terminal, re-renders every 5s (configurable)
- Uses ANSI escape codes for in-place update
- Shows a "last updated: Xs ago" timestamp
- Ctrl+C to exit

**`--json` mode:**
- Outputs the same data as a JSON object
- Useful for scripting, piping to `jq`, or external monitoring tools

**`--watch` + `--json` mode:**
- Outputs one JSON object per line every 5s (JSONL format)

**Data sources (via Unix socket IPC):**

`ghr status` connects to the daemon's Unix socket at `{state_dir}/ghr.sock` and queries the JSON API:
- `GET /status` → full status (service info, groups, runners, health, recent events)
- `GET /health` → health check result only

The socket provides real-time data from the running daemon (group stats, runner details, health state, recent events). No need to read log files or query the GitHub API directly from the CLI.

**When daemon is not running (socket not available):**

Falls back to reading static state from disk:
- Service info: PID file + `daemon.state.json`
- launchctl status (if plist exists)

```
Service
  Status:    stopped
  Last run:  2024-01-15 14:32 (exited 2h ago)
  Config:    /etc/ghr/config.yaml

No active groups or runners.
Use 'ghr start' to start the daemon.
```

**Flags:**
- `--config` (string, optional): config path. Auto-detected from plist or state file if not provided
- `--json` (bool): JSON output
- `--watch` (bool): live refresh mode
- `--interval` (duration, default 5s): refresh interval for --watch

---

### `ghr purge`

Emergency reset. Stops everything, deletes all scale sets, cleans all workdirs.

```bash
ghr purge --config /path/to/config.yaml [--timeout 10m]
```

**Flow:**
1. If daemon is running → stop it first (ghr stop)
2. Load credentials (`auth.Load()` — same resolution order as daemon startup)
3. Create a temporary scaleset client using resolved credentials + GitHub URL
4. For each group in config:
   - Find the scale set by name
   - List all runners in the scale set
   - Wait for busy runners to go idle (with timeout, polling)
   - Delete the scale set (which removes all runner registrations)
5. Cleanup local:
   - Kill any stray processes found via PID files
   - Remove all workdir directories
   - Remove state files
6. Print summary: "purged X scale sets, cleaned Y workdirs"

**Flags:**
- `--config` (string, required): config path
- `--timeout` (duration, default 5m): max wait for busy runners
- `--force` (bool): don't wait for busy runners, delete immediately

---

### `ghr login`

Interactive authentication wizard. Validates credentials and saves them.

```bash
ghr login
```

**Interactive flow:**
1. Prompt: PAT or GitHub App?
2. Collect credentials (token, or client_id + installation_id + key path)
3. Collect GitHub URL (org, repo, or enterprise)
4. Validate by calling GitHub API
5. Save to credentials file (`~/.config/ghr/credentials.json` or `/etc/ghr/credentials.json`)

**Non-interactive (for scripts/CI):**
```bash
ghr login --method pat --token ghp_xxx --url https://github.com/my-org
ghr login --method app --client-id Iv1.abc --installation-id 123 --private-key /path/key.pem --url https://github.com/my-org
```

See spec 08-auth.md for full details.

---

### `ghr logout`

Remove saved credentials.

```bash
ghr logout
# ✓ Credentials removed
```

---

### `ghr auth status`

Display current authentication state without exposing secrets.

```bash
ghr auth status
# Method:    GitHub App
# GitHub:    https://github.com/my-org
# Status:    ✓ authenticated
```

---

## Global flags (on root command)

- `--config` (string): path to config file (most commands need it)
- `--token` (string): override auth token for this invocation
- `--log-level` (string): override log level (debug/info/warn/error)

---

## State files

```
{state_dir}/
  daemon.pid          # PID of the running daemon
  daemon.state.json   # last known config path, start time, scale set IDs
  ghr.sock            # Unix socket for IPC (created by daemon, removed on shutdown)
```

`daemon.state.json` allows commands like `ghr stop` and `ghr status` to work without `--config`:
```json
{
  "config_path": "/etc/ghr/config.yaml",
  "started_at": "2024-01-15T10:00:00Z",
  "pid": 12345,
  "groups": {
    "qa-runners": {"scale_set_id": 42},
    "backend-runners": {"scale_set_id": 43}
  }
}
```
