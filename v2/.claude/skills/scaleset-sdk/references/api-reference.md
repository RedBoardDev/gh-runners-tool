# actions/scaleset — Complete API Reference

## Table of Contents

1. [Package constants](#1-package-constants)
2. [Core types](#2-core-types)
3. [Job message types](#3-job-message-types)
4. [Client construction](#4-client-construction)
5. [HTTP options](#5-http-options)
6. [Authentication flow](#6-authentication-flow)
7. [Client API methods](#7-client-api-methods)
8. [MessageSessionClient](#8-messagesessionclient)
9. [Listener package](#9-listener-package)
10. [Error handling](#10-error-handling)
11. [Config URL parsing](#11-config-url-parsing)
12. [Full endpoint map](#12-full-endpoint-map)
13. [Statistics fields](#13-statistics-fields)
14. [Long-polling mechanics](#14-long-polling-mechanics)
15. [Concurrency model](#15-concurrency-model)
16. [Known limitations](#16-known-limitations)
17. [Dependencies](#17-dependencies)

---

## 1. Package constants

```go
const HeaderScaleSetMaxCapacity = "X-ScaleSetMaxCapacity"
const DefaultRunnerGroup = "default"

type MessageType string
const (
    MessageTypeJobAvailable MessageType = "JobAvailable"
    MessageTypeJobAssigned  MessageType = "JobAssigned"
    MessageTypeJobStarted   MessageType = "JobStarted"
    MessageTypeJobCompleted MessageType = "JobCompleted"
)

var ErrInvalidGitHubConfigURL = fmt.Errorf("invalid config URL, should point to an enterprise, org, or repository")
```

---

## 2. Core types

```go
type RunnerScaleSet struct {
    ID                 int                      `json:"id,omitempty"`
    Name               string                   `json:"name,omitempty"`
    RunnerGroupID      int                      `json:"runnerGroupId,omitempty"`
    RunnerGroupName    string                   `json:"runnerGroupName,omitempty"`
    Labels             []Label                  `json:"labels,omitempty"`
    RunnerSetting      RunnerSetting            `json:"RunnerSetting,omitempty"`
    CreatedOn          time.Time                `json:"createdOn,omitempty"`
    RunnerJitConfigURL string                   `json:"runnerJitConfigUrl,omitempty"`
    Statistics         *RunnerScaleSetStatistic `json:"statistics,omitempty"`
}

type Label struct {
    Type string `json:"type"`  // "System" or empty (defaults to "System")
    Name string `json:"name"`
}

type RunnerSetting struct {
    DisableUpdate bool `json:"disableUpdate,omitempty"`
}

type RunnerGroup struct {
    ID        int    `json:"id"`
    Name      string `json:"name"`
    Size      int    `json:"size"`
    IsDefault bool   `json:"isDefaultGroup"`
}

type RunnerScaleSetSession struct {
    SessionID               uuid.UUID                `json:"sessionId,omitempty"`
    OwnerName               string                   `json:"ownerName,omitempty"`
    RunnerScaleSet          *RunnerScaleSet          `json:"runnerScaleSet,omitempty"`
    MessageQueueURL         string                   `json:"messageQueueUrl,omitempty"`
    MessageQueueAccessToken string                   `json:"messageQueueAccessToken,omitempty"`
    Statistics              *RunnerScaleSetStatistic `json:"statistics,omitempty"`
}

type RunnerScaleSetStatistic struct {
    TotalAvailableJobs     int `json:"totalAvailableJobs"`
    TotalAcquiredJobs      int `json:"totalAcquiredJobs"`
    TotalAssignedJobs      int `json:"totalAssignedJobs"`   // THE scaling metric
    TotalRunningJobs       int `json:"totalRunningJobs"`
    TotalRegisteredRunners int `json:"totalRegisteredRunners"`
    TotalBusyRunners       int `json:"totalBusyRunners"`
    TotalIdleRunners       int `json:"totalIdleRunners"`
}

type RunnerScaleSetMessage struct {
    MessageID            int
    Statistics           *RunnerScaleSetStatistic
    JobAvailableMessages []*JobAvailable
    JobAssignedMessages  []*JobAssigned
    JobStartedMessages   []*JobStarted
    JobCompletedMessages []*JobCompleted
}

type RunnerScaleSetJitRunnerSetting struct {
    Name       string `json:"name"`
    WorkFolder string `json:"workFolder"`
}

type RunnerScaleSetJitRunnerConfig struct {
    Runner           *RunnerReference `json:"runner"`
    EncodedJITConfig string           `json:"encodedJITConfig"`
}

type RunnerReference struct {
    ID               int    `json:"id"`
    Name             string `json:"name"`
    RunnerScaleSetID int    `json:"runnerScaleSetId"`
}

type SystemInfo struct {
    System     string `json:"system"`
    Version    string `json:"version"`
    CommitSHA  string `json:"commit_sha"`
    ScaleSetID int    `json:"scale_set_id"`
    Subsystem  string `json:"subsystem"`
}

type GitHubAppAuth struct {
    ClientID       string
    InstallationID int64
    PrivateKey     string  // PEM-formatted RSA private key
}

type ProxyFunc func(req *http.Request) (*url.URL, error)
```

---

## 3. Job message types

```go
type JobMessageBase struct {
    JobMessageType
    RunnerRequestID    int64     `json:"runnerRequestId"`
    RepositoryName     string    `json:"repositoryName"`
    OwnerName          string    `json:"ownerName"`
    JobID              string    `json:"jobId"`
    JobWorkflowRef     string    `json:"jobWorkflowRef"`
    JobDisplayName     string    `json:"jobDisplayName"`
    WorkflowRunID      int64     `json:"workflowRunId"`
    EventName          string    `json:"eventName"`
    RequestLabels      []string  `json:"requestLabels"`
    QueueTime          time.Time `json:"queueTime"`
    ScaleSetAssignTime time.Time `json:"scaleSetAssignTime"`
    RunnerAssignTime   time.Time `json:"runnerAssignTime"`
    FinishTime         time.Time `json:"finishTime"`
}

type JobAvailable struct {
    AcquireJobURL string `json:"acquireJobUrl"`
    JobMessageBase
}

type JobAssigned struct {
    JobMessageBase
}

type JobStarted struct {
    RunnerID   int    `json:"runnerId"`
    RunnerName string `json:"runnerName"`
    JobMessageBase
}

type JobCompleted struct {
    Result     string `json:"result"`  // "Succeeded", "Failed", "Cancelled"
    RunnerID   int    `json:"runnerId"`
    RunnerName string `json:"runnerName"`
    JobMessageBase
}
```

---

## 4. Client construction

```go
// GitHub App (recommended)
type ClientWithGitHubAppConfig struct {
    GitHubConfigURL string
    GitHubAppAuth   GitHubAppAuth
    SystemInfo      SystemInfo
}
func NewClientWithGitHubApp(config ClientWithGitHubAppConfig, options ...HTTPOption) (*Client, error)

// PAT
type NewClientWithPersonalAccessTokenConfig struct {
    GitHubConfigURL     string
    PersonalAccessToken string
    SystemInfo          SystemInfo
}
func NewClientWithPersonalAccessToken(config NewClientWithPersonalAccessTokenConfig, options ...HTTPOption) (*Client, error)
```

GitHubConfigURL examples:
- Org: `https://github.com/my-org`
- Repo: `https://github.com/my-org/my-repo`
- Enterprise: `https://github.com/enterprises/my-enterprise`
- GHES: `https://ghes.company.com/my-org`

---

## 5. HTTP options

```go
type HTTPOption func(*httpClientOption)

func WithRetryMax(retryMax int) HTTPOption              // default: 4
func WithRetryWaitMax(retryWaitMax time.Duration) HTTPOption  // default: 30s
func WithTimeout(duration time.Duration) HTTPOption      // default: 5min
func WithLogger(logger *slog.Logger) HTTPOption          // default: discard
func WithRootCAs(rootCAs *x509.CertPool) HTTPOption      // custom CA pool
func WithoutTLSVerify() HTTPOption                        // skip TLS verification
func WithProxy(proxyFunc ProxyFunc) HTTPOption            // custom proxy
func WithRetryableHTTPClint(client *retryablehttp.Client) HTTPOption  // NOTE: typo in name is intentional (published API)
```

---

## 6. Authentication flow

### GitHub App path (4 steps, all automatic)

1. **Create JWT**: RS256 signed, iat = now-60s (clock skew), exp = iat+9min, iss = ClientID
2. **Get installation access token**: `POST /app/installations/{id}/access_tokens` with Bearer JWT
3. **Get registration token**: `POST /orgs/{org}/actions/runners/registration-token` (or /repos/ or /enterprises/) with Bearer access_token
4. **Get admin connection**: `POST /actions/runner-registration` with `Authorization: RemoteAuth {registration_token}` — returns `ActionsServiceURL` + `AdminToken` (JWT)

### PAT path (2 steps)

1. **Get registration token**: same endpoint, with Bearer PAT directly
2. **Get admin connection**: same as step 4 above

### Token refresh

`updateTokenIfNeeded()` runs before every Actions Service request. If admin token expires within 60s, full chain re-executes. Expiry parsed from JWT claims (ParseUnverified).

The admin connection request retries on 401 and 403 (propagation delays).

---

## 7. Client API methods

All methods are thread-safe (mutex-protected).

### Scale Set CRUD

```go
func (c *Client) CreateRunnerScaleSet(ctx, *RunnerScaleSet) (*RunnerScaleSet, error)
// POST /_apis/runtime/runnerscalesets
// Auto-adds label from Name if no labels provided. Errors if both Name and Labels empty.

func (c *Client) GetRunnerScaleSet(ctx, runnerGroupID int, name string) (*RunnerScaleSet, error)
// GET /_apis/runtime/runnerscalesets?runnerGroupId={id}&name={name}
// Returns nil,nil if count=0. Error if count>1.

func (c *Client) GetRunnerScaleSetByID(ctx, id int) (*RunnerScaleSet, error)
// GET /_apis/runtime/runnerscalesets/{id}

func (c *Client) UpdateRunnerScaleSet(ctx, id int, *RunnerScaleSet) (*RunnerScaleSet, error)
// PATCH /_apis/runtime/runnerscalesets/{id}

func (c *Client) DeleteRunnerScaleSet(ctx, id int) error
// DELETE /_apis/runtime/runnerscalesets/{id} — expects 204
```

### Runner management

```go
func (c *Client) GetRunner(ctx, runnerID int) (*RunnerReference, error)
func (c *Client) GetRunnerByName(ctx, name string) (*RunnerReference, error)  // nil,nil if not found
func (c *Client) RemoveRunner(ctx, runnerID int64) error                      // expects 204
```

### JIT config

```go
func (c *Client) GenerateJitRunnerConfig(ctx, *RunnerScaleSetJitRunnerSetting, scaleSetID int) (*RunnerScaleSetJitRunnerConfig, error)
// POST /_apis/runtime/runnerscalesets/{id}/generatejitconfig
```

### Runner group

```go
func (c *Client) GetRunnerGroupByName(ctx, name string) (*RunnerGroup, error)
// Default group has ID=1 (hardcode for "default" to skip this call)
```

### Message session

```go
func (c *Client) MessageSessionClient(ctx, scaleSetID int, owner string, options ...HTTPOption) (*MessageSessionClient, error)
// Creates session immediately (POST). owner = hostname or UUID.
```

### Utility

```go
func (c *Client) SetSystemInfo(info SystemInfo)
func (c *Client) SystemInfo() SystemInfo
func (c *Client) DebugInfo() string  // JSON with HasProxy, HasRootCA, SystemInfo
```

---

## 8. MessageSessionClient

```go
func (c *MessageSessionClient) GetMessage(ctx, lastMessageID, maxCapacity int) (*RunnerScaleSetMessage, error)
// Long-polls. 200=message, 202=nil,nil (no messages). Auto-refreshes on 401.

func (c *MessageSessionClient) DeleteMessage(ctx, messageID int) error
// Ack. 204=success. Auto-refreshes on 401.

func (c *MessageSessionClient) AcquireJobs(ctx, requestIDs []int64) ([]int64, error)
// Claims jobs. Returns actually acquired IDs (may be subset).

func (c *MessageSessionClient) Session() RunnerScaleSetSession
// Returns copy of current session.

func (c *MessageSessionClient) Close(ctx) error
// Deletes session. Always call (use defer).
```

---

## 9. Listener package

```go
import "github.com/actions/scaleset/listener"

type Config struct {
    ScaleSetID int
    MaxRunners int
    Logger     *slog.Logger
}

func New(client Client, config Config, options ...Option) (*Listener, error)
func (l *Listener) Run(ctx context.Context, scaler Scaler) error
func (l *Listener) SetMaxRunners(count int)  // thread-safe, takes effect on next poll

type Scaler interface {
    HandleDesiredRunnerCount(ctx context.Context, count int) (int, error)
    HandleJobStarted(ctx context.Context, jobInfo *scaleset.JobStarted) error
    HandleJobCompleted(ctx context.Context, jobInfo *scaleset.JobCompleted) error
}

type Client interface {
    GetMessage(ctx context.Context, lastMessageID, maxCapacity int) (*scaleset.RunnerScaleSetMessage, error)
    DeleteMessage(ctx context.Context, messageID int) error
    AcquireJobs(ctx context.Context, requestIDs []int64) ([]int64, error)
    Session() scaleset.RunnerScaleSetSession
}

type MetricsRecorder interface {
    RecordStatistics(statistics *scaleset.RunnerScaleSetStatistic)
    RecordJobStarted(msg *scaleset.JobStarted)
    RecordJobCompleted(msg *scaleset.JobCompleted)
    RecordDesiredRunners(count int)
}

func WithMetricsRecorder(recorder MetricsRecorder) Option
```

### Run() loop internals

1. Read initial session statistics
2. Call `HandleDesiredRunnerCount(ctx, initialStats.TotalAssignedJobs)`
3. Loop:
   - `GetMessage(ctx, lastMessageID, maxRunners)` — long-polls ~50s
   - If nil: call `HandleDesiredRunnerCount` with cached stats, continue
   - If message: ack (DeleteMessage) → AcquireJobs → HandleJobStarted(s) → HandleJobCompleted(s) → HandleDesiredRunnerCount
   - Any error from Scaler: return error (terminates Run)

---

## 10. Error handling

### Sentinel errors

```go
var RunnerNotFoundError           = scalesetError("runner not found")
var RunnerExistsError             = scalesetError("runner exists")
var JobStillRunningError          = scalesetError("job still running")
var MessageQueueTokenExpiredError = scalesetError("message queue token expired")
```

Use `errors.Is(err, scaleset.RunnerNotFoundError)` etc.

### Exception mapping

Server returns JSON `{"typeName":"...", "message":"..."}`. Mapped:
- `AgentExistsException` → `RunnerExistsError`
- `AgentNotFoundException` → `RunnerNotFoundError`
- `JobStillRunningException` → `JobStillRunningError`

### Error metadata

All HTTP errors include ActivityId and X-GitHub-Request-Id headers in the message.

---

## 11. Config URL parsing

| URL pattern | Scope | Example |
|---|---|---|
| `github.com/{org}` | Organization | `https://github.com/my-org` |
| `github.com/{org}/{repo}` | Repository | `https://github.com/my-org/my-repo` |
| `github.com/enterprises/{name}` | Enterprise | `https://github.com/enterprises/my-ent` |
| `ghes.example.com/{org}` | Org (GHES) | `https://ghes.corp.com/my-org` |

API URL routing:
- Hosted (github.com, *.ghe.com): `api.github.com` or `api.{host}`
- GHES: `{host}/api/v3`
- `GITHUB_ACTIONS_FORCE_GHES` env var forces GHES mode

---

## 12. Full endpoint map

| Method | HTTP | Endpoint | Status |
|---|---|---|---|
| CreateRunnerScaleSet | POST | `/_apis/runtime/runnerscalesets` | 200 |
| GetRunnerScaleSet | GET | `/_apis/runtime/runnerscalesets?runnerGroupId=&name=` | 200 |
| GetRunnerScaleSetByID | GET | `/_apis/runtime/runnerscalesets/{id}` | 200 |
| UpdateRunnerScaleSet | PATCH | `/_apis/runtime/runnerscalesets/{id}` | 200 |
| DeleteRunnerScaleSet | DELETE | `/_apis/runtime/runnerscalesets/{id}` | 204 |
| GetRunnerGroupByName | GET | `/_apis/runtime/runnergroups/?groupName=` | 200 |
| GetRunner | GET | `/_apis/distributedtask/pools/0/agents/{id}` | 200 |
| GetRunnerByName | GET | `/_apis/distributedtask/pools/0/agents?agentName=` | 200 |
| RemoveRunner | DELETE | `/_apis/distributedtask/pools/0/agents/{id}` | 204 |
| GenerateJitRunnerConfig | POST | `/_apis/runtime/runnerscalesets/{id}/generatejitconfig` | 200 |
| createMessageSession | POST | `/_apis/runtime/runnerscalesets/{id}/sessions` | 200 |
| deleteMessageSession | DELETE | `/_apis/runtime/runnerscalesets/{id}/sessions/{sessionId}` | 204 |
| refreshMessageSession | PATCH | `/_apis/runtime/runnerscalesets/{id}/sessions/{sessionId}` | 200 |
| AcquireJobs | POST | `/_apis/runtime/runnerscalesets/{id}/acquirejobs` | 200 |
| GetMessage | GET | `{messageQueueURL}?lastMessageId=` | 200/202 |
| DeleteMessage | DELETE | `{messageQueueURL}/{messageId}` | 204 |
| Registration token (org) | POST | `/orgs/{org}/actions/runners/registration-token` | 201 |
| Registration token (repo) | POST | `/repos/{owner}/{repo}/actions/runners/registration-token` | 201 |
| Registration token (ent) | POST | `/enterprises/{ent}/actions/runners/registration-token` | 201 |
| Access token (App) | POST | `/app/installations/{id}/access_tokens` | 201 |
| Admin connection | POST | `/actions/runner-registration` | 2xx |

---

## 13. Statistics fields

```go
type RunnerScaleSetStatistic struct {
    TotalAvailableJobs     int  // jobs waiting to be assigned
    TotalAcquiredJobs      int  // jobs claimed by AcquireJobs
    TotalAssignedJobs      int  // THE metric: jobs that need runners
    TotalRunningJobs       int  // jobs currently executing
    TotalRegisteredRunners int  // runners registered with GitHub
    TotalBusyRunners       int  // runners currently running a job
    TotalIdleRunners       int  // runners waiting for a job
}
```

`TotalAssignedJobs >= TotalRunningJobs`. Use `TotalAssignedJobs` for scaling, NOT individual message counts (messages are capped at 50 per batch).

---

## 14. Long-polling mechanics

- `GetMessage` uses HTTP long-polling (~50s server-side timeout)
- HTTP 200 = messages available (returned immediately)
- HTTP 202 = no messages (timeout, returns nil,nil)
- `lastMessageId` query param prevents reprocessing
- `X-ScaleSetMaxCapacity` header tells server your capacity
- Messages not ack'd (DeleteMessage) are redelivered
- Job reassignment: jobs can appear as JobAssigned → JobCompleted(Cancelled) up to 3 times with incremental delays

---

## 15. Concurrency model

- `Client.mu sync.Mutex` — every public method acquires it
- `MessageSessionClient.mu sync.Mutex` — separate mutex, every public method acquires it
- `Listener.maxRunners atomic.Uint32` — SetMaxRunners is lock-free
- When MessageSessionClient needs the parent Client (for token refresh), it explicitly acquires innerClient.mu

---

## 16. Known limitations

1. **Public Preview** — interfaces may change
2. **Go 1.25+ required**
3. **Message batch cap of 50** — don't count individual messages for scaling
4. **Silent label dropping on GHES < 3.21** without feature flag
5. **Typo in API**: `WithRetryableHTTPClint` (missing 'e') — can't be fixed
6. **HTTP defaults**: retryMax=4, retryWaitMax=30s, timeout=5min
7. **All response bodies read into memory** (BOM-trimmed)
8. **`GITHUB_ACTIONS_FORCE_GHES`** env var forces GHES mode (check existence, not value)

---

## 17. Dependencies

| Package | Version | Role |
|---|---|---|
| golang-jwt/jwt/v4 | v4.5.2 | JWT signing/verification |
| hashicorp/go-retryablehttp | v0.7.8 | HTTP retries |
| google/uuid | v1.6.0 | Session IDs |
| spf13/cobra | v1.10.2 | CLI framework (example) |
| stretchr/testify | v1.11.1 | Testing |
