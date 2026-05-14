# ghr — GitHub Actions Runner Controller for macOS

## Project

Self-hosted GitHub Actions runner controller built on the official `actions/scaleset` Go SDK. Manages ephemeral runners via JIT configs, scale sets, and long-polling. Targets macOS (Apple Silicon + Intel).

## Quick Reference

```bash
go build ./cmd/ghr              # build
go test ./...                    # test all
go test -race ./...              # test with race detector
go vet ./...                     # static analysis
gofmt -w .                       # format
golangci-lint run                # lint (if installed)
```

## Architecture

Package-by-feature under `internal/`. No DDD layers. See `specs/00-architecture.md`.

```
cmd/ghr/main.go       → wiring, DI, CLI
internal/cli/          → Cobra commands (thin)
internal/auth/         → credentials (login, load, save)
internal/config/       → YAML + env loading
internal/controller/   → scale set orchestration + Scaler
internal/runner/       → binary download + process management
internal/github/       → scaleset SDK adapter
internal/health/       → health monitoring
internal/notification/ → event-driven alerts (Discord, webhooks)
internal/monitoring/   → push-based reporters (Uptime Kuma)
internal/api/          → Unix socket JSON API (IPC for ghr status)
internal/launchd/      → macOS service management
internal/logging/      → slog multi-writer, rotation
internal/model/        → shared structs only (no interfaces, no logic)
```

## Code Conventions

- Go 1.25+ required (for actions/scaleset SDK)
- Interfaces defined where consumed, not where implemented
- Structs with exported fields, not getter interfaces
- Error wrapping: `fmt.Errorf("context: %w", err)`
- `oklog/run` for daemon goroutine lifecycle
- `context.Context` as first param everywhere
- No `any` without justification, no `_` to ignore errors
- Table-driven tests with `t.Run` subtests

## Commit Convention

`type(scope): description` — types: feat, fix, docs, refactor, test, chore

## Key Dependencies

- `github.com/actions/scaleset` — Scale Set API + listener
- `github.com/spf13/cobra` — CLI
- `github.com/oklog/run` — goroutine lifecycle
- `github.com/joho/godotenv` — .env loading
- `gopkg.in/yaml.v3` — config
- `log/slog` (stdlib) — structured logging

## Specs

All specs in `specs/`. Read before implementing:
- `00-architecture.md` — package structure, interfaces, DI wiring
- `01-core-scaleset.md` — scale set engine, scaler, runner manager
- `02-cli-commands.md` — start/stop/run/status/purge/login
- `03-health-monitor.md` — health checks, issue detection
- `04-logging.md` — structured logging, rotation, per-runner files
- `05-notifications.md` — Discord, webhook providers
- `06-uptime-kuma.md` — push monitoring
- `07-config.md` — YAML schema, validation, defaults
- `08-auth.md` — login wizard, credentials file, resolution order
