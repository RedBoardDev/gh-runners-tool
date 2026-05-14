---
name: scaleset-sdk
description: Build custom GitHub Actions runner autoscalers using the official actions/scaleset Go SDK. Use this skill whenever working with GitHub Actions Runner Scale Sets, implementing the Scaler interface, configuring JIT runners, managing scale set sessions, or building self-hosted runner infrastructure (including ghr). Triggers on any code importing "github.com/actions/scaleset", any mention of scale sets, JIT runner config, runner autoscaling, or self-hosted runner management with Go. Also use when debugging scale set authentication, message polling, or runner lifecycle issues.
---

# GitHub Actions Runner Scale Set SDK

Complete reference for building custom autoscaling solutions with `github.com/actions/scaleset`.

## When to use this skill

- Writing Go code that imports `github.com/actions/scaleset` or `github.com/actions/scaleset/listener`
- Implementing the `listener.Scaler` interface
- Building a custom runner backend (process, VM, container)
- Debugging scale set auth, polling, or runner lifecycle
- Working on ghr (GitHub runner controller for macOS)

## Quick start pattern

Every scale set autoscaler follows this skeleton:

```go
// 1. Create client (PAT or GitHub App)
client, _ := scaleset.NewClientWithPersonalAccessToken(scaleset.NewClientWithPersonalAccessTokenConfig{
    GitHubConfigURL:     "https://github.com/my-org",
    PersonalAccessToken: token,
    SystemInfo:          scaleset.SystemInfo{System: "ghr", Version: "1.0"},
})

// 2. Create or get scale set
scaleSet, _ := client.CreateRunnerScaleSet(ctx, &scaleset.RunnerScaleSet{
    Name:          "my-runners",        // this IS the runs-on: label
    RunnerGroupID: 1,                   // 1 = "default"
    Labels:        []scaleset.Label{{Type: "System", Name: "my-runners"}},
    RunnerSetting: scaleset.RunnerSetting{DisableUpdate: true},
})
defer client.DeleteRunnerScaleSet(context.WithoutCancel(ctx), scaleSet.ID)

// 3. Open message session
sessionClient, _ := client.MessageSessionClient(ctx, scaleSet.ID, hostname)
defer sessionClient.Close(context.Background())

// 4. Create listener + run with your Scaler
l, _ := listener.New(sessionClient, listener.Config{
    ScaleSetID: scaleSet.ID,
    MaxRunners: 15,
})
l.Run(ctx, &MyScaler{})  // blocks until ctx cancelled or error
```

## The Scaler interface (the only thing you implement)

```go
type Scaler interface {
    HandleDesiredRunnerCount(ctx context.Context, count int) (int, error)
    HandleJobStarted(ctx context.Context, jobInfo *scaleset.JobStarted) error
    HandleJobCompleted(ctx context.Context, jobInfo *scaleset.JobCompleted) error
}
```

### HandleDesiredRunnerCount(ctx, count) (int, error)

- `count` = `statistics.TotalAssignedJobs` (jobs needing runners RIGHT NOW)
- Called VERY frequently: at init, after every message, after every long-poll timeout (~50s)
- Return the actual runner count you scaled to (used for metrics only)
- Any error terminates `Run()`
- Scaling formula from the reference example: `target = min(maxRunners, minRunners + count)`
- Scale-down is NOT done here — it happens in `HandleJobCompleted`

### HandleJobStarted(ctx, jobInfo) error

- Mark the runner as busy (bookkeeping). No scaling action needed.
- `jobInfo.RunnerName` identifies which runner got the job.
- Any error terminates `Run()`

### HandleJobCompleted(ctx, jobInfo) error

- THIS is where scale-down happens: destroy the runner process/container/VM + cleanup workdir.
- `jobInfo.RunnerName` identifies which runner to destroy.
- `jobInfo.Result`: `"Succeeded"`, `"Failed"`, or `"Cancelled"` (cancelled = job reassignment, not a real completion)
- Any error terminates `Run()`

### Processing order within a single message batch

1. AcquireJobs (automatic, not exposed to Scaler)
2. All HandleJobStarted calls
3. All HandleJobCompleted calls
4. HandleDesiredRunnerCount

JobCompleted runs BEFORE HandleDesiredRunnerCount. This is why the count naturally decreases after runners are cleaned up.

## JIT Runner Config (replaces config.sh)

```go
jit, _ := scalesetClient.GenerateJitRunnerConfig(ctx,
    &scaleset.RunnerScaleSetJitRunnerSetting{Name: "runner-abc123"},
    scaleSetID,
)
// jit.EncodedJITConfig is a base64 blob — treat as SECRET until consumed
```

The runner binary reads the JIT config from an env var instead of needing `config.sh`:

```go
cmd := exec.Command("./run.sh")
cmd.Env = append(os.Environ(), "ACTIONS_RUNNER_INPUT_JITCONFIG="+jit.EncodedJITConfig)
cmd.Start()
```

No `config.sh` step needed. No registration token management. The JIT config contains everything.

## Authentication

Read `references/api-reference.md` section "Authentication" for the full flow. Summary:

- **GitHub App (recommended)**: `ClientID` + `InstallationID` + `PrivateKey` (PEM). Auto-rotates tokens.
- **PAT**: simpler, broader scope. Pass as `PersonalAccessToken`.
- Token exchange is automatic: PAT/App -> registration token -> admin token. Refresh is transparent (60s before expiry).

## Key design facts

1. **Scale set name = workflow label**. `runs-on: my-scale-set` targets the scale set named `my-scale-set`.
2. **Runners are ephemeral by default**. One job, then removed.
3. **Long-polling, not interval polling**. `GetMessage` blocks up to ~50s. React instantly to new jobs.
4. **Message ack is optimistic**. Messages are deleted BEFORE your Scaler processes them.
5. **`handleMessage` uses `context.WithoutCancel`**. Even during shutdown, message processing completes.
6. **Scale set is deleted on daemon shutdown** (`defer DeleteRunnerScaleSet`). Clean state on restart.
7. **Session token refresh is automatic**. 401 -> refresh -> retry (once). Transparent to your code.
8. **Any Scaler error kills the listener loop**. Handle transient errors inside your Scaler.
9. **SetMaxRunners is thread-safe**. Call it anytime to adjust capacity dynamically.
10. **Go 1.25+ required**.

## Reference docs

For detailed API signatures, types, error handling, and endpoint maps, read:
- `references/api-reference.md` — Complete SDK reference (types, methods, auth, errors, endpoints)
- `references/macos-adaptation.md` — How to adapt the Docker example to macOS process-based runners
