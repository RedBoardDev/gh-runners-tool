---
paths:
  - "**/*.go"
  - "**/*.yaml"
  - "**/*.json"
---

# Security Rules

- Never hardcode secrets (tokens, keys, passwords). Use env vars or the credentials file.
- Never log secrets. PATs are masked (`ghp_xxxx...xxxx`), JIT configs are never logged.
- JIT configs (`EncodedJITConfig`) are secrets — treat as such until consumed by the runner.
- Credentials file: `0600` permissions. Warn if overly permissive.
- Private key paths: verify `0600` permissions at login time.
- Webhook URLs (Discord, etc.): via env vars only, never in config.yaml.
- Never `exec.Command` with unsanitized user input.
- Never `filepath.Join` with untrusted path components (path traversal).
- TLS: do not skip verification by default. Support custom CAs via config if needed.
- Validate all external input (config values, API responses, env vars).
