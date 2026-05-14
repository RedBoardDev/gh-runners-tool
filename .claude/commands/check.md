---
description: Run full CI checks locally (build, vet, fmt, lint, test)
allowed-tools: Bash(go *) Bash(golangci-lint *)
---

# Full CI Check

Run the complete check pipeline:

1. `go build ./cmd/ghr` — must compile
2. `go vet ./...` — static analysis
3. Check formatting: `gofmt -l .` — must return empty (all formatted)
4. `golangci-lint run` — lint (config: `.golangci.yml`)
5. `go test -race ./...` — all tests with race detector

Report pass/fail for each step. Stop on first failure.
