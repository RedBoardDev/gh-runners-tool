---
paths:
  - "internal/**/*.go"
  - "cmd/**/*.go"
---

# Architecture Rules

## Package structure
- Package-by-feature under `internal/`, one level deep. No `domain/`, `app/`, `infra/` layers.
- `internal/model/` contains ONLY shared data structs and enums. No interfaces. No logic. Under 100 LOC.
- Each package owns its feature end-to-end.

## Interfaces
- Define interfaces where they are CONSUMED, not where they are implemented.
- Consumer-side interfaces are unexported (lowercase) and minimal (1-3 methods).
- Never create a central `ports.go` or `interfaces.go`.
- Never create getter interfaces (`ID() string`, `Name() string`). Use struct fields.

## Dependencies
- Dependency injection is manual in `cmd/ghr/main.go`. No DI framework.
- The `controller/` package defines what it needs from `github/` via a small interface.
- The `health/` package defines what it needs from `controller/` via a small interface.
- Import direction: `cli` → `controller` → `github`, `runner`, `notification`. Never the reverse.

## Concurrency
- `oklog/run.Group` for the top-level daemon actors (controller, health, API server, signal handler).
- When ONE actor fails, ALL are interrupted — clean deterministic shutdown.
- Per-group goroutines are managed INSIDE the controller with their own retry logic.
- A single group failure does NOT kill other groups.

## Configuration
- All config values come from the config struct. No global variables.
- Secrets via env vars only, never in YAML.
- Auth credentials via `ghr login` / credentials file, not config.

## Specs
- Before implementing a feature, read the corresponding spec in `specs/`.
- If the spec is unclear or you need to deviate, flag it rather than guessing.
