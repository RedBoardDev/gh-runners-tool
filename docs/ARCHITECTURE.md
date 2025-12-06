# Architecture Overview

## Packages
- `cmd/ghr`: entrypoint, wires CLI.
- `internal/cli`: cobra commands (`daemon`, `apply`, `status`), config flag handling, pid file utilities.
- `internal/config`: YAML + `.env` loading/validation, defaults for paths/version.
- `internal/domain`: core domain structs for groups and runner instances.
- `internal/provider/github`: GitHub API client for runner registration tokens.
- `internal/runner`: runner lifecycle (download cache, per-runner copy, configure, launch, cleanup).
- `internal/reconciler`: converges desired groups to running runners; watches exits and scales up/down.
- `internal/logging`: basic stdout logger.

## Data Paths
- Cache: `/var/lib/ghr/cache` (runner archives/extracted bits).
- Workdirs: `/var/lib/ghr/groups/<group>/<id>` (per runner, cleaned on exit).
- State (pid): `/var/lib/ghr/state/daemon.pid`.
- Runner pid files: `<workdir>/.ghr-pid` (used for cleanup on startup).

## Control Flow
1. `ghr daemon --config config.yaml` loads config, creates GitHub client + runner manager + reconciler.
2. Daemon writes pid file, starts reconcile loop on interval (default 15s).
3. SIGHUP triggers config reload; reconcile loop also reaps finished runners and recreates ephemerals to maintain counts.
4. `ghr apply` validates config and sends SIGHUP to daemon to reload.
5. On startup, daemon calls runner cleanup to kill any stray processes found in configured workdir bases and removes their workdirs to avoid accumulation.

## Runner Lifecycle
1. Resolve runner version (`latest` via GitHub releases) and download/archive cache if missing.
2. Copy cached bits to a fresh workdir per runner; run `config.sh --unattended --url ... --token ... [--labels] [--ephemeral]`.
3. Start `run.sh`; wait/observe exit; cleanup workdir after exit.
4. Reconciler detects exits and scales replacements for ephemeral groups to keep target counts.

## Security Notes
- Tokens only via env (`GITHUB_TOKEN`/`GITHUB_PAT`), never in config.
- Cleanup removes workdirs after runner exit; no per-group users to keep complexity low.
- macOS-only target; Linux best-effort later.

