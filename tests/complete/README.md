# Complete Test

Full end-to-end test of all ghr v2 features with 4 groups, 20 jobs, and all edge cases.

## What is tested

### Scale set management
- 4 scale sets created at startup
- Scale sets deleted on shutdown (Ctrl+C)
- Per-group health override (ghr-deploy: runner_timeout=10m)

### Scaling behavior
- Scale-up from 0 to max (ghr-heavy: 0 -> 2)
- Pre-provisioned idle runner (ghr-fast: min=1, ghr-single: min=1)
- Scale-up to max under load (ghr-fast: 1 -> 3)
- Job queuing when max reached (ghr-fast 4th job waits)
- Scale-down after job completion (ephemeral runners)
- Second wave of jobs after first batch completes
- Sequential enforcement with max=1 (ghr-deploy: 3 jobs one after another)
- Always-on min=max=1 (ghr-single: runner always available)

### Runner lifecycle
- Runner provisioned (workdir copy, JIT config, process start)
- Job started (idle -> busy transition)
- Job completed success (stop, cleanup workdir)
- Job completed failure (runner.failed event, cleanup still happens)
- Instant job (fast provision/cleanup cycle)
- Multi-step job (steps share runner)
- High stdout output (100 lines of payload)

### Health monitoring (check_interval=10s)
- Runner liveness checks (kill -0 on PIDs)
- Runner timeout detection (runner_timeout=5m, won't trigger in test)
- Idle timeout (idle_timeout=2m, triggers on min_runners idle runners after all jobs done)
- Disk space check (min_disk_space=500MB)
- Health issues -> notification events

### Notifications (Discord)
- runner.failed event sent when edge-fail job fails
- health.* events sent on any health issue
- daemon.start / daemon.stop events

### Monitoring (Uptime Kuma)
- Daemon health push every check_interval (10s)
- Per-group health push (4 groups, 4 push tokens)
- Degraded threshold at 0.5

### Logging
- Daemon log: {log_dir}/daemon/{date}.json
- Group logs: {log_dir}/groups/{group}/{date}.json (4 groups)
- Runner logs: {log_dir}/groups/{group}/runners/{runner}/{date}.json
- Console output in text format with debug level
- Runner stdout captured in runner log files

### Shutdown
- Ctrl+C triggers graceful shutdown
- All idle runners killed
- All workdirs cleaned
- All scale sets deleted
- PID file removed
- State file removed
- Socket removed
- No orphan processes

## Setup

1. Copy `env.example` to `.env` and fill in your values
2. Edit `config.yaml` and set `github.url` to your org
3. Run:

```bash
cd tests/complete
cp env.example .env
# Edit .env with your Discord webhook + Uptime Kuma URLs

ghr run --config config.yaml --log-level debug
```

4. Copy `workflow.yml` to `.github/workflows/test-ghr-complete.yml` in your repo
5. Trigger from GitHub Actions > "Run workflow"

## Verification checklist

After the workflow completes:

- [ ] All 20 jobs completed in GitHub Actions (19 success, 1 failure)
- [ ] ghr-fast scaled to 3 runners concurrently
- [ ] ghr-heavy scaled to 2 runners concurrently
- [ ] ghr-deploy ran 3 jobs sequentially (max=1)
- [ ] ghr-single had pre-provisioned runner at startup
- [ ] edge-fail shows `result=failed` in ghr logs
- [ ] Discord received a notification for the failed job
- [ ] Uptime Kuma shows pushes for daemon + 4 groups

After Ctrl+C:

- [ ] No runner processes remain (`ps aux | grep Runner.Listener`)
- [ ] Workdirs empty (`ls ~/.local/share/ghr/runners/`)
- [ ] No PID file (`ls ~/.local/state/ghr/daemon.pid`)
- [ ] No socket (`ls ~/.local/state/ghr/ghr.sock`)
- [ ] Log files exist with structured JSON entries

After waiting 2+ minutes idle (before Ctrl+C):

- [ ] Idle runners killed by health monitor (idle_timeout=2m)
- [ ] min_runners runners re-provisioned after idle kill
