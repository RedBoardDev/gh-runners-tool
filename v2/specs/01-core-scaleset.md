# Spec 01 — Core: Scale Set Engine

## Overview

The foundation of ghr v2. Replaces the v1 reconciler/slots system with the official `actions/scaleset` Go SDK. Each configured group becomes an independent scale set with its own listener, scaler, and runner pool.

---

## Components

### ScaleSetClient

One `scaleset.Client` instance shared across all groups. Handles authentication (PAT or GitHub App) and all GitHub API communication.

**Construction:**
- Credentials are loaded by `auth.Load()` (see spec 08-auth.md)
- If credentials method is `github_app` → `scaleset.NewClientWithGitHubApp`
- If credentials method is `pat` → `scaleset.NewClientWithPersonalAccessToken`
- GitHub URL comes from the credentials file (set during `ghr login`)

**Config URL** from credentials:
- Org scope: `https://github.com/my-org`
- Repo scope: `https://github.com/my-org/my-repo`

### GroupController

Orchestrates the lifecycle of all groups. One goroutine per group, each running independently.

```
GroupController
  ├── group "qa-runners"
  │     ├── RunnerScaleSet (id=42, name="qa-runners")
  │     ├── MessageSessionClient
  │     ├── listener.Listener
  │     └── MacOSScaler
  ├── group "backend-runners"
  │     ├── RunnerScaleSet (id=43, name="backend-runners")
  │     ├── MessageSessionClient
  │     ├── listener.Listener
  │     └── MacOSScaler
  └── ...
```

**Scale set resolution at startup (per group):**
1. Call `GetRunnerScaleSet(name)` to check if a scale set with this name already exists
2. If it exists → reuse (update labels/settings if needed)
3. If it does not exist → `CreateRunnerScaleSet` with name, labels, runner group, `DisableUpdate: true`

**Startup sequence per group:**
1. Resolve runner group ID (default = 1, or lookup by name)
2. Resolve scale set (see above)
3. `SetSystemInfo` with the scale set ID
4. `MessageSessionClient(ctx, scaleSetID, hostname)`
5. `listener.New(sessionClient, config)`
6. Create `MacOSScaler` with runner manager reference
7. Resolve runner version: use group-level `version` if set, otherwise global `runner.version`
8. `listener.Run(ctx, scaler)` — blocks until context cancelled or error

**Shutdown sequence per group (reverse order):**
1. Cancel the group context → listener.Run returns
2. Scaler shutdown: kill all runner processes, cleanup workdirs
3. Close session: `sessionClient.Close(ctx)`
4. Delete scale set: `client.DeleteRunnerScaleSet(ctx, id)`

**Per-group goroutine management:**

`oklog/run` manages top-level actors only (controller, health monitor, API server, signal handler). The controller manages per-group goroutines **internally** with its own retry logic. A single group failure does NOT kill other groups or the daemon.

- Each group runs in its own goroutine, managed by the controller
- If a group's `listener.Run` returns an error (not context.Canceled), the controller logs and restarts that group after a backoff (2s → 4s → 8s → 30s max)
- If the scale set creation/resolution fails for a group, the controller logs and retries with backoff
- Only if the controller itself returns (all groups failed or context cancelled) does `oklog/run` trigger shutdown of the other top-level actors
- If all groups fail simultaneously, the controller returns an error, the daemon exits, and launchd restarts it

### MacOSScaler

Implements `listener.Scaler`. One instance per group.

**State:**
```go
type MacOSScaler struct {
    logger         *slog.Logger
    manager        *runner.Manager
    scalesetClient *scaleset.Client
    scaleSetID     int
    groupName      string
    maxRunners     int
    minRunners     int

    mu      sync.Mutex
    idle    map[string]*RunnerProcess  // name → process
    busy    map[string]*RunnerProcess  // name → process
}
```

**HandleDesiredRunnerCount(ctx, count) (int, error):**
```
target = min(maxRunners, minRunners + count)
current = len(idle) + len(busy)
if target > current:
    for i in range(target - current):
        startRunner(ctx)
// scale-down handled by HandleJobCompleted
return current runner count
```

Errors during `startRunner` are logged but do NOT return an error (which would kill the listener). Instead, the next `HandleDesiredRunnerCount` call will retry.

**HandleJobStarted(ctx, jobInfo) error:**
- Move runner from idle to busy map by `jobInfo.RunnerName`
- Log: runner name, job display name, workflow run ID
- If runner not found in idle map: log warning, don't error (race condition possible)

**HandleJobCompleted(ctx, jobInfo) error:**
- Remove runner from busy (or idle) map by `jobInfo.RunnerName`
- Kill process: SIGTERM → wait 10s → SIGKILL if still alive
- Remove workdir: `os.RemoveAll`
- Log: runner name, job result, duration (FinishTime - QueueTime)
- If runner not found: log warning, don't error

**startRunner(ctx) error:**
1. Generate unique name: `{groupName}-{randHex8}` (e.g., `qa-runners-a3f2b7c1`)
2. `GenerateJitRunnerConfig(ctx, &RunnerScaleSetJitRunnerSetting{Name: name}, scaleSetID)`
3. Prepare workdir: copy cached runner bits to `{workdirBase}/{groupName}/{name}/`
4. Start process:
   ```
   cmd = exec.CommandContext(ctx, "{workdir}/run.sh")
   cmd.Dir = workdir
   cmd.Env = append(os.Environ(), "ACTIONS_RUNNER_INPUT_JITCONFIG="+jit.EncodedJITConfig)
   cmd.Stdout = logFile  // per-runner log file
   cmd.Stderr = logFile
   cmd.Start()
   ```
5. Write PID file: `{workdir}/.ghr-pid`
6. Add to idle map

**shutdown(ctx):**
- Lock mutex
- Kill all idle + busy runners (SIGTERM → wait → SIGKILL)
- Remove all workdirs
- Clear maps

---

## Runner Manager

Reused and improved from v1. Handles runner binary lifecycle.

**ensureRunnerBits(ctx, version) (string, error):**
1. If version == "latest": resolve via GitHub Releases API (`actions/runner/releases/latest`)
2. Check cache: `{cacheDir}/{version}/` exists → return path
3. Download: `https://github.com/actions/runner/releases/download/v{ver}/actions-runner-osx-{arch}-{ver}.tar.gz`
   - arch = `arm64` if `runtime.GOARCH == "arm64"`, else `x64`
4. Extract tar.gz to cache dir
5. Return path

**prepareWorkdir(cachedDir, workdirBase, groupName, runnerName) (string, error):**
1. `workdir = {workdirBase}/{groupName}/{runnerName}`
2. `os.MkdirAll(workdir, 0o755)`
3. `copyDir(cachedDir, workdir)`
4. Return workdir

**cleanupStale(workdirBase):**
- Walk all group dirs under workdirBase
- For each runner dir: read `.ghr-pid`, check if process alive
- If dead: kill (safety), remove workdir
- Called once at daemon startup before creating scale sets

---

## Config schema (relevant fields)

```yaml
# Auth is NOT in config.yaml — handled by 'ghr login' (see spec 08-auth.md)
# GitHub URL comes from credentials file

github:
  runner_group: "default"                    # optional, default "default"

runner:
  version: "latest"
  cache_dir: "/var/lib/ghr/cache"
  workdir_base: "/var/lib/ghr/runners"

groups:
  - name: "qa-runners"                      # required, unique, becomes scale set name + label
    max_runners: 10                          # required, >= 1
    min_runners: 2                           # optional, default 0
    labels: ["qa", "macos"]                  # optional, additional labels
    version: "2.320.0"                       # optional, overrides global runner.version
  - name: "backend-runners"
    max_runners: 6
    labels: ["backend", "macos"]
```

---

## Startup flow (complete)

1. Load + validate config
2. Load .env if present (godotenv)
3. Load credentials (`auth.Load()`: --token flag → GITHUB_TOKEN env → credentials file → .env). GitHub URL resolution: credentials file URL > config.yaml `github.url` > error.
4. If no credentials found → fatal: `run 'ghr login' to authenticate`
5. Create scaleset.Client using resolved credentials + GitHub URL
6. Resolve runner version (global or per-group overrides), ensure bits are cached
7. Cleanup stale workdirs from previous runs
8. For each group: resolve scale set (see "Scale set resolution" below) → session → listener → scaler
9. Start `oklog/run.Group` with top-level actors:
   - **Controller**: manages all group goroutines internally (see "Per-group goroutine management" below)
   - **Health monitor**: periodic health checks
   - **API server**: Unix socket JSON API at `{state_dir}/ghr.sock` (see spec 00)
   - **Signal handler**: SIGINT/SIGTERM
10. Listen for SIGTERM/SIGINT → trigger shutdown

## Shutdown flow (complete)

1. Cancel root context
2. All listener.Run calls return (context.Canceled)
3. Each scaler.shutdown: kill runners, cleanup workdirs
4. Each session.Close
5. Each DeleteRunnerScaleSet (with context.WithoutCancel)
6. Remove PID file
7. Exit 0
