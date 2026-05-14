---
description: Run tests with race detector and show results
allowed-tools: Bash(go test *)
---

# Run Tests

Run tests for $ARGUMENTS (default: all packages):

```bash
go test -race -v $ARGUMENTS
```

If no arguments: `go test -race ./...`

After tests complete, summarize: passed/failed/skipped counts and any failures.
