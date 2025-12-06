# gh-runners-tool

GitHub Actions runner controller for macOS that keeps a target number of runners per group, favors ephemeral runners, and cleans up aggressively to avoid zombie workdirs or GitHub “offline” entries.

## Table of Contents
- [Features](#features)
- [Architecture](#architecture)
- [Requirements](#requirements)
- [Quickstart](#quickstart)
- [Configuration](#configuration)
  - [Environment](#environment)
  - [config.yaml](#configyaml)
- [Commands](#commands)
- [Paths and Files](#paths-and-files)
- [Runner Lifecycle and Cleanup](#runner-lifecycle-and-cleanup)
- [Operational Notes](#operational-notes)
- [Troubleshooting](#troubleshooting)
- [Development](#development)
- [Limitations](#limitations)

## Features
- self-hosted runner management without containers.
- Group-based desired state: name, count, labels, ephemeral flag, per-group workdir/version overrides.
- Daemon with reconciliation loop that reacts to runner exits and also runs on a periodic safety interval.
- SIGHUP-driven config reload (`ghr apply`) without restarting the daemon.
- Aggressive cleanup of stale workdirs, processes, and GitHub runner registrations that match configured group prefixes.
- `.env`-based secrets; YAML config for desired state; defaults for paths/version.

## Architecture
High-level design, data paths, and control flow are documented in `docs/ARCHITECTURE.md`. Read it for package responsibilities and lifecycle details.

## Requirements
- macOS with `bash` available.
- Go 1.21+ to build (module targets Go 1.24.4).
- Network access to `github.com` to download runner binaries and fetch registration tokens.
- GitHub PAT with runner registration permissions exported as `GITHUB_TOKEN` (or `GITHUB_PAT`) via `.env`.

## Quickstart
```bash
git clone <this-repo>
cd gh-runners-tool
cp config.example.yaml config.yaml
cp env.example .env   # edit with your PAT
go build ./cmd/ghr

# launch daemon (foreground)
./ghr daemon --config config.yaml --interval 15s
```

## Configuration
### Environment
`.env` is mandatory for authentication and stays out of version control.
```
GITHUB_TOKEN=YOUR_GITHUB_PAT_WITH_RUNNER_PERMS
# or
GITHUB_PAT=YOUR_GITHUB_PAT_WITH_RUNNER_PERMS
```
Security: keep `.env` in `.gitignore` and prefer a secrets manager for production.

### config.yaml
Define the desired state for runners.
```yaml
github:
  scope: org            # org | repo
  owner: your-org
  # repo: your-repo     # required when scope=repo
defaults:
  workdir_base: /var/lib/ghr/groups
  cache_dir: /var/lib/ghr/cache
  version: latest       # or a pinned runner version
groups:
  - name: deploy-api
    count: 10
    ephemeral: true
    labels: [deploy, macos]
    # workdir_base: /custom/path/deploy-api  # optional override
    # version: 2.319.1                       # optional override
```
Validation rules:
- `github.scope` must be `org` or `repo`; `github.owner` is required; `github.repo` required when `scope=repo`.
- At least one group is required; `count` must be `>= 0`; `name` must be set.
- Defaults fill missing `workdir_base`, `cache_dir`, and `version`. Groups inherit defaults when not overridden.

## Commands
- `ghr daemon`
  Starts the controller. Flags: `--config` (default `config.yaml`), `--interval` (default `15s`). Writes a pid file and reloads config on `SIGHUP`.
- `ghr apply`
  Validates config and sends `SIGHUP` to the running daemon for zero-downtime reload.
- `ghr status`
  Prints the daemon pid if the pid file exists.
- `ghr purge`
  Deletes all self-hosted runners for the configured scope, waiting for busy runners to go idle. Flags: `--timeout` (default `5m`), `--interval` (default `5s`).

Examples:
```bash
./ghr daemon --config config.yaml --interval 10s
./ghr apply --config config.yaml
./ghr status
./ghr purge --config config.yaml --timeout 10m --interval 10s
```

## Paths and Files
- Workdirs: `/var/lib/ghr/groups/<group>/<id>` by default; per-group overrides supported.
- Cache: `/var/lib/ghr/cache` by default (shared runner bits).
- State / pid file: `/var/lib/ghr/state/daemon.pid`; falls back to `$HOME/.local/state/ghr` if `/var/lib` is not writable.

## Runner Lifecycle and Cleanup
- Resolve runner version (`latest` or pinned), download/cache if missing.
- Copy cached bits to a fresh per-runner workdir; run `config.sh --unattended` with labels and optional `--ephemeral`; start `run.sh`.
- Reconciler observes exits and recreates ephemeral runners to maintain target counts; also runs on the configured interval.
- On startup, daemon kills stray runner processes found in configured workdir bases, then removes workdirs.
- On shutdown, daemon stops runners, deregisters them from GitHub, and removes workdirs.
- Startup and shutdown include GitHub cleanup of runner registrations with names prefixed by configured group names.

## Operational Notes
- Keep the daemon running (foreground or under a supervisor like `launchd`); stopping it stops runners.
- Reload configuration with `ghr apply` (SIGHUP); no restart required.
- Without containers, jobs run directly on the host. Use dedicated machines you trust and ensure filesystem permissions allow runner cleanup.

## Troubleshooting
- `GITHUB_TOKEN (or GITHUB_PAT) is required` → ensure `.env` is present and exported in the environment that launches the daemon.
- Permission denied on `/var/lib/ghr/...` → override `workdir_base`, `cache_dir`, and `state` to user-writable paths.
- Runners remain “offline” in GitHub → run `ghr purge` to delete registrations; daemon also cleans prefixed registrations on startup/shutdown.
- Download/registration errors → inspect daemon stdout logs; verify network access to `github.com`.

## Development
- Build: `go build ./cmd/ghr`
- Lint/format: use `gofmt` and standard Go tooling.
- Tests: `go test ./...` (add tests as features evolve).

## Limitations
- macOS only; no container isolation; Linux not supported yet.
- No HTTP health/metrics endpoint (planned later).
- Persistent groups restart on daemon restart; existing jobs may be terminated by cleanup policy.
