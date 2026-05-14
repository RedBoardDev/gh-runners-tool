---
paths:
  - "**/*.go"
---

# Go Style & Idioms

## Naming
- Package names: short, lowercase, singular (`runner` not `runners`, `config` not `configuration`)
- Exported names: PascalCase, meaningful without package prefix (`runner.Process` not `runner.RunnerProcess`)
- Unexported: camelCase
- Acronyms: all caps (`ID`, `HTTP`, `URL`, `API`, `PID`, `JIT`)
- Interface names: verb-er for single-method (`io.Reader`), descriptive for multi-method

## Error handling
- Always wrap with context: `fmt.Errorf("start runner %s: %w", name, err)`
- Never ignore errors with `_` — handle or log explicitly
- Use sentinel errors (`var ErrNotFound = errors.New(...)`) for expected conditions
- Use `errors.Is` / `errors.As` for checking, never string comparison
- Return early on error (no deep nesting)

## Functions
- `context.Context` always first parameter
- Return concrete types, accept interfaces
- Keep functions short (< 40 lines guideline)
- Prefer named return values only when it aids godoc clarity

## Concurrency
- Protect shared state with `sync.Mutex` (not channels for simple state)
- Always use `context.Context` for cancellation
- Never start a goroutine without a way to stop it
- Use `oklog/run` for top-level actor management
- Use `sync.WaitGroup` or `errgroup` for worker pools

## Testing
- Table-driven with `t.Run` subtests
- Test file in same package (white-box) or `_test` package (black-box)
- Use `testify/assert` or `testify/require` for assertions
- Use `httptest.Server` for HTTP tests
- Test names: `TestFunctionName_Scenario_Expected`
- Race detector: always run with `-race` in CI

## Packages
- Everything under `internal/` (nothing exported outside module)
- One feature per package, no `utils/` or `helpers/`
- Avoid circular imports — if needed, extract shared types to `model/`
- Package-level `var` and `init()` only for simple defaults, never for complex setup
