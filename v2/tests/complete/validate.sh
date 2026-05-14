#!/bin/bash
set -uo pipefail

LOG_DIR="${GHR_LOG_DIR:-$HOME/.local/share/ghr/logs}"
STATE_DIR="${GHR_STATE_DIR:-$HOME/.local/state/ghr}"
RUNNER_DIR="${GHR_RUNNER_DIR:-$HOME/.local/share/ghr/runners}"
PASS=0
FAIL=0
WARN=0

pass() { PASS=$((PASS + 1)); printf "  \033[32m✓\033[0m %s\n" "$1"; }
fail() { FAIL=$((FAIL + 1)); printf "  \033[31m✗\033[0m %s\n" "$1"; }
warn() { WARN=$((WARN + 1)); printf "  \033[33m!\033[0m %s\n" "$1"; }
section() { printf "\n\033[1m%s\033[0m\n" "$1"; }

TODAY=$(date +%Y-%m-%d)
DAEMON_LOG="$LOG_DIR/daemon/$TODAY.json"

if [ ! -f "$DAEMON_LOG" ]; then
    echo "ERROR: Daemon log not found at $DAEMON_LOG"
    echo "Set GHR_LOG_DIR if logs are elsewhere."
    exit 1
fi

section "=== Scale Set Management ==="

GROUPS_STARTED=$(grep -c '"group listener started"' "$DAEMON_LOG" 2>/dev/null || echo 0)
if [ "$GROUPS_STARTED" -ge 4 ]; then pass "4 groups started ($GROUPS_STARTED listeners)"
else fail "Expected 4 groups, got $GROUPS_STARTED"; fi

for g in ghr-fast ghr-heavy ghr-deploy ghr-single; do
    if grep -q "\"group\":\"$g\"" "$DAEMON_LOG" 2>/dev/null; then
        pass "Group $g active"
    else
        fail "Group $g not found in logs"
    fi
done

section "=== Runner Provisioning ==="

TOTAL_PROVISIONED=$(grep -c '"runner provisioned"' "$DAEMON_LOG" 2>/dev/null || echo 0)
pass "Total runners provisioned: $TOTAL_PROVISIONED"

for g in ghr-fast ghr-heavy ghr-deploy ghr-single; do
    GROUP_LOG="$LOG_DIR/groups/$g/$TODAY.json"
    if [ -f "$GROUP_LOG" ]; then
        COUNT=$(grep -c '"runner provisioned"' "$GROUP_LOG" 2>/dev/null || echo 0)
        pass "  $g: $COUNT runners provisioned"
    else
        fail "  $g: no group log found"
    fi
done

FAST_PROVISIONED=$(grep '"runner provisioned"' "$DAEMON_LOG" 2>/dev/null | grep -c '"group":"ghr-fast"' || echo 0)
if [ "$FAST_PROVISIONED" -ge 3 ]; then pass "ghr-fast scaled to 3+ runners"
else fail "ghr-fast only scaled to $FAST_PROVISIONED (expected >=3)"; fi

section "=== Min Runners (Pre-provisioned) ==="

DAEMON_START=$(grep '"ghr starting"' "$DAEMON_LOG" | head -1 | jq -r '.time' 2>/dev/null || echo "")
FIRST_LISTENER=$(grep '"group listener started"' "$DAEMON_LOG" | head -1 | jq -r '.time' 2>/dev/null || echo "")

FAST_FIRST=$(grep '"runner provisioned"' "$DAEMON_LOG" | grep '"group":"ghr-fast"' | head -1 | jq -r '.time' 2>/dev/null || echo "")
FAST_FIRST_JOB=$(grep '"job started"' "$DAEMON_LOG" | grep '"group":"ghr-fast"' | head -1 | jq -r '.time' 2>/dev/null || echo "")

if [ -n "$FAST_FIRST" ] && [ -n "$FAST_FIRST_JOB" ]; then
    if [[ "$FAST_FIRST" < "$FAST_FIRST_JOB" ]]; then
        pass "ghr-fast: runner provisioned BEFORE first job (min_runners=1)"
    else
        fail "ghr-fast: runner provisioned AFTER first job"
    fi
else
    warn "Cannot determine min_runners timing"
fi

section "=== Job Execution ==="

JOBS_STARTED=$(grep -c '"job started"' "$DAEMON_LOG" 2>/dev/null || echo 0)
JOBS_COMPLETED=$(grep -c '"job completed"' "$DAEMON_LOG" 2>/dev/null || echo 0)
JOBS_SUCCEEDED=$(grep '"job completed"' "$DAEMON_LOG" 2>/dev/null | grep -c '"result":"succeeded"' || echo 0)
JOBS_FAILED=$(grep '"job completed"' "$DAEMON_LOG" 2>/dev/null | grep -c '"result":"failed"' || echo 0)

pass "Jobs started: $JOBS_STARTED"
pass "Jobs completed: $JOBS_COMPLETED"
pass "  Succeeded: $JOBS_SUCCEEDED"
pass "  Failed: $JOBS_FAILED"

if [ "$JOBS_COMPLETED" -ge 18 ]; then pass "Enough jobs completed (>= 18)"
else fail "Only $JOBS_COMPLETED jobs completed (expected >= 18)"; fi

if [ "$JOBS_FAILED" -ge 1 ]; then pass "At least 1 failed job detected (edge-fail)"
else fail "No failed job detected"; fi

section "=== Concurrency ==="

FAST_LOG="$LOG_DIR/groups/ghr-fast/$TODAY.json"
if [ -f "$FAST_LOG" ]; then
    CONCURRENT=$(grep '"runner provisioned"' "$FAST_LOG" | head -3 | jq -r '.time[:19]' 2>/dev/null | sort -u | wc -l | tr -d ' ')
    if [ "$CONCURRENT" -le 2 ]; then
        pass "ghr-fast: 3 runners provisioned within same time window"
    else
        warn "ghr-fast: runners provisioned across $CONCURRENT distinct seconds"
    fi
fi

HEAVY_LOG="$LOG_DIR/groups/ghr-heavy/$TODAY.json"
if [ -f "$HEAVY_LOG" ]; then
    HEAVY_PROV=$(grep -c '"runner provisioned"' "$HEAVY_LOG" 2>/dev/null || echo 0)
    if [ "$HEAVY_PROV" -ge 2 ]; then pass "ghr-heavy: scaled to 2 runners"
    else fail "ghr-heavy: only $HEAVY_PROV runners (expected >=2)"; fi
fi

section "=== Sequential Enforcement (ghr-deploy max=1) ==="

DEPLOY_LOG="$LOG_DIR/groups/ghr-deploy/$TODAY.json"
if [ -f "$DEPLOY_LOG" ]; then
    DEPLOY_JOBS=$(grep -c '"job started"' "$DEPLOY_LOG" 2>/dev/null || echo 0)
    DEPLOY_RUNNERS=$(grep '"runner provisioned"' "$DEPLOY_LOG" | jq -r '.runner' 2>/dev/null | sort -u | wc -l | tr -d ' ')
    pass "ghr-deploy: $DEPLOY_JOBS jobs across $DEPLOY_RUNNERS unique runners"
    if [ "$DEPLOY_JOBS" -ge 3 ]; then pass "ghr-deploy: all 3 deploy jobs ran"
    else fail "ghr-deploy: only $DEPLOY_JOBS jobs (expected 3)"; fi
fi

section "=== Job Failure Handling ==="

FAILED_RUNNER=$(grep '"job completed"' "$DAEMON_LOG" | grep '"result":"failed"' | head -1 | jq -r '.runner' 2>/dev/null || echo "")
if [ -n "$FAILED_RUNNER" ]; then
    pass "Failed job runner identified: $FAILED_RUNNER"
    if grep -q "\"runner\":\"$FAILED_RUNNER\".*stopping" "$DAEMON_LOG" 2>/dev/null || \
       grep -q "stopping.*\"runner\":\"$FAILED_RUNNER\"" "$DAEMON_LOG" 2>/dev/null; then
        pass "Failed runner was stopped and cleaned"
    else
        warn "Cannot confirm failed runner cleanup in logs"
    fi
else
    fail "No failed job runner found"
fi

section "=== Runner Log Files ==="

RUNNER_LOG_COUNT=$(find "$LOG_DIR/groups" -path "*/runners/*/$TODAY.json" -type f 2>/dev/null | wc -l | tr -d ' ')
pass "Runner log files created: $RUNNER_LOG_COUNT"

for g in ghr-fast ghr-heavy ghr-deploy ghr-single; do
    GROUP_RUNNERS=$(find "$LOG_DIR/groups/$g/runners" -name "$TODAY.json" -type f 2>/dev/null | wc -l | tr -d ' ')
    pass "  $g: $GROUP_RUNNERS runner logs"
done

section "=== Duration Stats ==="

if grep -q '"duration"' "$DAEMON_LOG" 2>/dev/null; then
    pass "Job durations logged"
    echo "    Durations:"
    grep '"job completed"' "$DAEMON_LOG" | jq -r '  "    " + .runner + ": " + (.duration // "n/a")' 2>/dev/null | head -10
else
    warn "No duration data in logs"
fi

section "=== Cleanup State ==="

ORPHAN_PROCS=$(pgrep -f "Runner.Listener" 2>/dev/null | wc -l | tr -d ' ')
if [ "$ORPHAN_PROCS" -eq 0 ]; then pass "No orphan runner processes"
else fail "$ORPHAN_PROCS orphan processes found"; fi

WORKDIR_CONTENT=$(find "$RUNNER_DIR" -mindepth 2 -maxdepth 2 -type d 2>/dev/null | wc -l | tr -d ' ')
if [ "$WORKDIR_CONTENT" -eq 0 ]; then pass "All runner workdirs cleaned"
else fail "$WORKDIR_CONTENT workdirs remain"; fi

if [ ! -f "$STATE_DIR/daemon.pid" ]; then pass "PID file removed"
else fail "PID file still exists"; fi

if [ ! -S "$STATE_DIR/ghr.sock" ]; then pass "Socket removed"
else fail "Socket still exists"; fi

section "=== Log Structure ==="

if [ -f "$LOG_DIR/daemon/$TODAY.json" ]; then pass "Daemon log exists"
else fail "Daemon log missing"; fi

for g in ghr-fast ghr-heavy ghr-deploy ghr-single; do
    if [ -f "$LOG_DIR/groups/$g/$TODAY.json" ]; then pass "Group log $g exists"
    else fail "Group log $g missing"; fi
done

DAEMON_LINES=$(wc -l < "$DAEMON_LOG" | tr -d ' ')
pass "Daemon log entries: $DAEMON_LINES"

section "=== Notifications ==="

NOTIF_EVENTS=$(grep '"runner.failed"\|"runner.started"\|"daemon.start"' "$DAEMON_LOG" 2>/dev/null | wc -l | tr -d ' ')
if [ "$NOTIF_EVENTS" -ge 1 ]; then pass "Notification events emitted: $NOTIF_EVENTS"
else warn "No notification events found in daemon log"; fi

section "========================================="
printf "\033[1m  Results: \033[32m%d passed\033[0m, \033[31m%d failed\033[0m, \033[33m%d warnings\033[0m\n" "$PASS" "$FAIL" "$WARN"
section "========================================="

if [ "$FAIL" -gt 0 ]; then exit 1; fi
exit 0
