# Simple Test

Minimal test: 1 group, 1 runner, 1 job.

## Setup

```bash
ghr login
ghr run --config tests/simple/config.yaml
```

## Trigger

Copy `workflow.yml` to `.github/workflows/test-simple.yml` in your repo.
Run it from GitHub Actions > "Run workflow".

## Expected

- 1 scale set created
- 1 runner provisioned on job dispatch
- Job completes, runner cleaned up
- Ctrl+C stops cleanly
