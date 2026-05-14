# Spec 08 — Authentication

## Overview

Authentication is handled via `ghr login`, an interactive CLI wizard. Credentials are stored in a dedicated file, separate from `config.yaml`. Multiple auth methods supported with runtime resolution order.

---

## Commands

### `ghr login`

Interactive wizard that validates and saves credentials.

```
$ ghr login

? How do you want to authenticate?
  > Personal Access Token (PAT)
    GitHub App

```

#### PAT flow

```
? Paste your GitHub PAT: ghp_xxxxxxxxxxxx
? GitHub URL (org, repo, or enterprise): https://github.com/my-org

  Validating token...
✓ Authenticated as @username
✓ Scopes: admin:org, repo
✓ Credentials saved to ~/.config/ghr/credentials.json
```

Validation: `GET https://api.github.com/user` with Bearer token. Display username and scopes. Fail if token is invalid or lacks required scopes.

#### GitHub App flow

```
? GitHub App Client ID: Iv1.abc123
? Installation ID: 12345678
? Private key: (paste path or drag & drop .pem file)
  Path: /etc/ghr/github-app.pem
? GitHub URL: https://github.com/my-org

  Validating GitHub App credentials...
✓ JWT signed successfully
✓ Installation access token obtained
✓ Registration token obtained (org: my-org)
✓ Credentials saved to ~/.config/ghr/credentials.json
```

Validation: full token exchange chain (JWT → installation token → registration token). If any step fails, display which step failed and why.

#### Flags (non-interactive mode for scripts/CI)

```bash
# PAT
ghr login --method pat --token ghp_xxx --url https://github.com/my-org

# GitHub App
ghr login --method app \
  --client-id Iv1.abc123 \
  --installation-id 12345678 \
  --private-key /path/to/key.pem \
  --url https://github.com/my-org
```

---

### `ghr logout`

Remove saved credentials.

```
$ ghr logout
✓ Credentials removed from ~/.config/ghr/credentials.json
```

---

### `ghr auth status`

Display current authentication state.

```
$ ghr auth status

Method:       GitHub App
Client ID:    Iv1.abc123
Installation: 12345678
GitHub URL:   https://github.com/my-org
Key:          /etc/ghr/github-app.pem (readable ✓)
Status:       ✓ authenticated
```

For PAT:
```
$ ghr auth status

Method:       Personal Access Token
GitHub URL:   https://github.com/my-org
User:         @username
Scopes:       admin:org, repo
Status:       ✓ valid
```

When not authenticated:
```
$ ghr auth status

Status:       ✗ not authenticated
Run 'ghr login' to authenticate.
```

---

## Credentials file

### Location

| Context | Path |
|---|---|
| Running as root | `/etc/ghr/credentials.json` |
| Running as user | `~/.config/ghr/credentials.json` |
| Override | `GHR_CREDENTIALS_FILE` env var |

File permissions: `0600` (owner read/write only). `ghr login` sets this automatically.

### Format

PAT:
```json
{
  "method": "pat",
  "github_url": "https://github.com/my-org",
  "pat": "ghp_xxxxxxxxxxxx",
  "created_at": "2024-01-15T10:00:00Z"
}
```

GitHub App:
```json
{
  "method": "github_app",
  "github_url": "https://github.com/my-org",
  "github_app": {
    "client_id": "Iv1.abc123",
    "installation_id": 12345678,
    "private_key_path": "/etc/ghr/github-app.pem"
  },
  "created_at": "2024-01-15T10:00:00Z"
}
```

The private key itself is NOT stored in credentials.json — only the path. The key stays wherever the user put it.

---

## Runtime resolution order

When the daemon or any command needs auth, credentials are resolved in this order (first match wins):

| Priority | Source | Use case |
|---|---|---|
| 1 | `--token` CLI flag | One-off override |
| 2 | `GITHUB_TOKEN` env var | CI/CD, scripts |
| 3 | Credentials file | Normal usage (via `ghr login`) |
| 4 | `.env` file (`GITHUB_TOKEN`) | Legacy / compat |

If nothing is found:
```
Error: not authenticated.
Run 'ghr login' to set up authentication, or set GITHUB_TOKEN.
```

### GitHub URL resolution

The GitHub URL is resolved separately from credentials, in this order:

| Priority | Source | Use case |
|---|---|---|
| 1 | Credentials file (`github_url`) | Normal usage (set during `ghr login`) |
| 2 | Config file (`github.url`) | Fallback when using `--token` or `GITHUB_TOKEN` without `ghr login` |

If `--token` or `GITHUB_TOKEN` is used without `ghr login`, the `github.url` field in config.yaml is **required**. Without it, the daemon cannot determine the target org/repo and will exit with an error:
```
Error: GitHub URL not found.
Set github.url in config.yaml, or run 'ghr login' to configure it.
```

---

## Impact on config.yaml

Auth is **removed** from config.yaml entirely. The `auth:` section no longer exists.

Before (spec 07):
```yaml
auth:
  github_app:
    client_id: "..."
    installation_id: 12345678
    private_key_path: "..."
  pat_env_var: "GITHUB_TOKEN"
```

After:
```yaml
# No auth section. Authentication handled by 'ghr login'.
# See: ghr auth status
```

The `github.url` field remains in config.yaml as **optional**. It serves as a fallback when the credentials file does not contain a URL (e.g., when using `--token` or `GITHUB_TOKEN` without `ghr login`):

```yaml
github:
  url: "https://github.com/my-org"   # optional, fallback for URL
  runner_group: "default"             # optional, default "default"
```

URL resolution: credentials file URL > config.yaml URL. See "GitHub URL resolution" above.

---

## Minimal config after auth separation

```yaml
groups:
  - name: "runners"
    max_runners: 5
```

That's it. Auth comes from `ghr login`. GitHub URL comes from credentials. Everything else has defaults.

---

## Implementation

### `internal/auth/` package

```go
package auth

// Credentials represents stored authentication.
type Credentials struct {
    Method    string         `json:"method"`     // "pat" or "github_app"
    GitHubURL string         `json:"github_url"`
    PAT       string         `json:"pat,omitempty"`
    GitHubApp *GitHubAppCreds `json:"github_app,omitempty"`
    CreatedAt time.Time      `json:"created_at"`
}

type GitHubAppCreds struct {
    ClientID       string `json:"client_id"`
    InstallationID int64  `json:"installation_id"`
    PrivateKeyPath string `json:"private_key_path"`
}

// Load reads credentials from the file, env vars, or CLI flags.
// Returns the resolved credentials and the source (for logging).
func Load(opts LoadOpts) (*Credentials, string, error) { ... }

// Save writes credentials to the credentials file with 0600 perms.
func Save(creds *Credentials) error { ... }

// Remove deletes the credentials file.
func Remove() error { ... }

// Validate checks that credentials work by calling the GitHub API.
func Validate(ctx context.Context, creds *Credentials) (*ValidationResult, error) { ... }

// FilePath returns the credentials file path for the current user.
func FilePath() string { ... }

type LoadOpts struct {
    TokenFlag string // --token CLI override
}

type ValidationResult struct {
    Valid    bool
    Username string   // for PAT
    Scopes   []string // for PAT
    OrgName  string   // for GitHub App
}
```

### Interactive wizard (`internal/cli/login.go`)

Uses `github.com/charmbracelet/huh` or simple `fmt.Scan` + `term` for the interactive prompts. Keeps it clean without heavy TUI dependencies.

Fallback: if stdin is not a terminal (piped/CI), require `--method` flag and all params as flags.

---

## Security considerations

- Credentials file is `0600` (not world-readable)
- PAT stored in plaintext in credentials.json — documented risk. Recommendation in docs: use GitHub App for production.
- Private key is NOT copied into credentials.json — only the path
- `ghr login` warns if the private key file has overly permissive perms (not `0600`)
- Credentials file is in `.gitignore` by default
- `ghr auth status` never prints the full PAT — only first/last 4 chars: `ghp_xxxx...xxxx`
