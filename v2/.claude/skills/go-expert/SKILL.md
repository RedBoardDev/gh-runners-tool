---
name: go-expert
description: Advanced Go patterns and best practices for daemon/service projects. Use when writing Go code involving goroutine lifecycle, context propagation, graceful shutdown, process management, HTTP clients, structured logging (slog), table-driven tests, consumer-side interfaces, or any Go architectural decision. Triggers on Go code, go.mod changes, or Go-related questions.
paths:
  - "**/*.go"
  - "go.mod"
  - "go.sum"
---

# Go Expert Patterns

Advanced Go patterns for daemon/service projects. Read `references/patterns.md` for the full reference when implementing complex patterns.

## Quick reference — most common patterns

### Goroutine lifecycle (oklog/run)
```go
var g run.Group
// Add actors: each is an (execute, interrupt) pair
g.Add(func() error { return server.Run(ctx) }, func(error) { cancel() })
g.Add(func() error { <-ctx.Done(); return nil }, func(error) { cancel() })
err := g.Run() // blocks until first actor returns, then interrupts all others
```

### Graceful shutdown
```go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()
// ... run services with ctx ...
// On signal: ctx is cancelled, services stop, cleanup runs
shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
service.Shutdown(shutdownCtx)
```

### Consumer-side interface
```go
// In the CONSUMER package, not the producer:
type store interface {
    Get(ctx context.Context, id string) (*Thing, error)
    Put(ctx context.Context, thing *Thing) error
}
// The producer returns a concrete struct that implicitly satisfies this.
```

### Process management (exec.Cmd)
```go
cmd := exec.CommandContext(ctx, path)
cmd.Dir = workDir
cmd.Env = append(os.Environ(), "KEY=value")
cmd.Stdout = logFile
cmd.Stderr = logFile
if err := cmd.Start(); err != nil { return err }
// Graceful stop:
cmd.Process.Signal(syscall.SIGTERM)
done := make(chan error, 1)
go func() { done <- cmd.Wait() }()
select {
case err := <-done: // exited
case <-time.After(10 * time.Second):
    cmd.Process.Kill()
    <-done
}
```

### Structured logging (slog)
```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
groupLogger := logger.With("group", groupName)
runnerLogger := groupLogger.With("runner", runnerName)
runnerLogger.Info("job completed", "result", "success", "duration_s", 42)
```

For the full pattern library, read `references/patterns.md`.
