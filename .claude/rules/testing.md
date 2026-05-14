---
paths:
  - "**/*_test.go"
---

# Testing Rules

## Structure
- One test file per source file: `foo.go` → `foo_test.go`
- Table-driven tests with `t.Run` for every non-trivial function
- Group related tests in subtests: `TestGroupController/startup`, `TestGroupController/shutdown`

## Naming
- `TestFunctionName` for basic tests
- `TestFunctionName_Scenario` for specific scenarios
- `TestFunctionName_Scenario_Expected` for full clarity
- Benchmark: `BenchmarkFunctionName`

## Assertions
- Use `testify/require` for fatal checks (stop test on failure)
- Use `testify/assert` for non-fatal checks (continue test)
- Never use bare `if err != nil { t.Fatal(err) }` when testify is available

## Mocking
- Consumer-side interfaces make mocking trivial
- Hand-written fakes preferred over generated mocks for simple interfaces
- Use `httptest.Server` for HTTP integration tests
- Use the scaleset SDK's `internal/testserver` pattern for GitHub API mocks

## Coverage
- Run with `-race` flag always
- Focus on behavior, not coverage percentage
- Test error paths, not just happy paths
- Timeouts in tests: use `context.WithTimeout` or `time.After`, never bare `time.Sleep`

## What to test
- `model/` — no tests needed (pure data)
- `controller/` — mock github client + runner backend
- `runner/` — test binary download with httptest, process lifecycle with real exec
- `health/` — mock runner state + reporter interfaces
- `notification/` — test providers against httptest.Server
- `config/` — table-driven validation
- `cli/` — thin layer, minimal tests
