# Go Expert — Full Pattern Reference

## Table of Contents

1. [Goroutine lifecycle management](#1-goroutine-lifecycle)
2. [Context propagation](#2-context-propagation)
3. [Error handling patterns](#3-error-handling)
4. [HTTP client patterns](#4-http-client)
5. [Process management](#5-process-management)
6. [Structured logging (slog)](#6-structured-logging)
7. [Testing patterns](#7-testing)
8. [Configuration loading](#8-configuration)
9. [Concurrency patterns](#9-concurrency)
10. [File system operations](#10-filesystem)

---

## 1. Goroutine lifecycle

### oklog/run for daemon actors

```go
import "github.com/oklog/run"

var g run.Group

// Actor: long-running service
{
    ctx, cancel := context.WithCancel(context.Background())
    g.Add(
        func() error { return myService.Run(ctx) },  // execute
        func(error) { cancel() },                      // interrupt
    )
}

// Actor: signal handler
{
    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    g.Add(
        func() error { <-ctx.Done(); return nil },
        func(error) { cancel() },
    )
}

// When ANY actor returns, ALL others are interrupted via their interrupt func.
if err := g.Run(); err != nil {
    log.Fatal(err)
}
```

### errgroup for bounded parallel work

```go
import "golang.org/x/sync/errgroup"

g, ctx := errgroup.WithContext(ctx)
g.SetLimit(10) // max 10 concurrent

for _, item := range items {
    g.Go(func() error {
        return process(ctx, item)
    })
}
if err := g.Wait(); err != nil {
    return err
}
```

### Worker pool with backpressure

```go
type Pool struct {
    sem chan struct{}
    wg  sync.WaitGroup
}

func NewPool(size int) *Pool {
    return &Pool{sem: make(chan struct{}, size)}
}

func (p *Pool) Go(fn func()) {
    p.wg.Add(1)
    p.sem <- struct{}{} // blocks if pool is full
    go func() {
        defer p.wg.Done()
        defer func() { <-p.sem }()
        fn()
    }()
}

func (p *Pool) Wait() { p.wg.Wait() }
```

---

## 2. Context propagation

### Always pass context, never store it

```go
// YES
func (s *Service) Process(ctx context.Context, id string) error { ... }

// NO — storing context in a struct
type Service struct {
    ctx context.Context  // don't do this
}
```

### context.WithoutCancel for cleanup operations

```go
// Cleanup must complete even if parent context is cancelled
func (s *Service) Shutdown(ctx context.Context) {
    cleanupCtx := context.WithoutCancel(ctx)
    // or: cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    s.cleanup(cleanupCtx)
}
```

### Timeout per operation

```go
ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
defer cancel()
resp, err := client.Do(req.WithContext(ctx))
```

---

## 3. Error handling

### Sentinel errors

```go
var (
    ErrNotFound    = errors.New("not found")
    ErrConflict    = errors.New("conflict")
    ErrTimeout     = errors.New("timeout")
)

// Usage:
if errors.Is(err, ErrNotFound) { ... }
```

### Wrapping with context

```go
func (s *Service) GetUser(ctx context.Context, id string) (*User, error) {
    user, err := s.store.Get(ctx, id)
    if err != nil {
        return nil, fmt.Errorf("get user %s: %w", id, err)
    }
    return user, nil
}
```

### Custom error types

```go
type ValidationError struct {
    Field   string
    Message string
}

func (e *ValidationError) Error() string {
    return fmt.Sprintf("validation: %s: %s", e.Field, e.Message)
}

// Check:
var ve *ValidationError
if errors.As(err, &ve) {
    log.Printf("field %s: %s", ve.Field, ve.Message)
}
```

---

## 4. HTTP client

### Client with timeout and retry

```go
client := &http.Client{
    Timeout: 30 * time.Second,
    Transport: &http.Transport{
        MaxIdleConns:        100,
        MaxIdleConnsPerHost: 10,
        IdleConnTimeout:     90 * time.Second,
    },
}
```

### Request with context

```go
req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
if err != nil {
    return fmt.Errorf("build request: %w", err)
}
req.Header.Set("Authorization", "Bearer "+token)
req.Header.Set("Accept", "application/json")

resp, err := client.Do(req)
if err != nil {
    return fmt.Errorf("request: %w", err)
}
defer resp.Body.Close()

if resp.StatusCode >= 300 {
    body, _ := io.ReadAll(resp.Body)
    return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
}
```

### Exponential backoff with jitter

```go
func backoff(attempt int, base, max time.Duration) time.Duration {
    d := base * time.Duration(1<<uint(attempt))
    if d > max {
        d = max
    }
    jitter := time.Duration(rand.Int63n(int64(d / 5)))
    return d + jitter - d/10
}
```

---

## 5. Process management

### Start with PID tracking

```go
cmd := exec.CommandContext(ctx, binPath)
cmd.Dir = workDir
cmd.Env = append(os.Environ(), envVars...)
cmd.Stdout = logFile
cmd.Stderr = logFile

if err := cmd.Start(); err != nil {
    return fmt.Errorf("start: %w", err)
}

// Write PID file
pidPath := filepath.Join(workDir, ".pid")
os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644)
```

### Graceful stop (SIGTERM → wait → SIGKILL)

```go
func stopProcess(cmd *exec.Cmd, timeout time.Duration) error {
    if cmd.Process == nil {
        return nil
    }
    if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
        return cmd.Process.Kill()
    }
    done := make(chan error, 1)
    go func() { done <- cmd.Wait() }()
    select {
    case err := <-done:
        return err
    case <-time.After(timeout):
        return cmd.Process.Kill()
    }
}
```

### Check PID alive

```go
func pidAlive(pid int) bool {
    if pid <= 0 {
        return false
    }
    err := syscall.Kill(pid, 0)
    return err == nil || errors.Is(err, syscall.EPERM)
}
```

---

## 6. Structured logging

### slog with JSON handler

```go
handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level:     slog.LevelInfo,
    AddSource: false,
})
logger := slog.New(handler)
```

### Logger hierarchy with context

```go
daemonLogger := logger.With("component", "daemon")
groupLogger := daemonLogger.With("group", groupName)
runnerLogger := groupLogger.With("runner", runnerName)

runnerLogger.Info("job completed",
    "job_id", jobID,
    "result", "success",
    "duration_s", elapsed.Seconds(),
)
```

### Multi-handler (write to multiple destinations)

```go
type MultiHandler struct {
    handlers []slog.Handler
}

func (m *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
    for _, h := range m.handlers {
        if h.Enabled(ctx, level) {
            return true
        }
    }
    return false
}

func (m *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
    for _, h := range m.handlers {
        if h.Enabled(ctx, r.Level) {
            _ = h.Handle(ctx, r)
        }
    }
    return nil
}

func (m *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
    handlers := make([]slog.Handler, len(m.handlers))
    for i, h := range m.handlers {
        handlers[i] = h.WithAttrs(attrs)
    }
    return &MultiHandler{handlers: handlers}
}

func (m *MultiHandler) WithGroup(name string) slog.Handler {
    handlers := make([]slog.Handler, len(m.handlers))
    for i, h := range m.handlers {
        handlers[i] = h.WithGroup(name)
    }
    return &MultiHandler{handlers: handlers}
}
```

---

## 7. Testing

### Table-driven test

```go
func TestParseConfig(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    *Config
        wantErr string
    }{
        {
            name:  "valid org scope",
            input: "https://github.com/my-org",
            want:  &Config{Scope: "org", Owner: "my-org"},
        },
        {
            name:    "empty URL",
            input:   "",
            wantErr: "url is required",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := ParseConfig(tt.input)
            if tt.wantErr != "" {
                require.ErrorContains(t, err, tt.wantErr)
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tt.want, got)
        })
    }
}
```

### HTTP test server

```go
func TestClient_ListRunners(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        assert.Equal(t, "/orgs/my-org/actions/runners", r.URL.Path)
        assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
        json.NewEncoder(w).Encode(map[string]any{
            "runners": []map[string]any{
                {"id": 1, "name": "runner-1", "status": "online"},
            },
        })
    }))
    defer srv.Close()

    client := NewClient(srv.URL, "test-token")
    runners, err := client.ListRunners(context.Background())
    require.NoError(t, err)
    assert.Len(t, runners, 1)
}
```

---

## 8. Configuration

### YAML with defaults

```go
type Config struct {
    Level   string `yaml:"level"`
    Dir     string `yaml:"dir"`
    MaxSize int    `yaml:"max_size"`
}

func (c *Config) applyDefaults() {
    if c.Level == "" {
        c.Level = "info"
    }
    if c.Dir == "" {
        if os.Getuid() == 0 {
            c.Dir = "/var/log/ghr"
        } else {
            home, _ := os.UserHomeDir()
            c.Dir = filepath.Join(home, ".local", "share", "ghr", "logs")
        }
    }
}
```

### Validation

```go
func (c *Config) Validate() error {
    if len(c.Groups) == 0 {
        return fmt.Errorf("at least one group is required")
    }
    for i, g := range c.Groups {
        if g.Name == "" {
            return fmt.Errorf("groups[%d].name is required", i)
        }
        if g.MaxRunners < 1 {
            return fmt.Errorf("groups[%d].max_runners must be >= 1", i)
        }
        if g.MinRunners > g.MaxRunners {
            return fmt.Errorf("groups[%d].min_runners (%d) > max_runners (%d)", i, g.MinRunners, g.MaxRunners)
        }
    }
    return nil
}
```

---

## 9. Concurrency

### Mutex-protected state

```go
type RunnerState struct {
    mu   sync.Mutex
    idle map[string]*Process
    busy map[string]*Process
}

func (s *RunnerState) MarkBusy(name string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    proc, ok := s.idle[name]
    if !ok {
        return // log warning
    }
    delete(s.idle, name)
    s.busy[name] = proc
}

func (s *RunnerState) Count() int {
    s.mu.Lock()
    defer s.mu.Unlock()
    return len(s.idle) + len(s.busy)
}

func (s *RunnerState) Snapshot() []RunnerSnapshot {
    s.mu.Lock()
    defer s.mu.Unlock()
    // Return a copy, not the map itself
    out := make([]RunnerSnapshot, 0, len(s.idle)+len(s.busy))
    for _, p := range s.idle { out = append(out, p.Snapshot("idle")) }
    for _, p := range s.busy { out = append(out, p.Snapshot("busy")) }
    return out
}
```

---

## 10. Filesystem

### Safe directory copy

```go
func copyDir(src, dst string) error {
    return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
        if err != nil {
            return err
        }
        rel, _ := filepath.Rel(src, path)
        target := filepath.Join(dst, rel)

        if d.IsDir() {
            return os.MkdirAll(target, 0o755)
        }

        info, err := d.Info()
        if err != nil {
            return err
        }
        return copyFile(path, target, info.Mode())
    })
}

func copyFile(src, dst string, perm fs.FileMode) error {
    in, err := os.Open(src)
    if err != nil {
        return err
    }
    defer in.Close()

    out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
    if err != nil {
        return err
    }
    defer out.Close()

    _, err = io.Copy(out, in)
    return err
}
```
