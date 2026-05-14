# Adapting the Docker Example to macOS Process-Based Runners

## What stays the same (no changes)

Everything from the `scaleset` and `listener` packages is backend-agnostic:

- Client creation (PAT or GitHub App)
- Scale set CRUD
- Message session + listener loop
- `listener.Scaler` interface contract (same 3 methods, same semantics)
- Scaling formula: `target = min(maxRunners, minRunners + count)`
- HandleJobStarted: state transition (idle → busy)
- Signal handling and shutdown flow
- JIT config generation via `GenerateJitRunnerConfig`

## What must change

### 1. Replace Docker with exec.Command

**Docker version:**
```go
c, _ := dockerClient.ContainerCreate(ctx, &container.Config{
    Image: runnerImage,
    User:  "runner",
    Cmd:   []string{"/home/runner/run.sh"},
    Env:   []string{"ACTIONS_RUNNER_INPUT_JITCONFIG=" + jit.EncodedJITConfig},
}, nil, nil, nil, name)
dockerClient.ContainerStart(ctx, c.ID, container.StartOptions{})
```

**macOS version:**
```go
cmd := exec.CommandContext(ctx, filepath.Join(workDir, "run.sh"))
cmd.Dir = workDir
cmd.Env = append(os.Environ(), "ACTIONS_RUNNER_INPUT_JITCONFIG="+jit.EncodedJITConfig)
cmd.Stdout = os.Stdout
cmd.Stderr = os.Stderr
cmd.Start()
```

### 2. Runner state tracking

**Docker version:**
```go
type runnerState struct {
    mu   sync.Mutex
    idle map[string]string  // name → containerID
    busy map[string]string  // name → containerID
}
```

**macOS version:**
```go
type runnerProcess struct {
    cmd     *exec.Cmd
    workDir string
    pid     int
}

type runnerState struct {
    mu   sync.Mutex
    idle map[string]*runnerProcess  // name → process
    busy map[string]*runnerProcess  // name → process
}
```

### 3. HandleJobCompleted — cleanup

**Docker version:**
```go
func (s *Scaler) HandleJobCompleted(ctx context.Context, jobInfo *scaleset.JobCompleted) error {
    containerID := s.runners.markDone(jobInfo.RunnerName)
    return s.dockerClient.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
}
```

**macOS version:**
```go
func (s *Scaler) HandleJobCompleted(ctx context.Context, jobInfo *scaleset.JobCompleted) error {
    proc := s.runners.markDone(jobInfo.RunnerName)
    if proc.cmd.Process != nil {
        _ = proc.cmd.Process.Kill()
        _ = proc.cmd.Wait()
    }
    return os.RemoveAll(proc.workDir)
}
```

### 4. Shutdown

**Docker version:** `ContainerRemove(force: true)` for all containers.

**macOS version:**
```go
func (s *Scaler) shutdown(ctx context.Context) {
    s.runners.mu.Lock()
    defer s.runners.mu.Unlock()
    for name, proc := range s.runners.idle {
        _ = proc.cmd.Process.Kill()
        _ = proc.cmd.Wait()
        _ = os.RemoveAll(proc.workDir)
    }
    for name, proc := range s.runners.busy {
        _ = proc.cmd.Process.Kill()
        _ = proc.cmd.Wait()
        _ = os.RemoveAll(proc.workDir)
    }
    clear(s.runners.idle)
    clear(s.runners.busy)
}
```

### 5. Runner binary management

Docker has the runner inside the image. On macOS you need:

```go
// Download + extract once at startup
func (m *Manager) ensureRunnerBits(ctx context.Context, version string) (string, error) {
    // Resolve "latest" → actual version via GitHub Releases API
    // Download https://github.com/actions/runner/releases/download/v{ver}/actions-runner-osx-{arch}-{ver}.tar.gz
    // Extract to cacheDir/{version}/
    // Return path to extracted directory
}

// Copy cached bits to each runner's workdir
func (m *Manager) prepareWorkdir(baseDir, runnerID string) (string, error) {
    workDir := filepath.Join(baseDir, runnerID)
    os.MkdirAll(workDir, 0o755)
    copyDir(cachedRunnerDir, workDir)
    return workDir, nil
}
```

### 6. Config changes

Remove:
- `RunnerImage` field

Add:
- `RunnerVersion` (string: "latest" or pinned like "2.330.0")
- `CacheDir` (path for cached runner binaries)
- `WorkdirBase` (base path for runner workdirs)

## Complete startRunner for macOS

```go
func (s *Scaler) startRunner(ctx context.Context) (string, error) {
    name := fmt.Sprintf("runner-%s", randHex(4))

    // 1. Generate JIT config
    jit, err := s.scalesetClient.GenerateJitRunnerConfig(ctx,
        &scaleset.RunnerScaleSetJitRunnerSetting{Name: name},
        s.scaleSetID,
    )
    if err != nil {
        return "", fmt.Errorf("generate JIT config: %w", err)
    }

    // 2. Prepare workdir (copy cached runner bits)
    workDir, err := s.manager.prepareWorkdir(s.workdirBase, name)
    if err != nil {
        return "", fmt.Errorf("prepare workdir: %w", err)
    }

    // 3. Start runner process with JIT config
    cmd := exec.CommandContext(ctx, filepath.Join(workDir, "run.sh"))
    cmd.Dir = workDir
    cmd.Env = append(os.Environ(), "ACTIONS_RUNNER_INPUT_JITCONFIG="+jit.EncodedJITConfig)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr

    if err := cmd.Start(); err != nil {
        _ = os.RemoveAll(workDir)
        return "", fmt.Errorf("start runner: %w", err)
    }

    // 4. Track
    s.runners.addIdle(name, &runnerProcess{
        cmd:     cmd,
        workDir: workDir,
        pid:     cmd.Process.Pid,
    })

    return name, nil
}
```

## Architecture comparison

| Layer | Docker | macOS |
|---|---|---|
| Runner backend | Container | exec.Cmd process |
| Config delivery | JITCONFIG env var | Same env var |
| State tracking | name → containerID | name → *runnerProcess |
| Scale up | ContainerCreate + Start | exec.Command + Start |
| Scale down | ContainerRemove(force) | Kill + Wait + RemoveAll |
| Shutdown | Force remove all | Kill all + cleanup dirs |
| Isolation | Container | Filesystem (workdirs) |
| Runner binary | Inside Docker image | Downloaded + cached |
