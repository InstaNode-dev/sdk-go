# instanode.dev Go SDK

Zero-friction developer infrastructure in a single HTTP call.
Provision real Postgres databases, Redis caches, MongoDB databases,
NATS queues — and deploy the application that runs on top of them —
no account, no Docker, no setup.

**[https://instanode.dev](https://instanode.dev)**

```
go get github.com/InstaNode-dev/sdk-go@latest
```

Zero external dependencies. Requires Go 1.22+.

---

## Quickstart

Provision a database and cache in a few lines — no account needed:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/InstaNode-dev/sdk-go/instant"
)

func main() {
    ctx := context.Background()
    client := instant.New() // anonymous; set INSTANT_API_KEY for permanent resources

    // A resource name is required on every provision call.
    db, err := client.ProvisionDatabase(ctx, &instant.ProvisionOpts{Name: "app-db"})
    if err != nil { log.Fatal(err) }
    fmt.Println("postgres:", db.ConnectionURL) // postgres://usr:pass@host:5432/db

    cache, err := client.ProvisionCache(ctx, &instant.ProvisionOpts{Name: "app-cache"})
    if err != nil { log.Fatal(err) }
    fmt.Println("redis:", cache.ConnectionURL) // redis://:pass@host:6379
}
```

---

## All Methods

### Provisioning

| Method | Signature | Description |
|---|---|---|
| `ProvisionDatabase` | `(ctx, *ProvisionOpts) (*ProvisionResult, error)` | Postgres database + scoped user |
| `ProvisionCache` | `(ctx, *ProvisionOpts) (*ProvisionResult, error)` | Redis cache namespace |
| `ProvisionMongoDB` | `(ctx, *ProvisionOpts) (*ProvisionResult, error)` | MongoDB database + scoped user |
| `ProvisionQueue` | `(ctx, *ProvisionOpts) (*ProvisionResult, error)` | NATS JetStream stream |
| `ProvisionVector` | `(ctx, *VectorOpts) (*VectorResult, error)` | pgvector-enabled Postgres (POST /vector/new) |
| `ProvisionStorage` | `(ctx, *ProvisionOpts) (*StorageResult, error)` | S3-compatible object-storage prefix (POST /storage/new) |
| `ProvisionWebhook` | `(ctx, *ProvisionOpts) (*WebhookResult, error)` | Webhook receiver URL (POST /webhook/new) |

### Deployment

| Method | Signature | Description |
|---|---|---|
| `Deploy` | `(ctx, DeployOpts) (*Deployment, error)` | Build + deploy a single app from a gzipped tarball (POST /deploy/new — requires an API key) |
| `CreateStack` | `(ctx, CreateStackOpts) (*Stack, error)` | Deploy a multi-service stack (POST /stacks/new — the **anonymous** deploy path; works without an API key) |
| `GetStack` | `(ctx, slug string) (*Stack, error)` | Poll a stack's status + per-service URLs (GET /stacks/:slug) |
| `DeploymentEvents` | `(ctx, id string, limit int) (*DeploymentEventList, error)` | Failure-autopsy timeline for a deploy (GET /api/v1/deployments/:id/events) |
| `UpdateDeployEnv` | `(ctx, id string, env map[string]string) (*DeployEnvUpdate, error)` | Merge env vars into a deployment; redeploy to apply (PATCH /deploy/:id/env) |
| `UpdateStackEnv` | `(ctx, slug string, env map[string]string) (*StackEnvUpdate, error)` | Merge env vars into a stack; empty value deletes the key (PATCH /stacks/:slug/env) |
| `WakeDeployment` | `(ctx, id string) (*WakeResult, error)` | Wake a scaled-to-zero deployment (POST /deploy/:id/wake; 501 when the platform flag is off) |

### Vault (requires API key, paid tier)

| Method | Signature | Description |
|---|---|---|
| `SetVaultKey` | `(ctx, env, key, value string) (*VaultWriteResult, error)` | Store an encrypted secret as a new version (PUT /api/v1/vault/:env/:key) |
| `RotateVaultKey` | `(ctx, env, key, value string) (*VaultWriteResult, error)` | Rotate a secret — new value, version+1, distinct audit action (POST /api/v1/vault/:env/:key/rotate) |

### Storage access

| Method | Signature | Description |
|---|---|---|
| `PresignStorage` | `(ctx, token string, PresignOpts) (*PresignResult, error)` | Mint a ≤1h presigned S3 URL (POST /storage/:token/presign — the token is the credential, no API key needed) |

### Resource Management (requires API key)

| Method | Signature | Description |
|---|---|---|
| `ListResources` | `(ctx) (*ResourceList, error)` | List all team resources |
| `GetResource` | `(ctx, token string) (*Resource, error)` | Get a resource by token |
| `DeleteResource` | `(ctx, token string) error` | Soft-delete a resource |
| `RotateCredentials` | `(ctx, token string) (*RotateResult, error)` | New password → return updated connection URL |
| `PauseResource` | `(ctx, token string) (*PauseResumeResult, error)` | Suspend without deleting — storage kept, connections refused (POST /api/v1/resources/:id/pause, Pro+) |
| `ResumeResource` | `(ctx, token string) (*PauseResumeResult, error)` | Un-pause — connection URL unchanged (POST /api/v1/resources/:id/resume) |

### Account & Claiming

| Method | Signature | Description |
|---|---|---|
| `Claim` | `(ctx, ClaimOpts) (*ClaimResult, error)` | Convert anonymous session to registered team |
| `ClaimTokens` | `(ctx, apiKey string, tokens []string) (*ClaimResult, error)` | Associate anonymous tokens with authenticated team |

---

## Provisioning

Every provision method requires a non-nil `*ProvisionOpts` with a valid `Name`.
The name is a **required** resource label: 1–64 characters matching
`^[A-Za-z0-9][A-Za-z0-9 _-]*$`. The SDK validates it client-side before the
request; the server otherwise rejects a missing or invalid name with HTTP 400.

```go
// Postgres
db, err := client.ProvisionDatabase(ctx, &instant.ProvisionOpts{Name: "app-db"})
// db.ConnectionURL  → postgres://usr_<token>:<pass>@host:5432/db_<token>
// db.Limits.StorageMB, db.Limits.Connections

// Redis
cache, err := client.ProvisionCache(ctx, &instant.ProvisionOpts{Name: "app-cache"})
// cache.ConnectionURL → redis://:pass@host:6379
// cache.KeyPrefix     → prefix all keys with this value (key-namespace isolation)

// MongoDB
mdb, err := client.ProvisionMongoDB(ctx, &instant.ProvisionOpts{Name: "app-mongo"})
// mdb.ConnectionURL → mongodb://usr:pass@host:27017/db_<token>

// NATS JetStream
q, err := client.ProvisionQueue(ctx, &instant.ProvisionOpts{Name: "app-queue"})
// q.ConnectionURL → nats://usr:pass@host:4222

// pgvector (embeddings) — same as Postgres, plus a dimensions hint
vdb, err := client.ProvisionVector(ctx, &instant.VectorOpts{
    ProvisionOpts: instant.ProvisionOpts{Name: "embeddings"},
    Dimensions:    1536, // 0 → server default (1536)
})
// vdb.ConnectionURL → postgres://...  vdb.Extension → "pgvector"  vdb.Dimensions
```

Anonymous resources expire after **24 hours**. Claim them permanently with a free account
(see [Claim](#claim) below or visit the URL in `result.Note`).

### Timeouts: provisioning runs longer than reads

Provisioning is **synchronous** — `ProvisionDatabase` / `Cache` / `MongoDB` /
`Queue` / `Vector` / `Storage` / `Webhook`, `Deploy`, and `CreateStack` block
while the API creates the real backend (or accepts the build). Under production
hot-pool contention a *fresh* Postgres provision
can take **more than 30 seconds**. If the client gave up at 30 s, the server
kept working and held a 60 s in-flight idempotency marker, so the next retry hit
`409 idempotency_key_in_progress` instead of succeeding.

To avoid that, the SDK gives provisioning + deploy calls a **120 s** per-request
deadline by default, while read calls (list, get, claim, delete, rotate) keep
the shorter **30 s** default. You do not need to do anything — the split is
automatic.

Override either with `WithTimeout`, which sets a single budget governing **both**
read and provisioning calls:

```go
// One 90 s budget for every call — set high enough to outlive a slow provision.
client := instant.New(instant.WithTimeout(90 * time.Second))
```

A deadline you set on the `context.Context` you pass in is always honoured if it
is *tighter* than the SDK's provisioning budget — the SDK only lengthens an
open-ended context, it never overrides a shorter caller deadline.

---

## Deploy

Build and deploy an application from a gzipped tarball. The SDK uploads as
`multipart/form-data` to `POST /deploy/new`, optionally with env vars set on the first
build (avoiding the deploy → patch env → redeploy round-trip).

```go
f, _ := os.Open("build.tar.gz")
defer f.Close()

d, err := client.Deploy(ctx, instant.DeployOpts{
    Tarball:        f,
    Name:           "my-api",
    Port:           8080,
    Env:            "production",
    EnvVars:        map[string]string{"DATABASE_URL": "vault://DATABASE_URL"},
    IdempotencyKey: "first-deploy-build-7",
})
if err != nil { log.Fatal(err) }
fmt.Println("deploy id:", d.ID, "status:", d.Status, "url:", d.URL)
```

`Deployment.Status` is one of `building`, `deploying`, `healthy`, `failed`, `stopped`.
Poll the deploy by id via the live API to watch it reach a terminal state.

`Deploy` (POST /deploy/new) requires an API key. To deploy **anonymously** — no
account, exactly like provisioning a database — use `CreateStack` (a single-service
stack is a complete app):

```go
f, _ := os.Open("api.tar.gz")
defer f.Close()

st, err := client.CreateStack(ctx, instant.CreateStackOpts{
    Name: "my-app",
    Env:  "production",
    Services: []instant.StackServiceSpec{{
        Name:    "api",
        Tarball: f,
        Port:    8080,
        Expose:  true, // public Ingress + TLS
    }},
})
if err != nil { log.Fatal(err) }

// Poll until the build finishes.
for {
    st, _ = client.GetStack(ctx, st.Slug)
    if st.Status != "building" { break }
    time.Sleep(2 * time.Second)
}
for _, svc := range st.Services {
    fmt.Printf("%s  %s  %s\n", svc.Name, svc.Status, svc.URL)
}
```

When a deploy fails, `DeploymentEvents` returns the failure-autopsy timeline (build
exit reason, last log lines, a remediation hint) so an agent can self-correct:

```go
evs, _ := client.DeploymentEvents(ctx, d.AppID, 0) // 0 = server default limit
for _, e := range evs.Events {
    fmt.Printf("%s/%s: %s\n%s\n", e.Kind, e.Reason, e.Hint, e.LastLines)
}
```

### What's NOT covered yet

This SDK exposes a focused slice of the platform surface. The full agent API documents
~90+ additional endpoints across deployments management (`GET /deploy/:id`,
`POST /deploy/:id/redeploy`, `DELETE /deploy/:id`),
stack mutation (`POST /stacks/:slug/redeploy`,
`DELETE /stacks/:slug`), vault reads (`GET /api/v1/vault/:env[/:key]`,
`POST /api/v1/vault/copy`), billing (`POST /api/v1/billing/checkout`,
`/api/v1/billing/usage`), team management, env-twin / promotion, audit, webhook
receivers, custom domains, GitHub App connections, and more.

See the [full OpenAPI](https://api.instanode.dev/openapi.json) for the canonical list.

A `go-instanode` v1.x will widen the surface; for now an `*http.Client` against the
documented endpoints is the recommended path for anything not yet wrapped here.

---

## Resource management (authenticated)

Set `INSTANT_API_KEY` or pass `WithAPIKey` to access management endpoints.

```go
client := instant.New(instant.WithAPIKey("inst_live_..."))

// List all resources for your team
list, err := client.ListResources(ctx)
for _, r := range list.Items {
    fmt.Printf("%s  %s  %s\n", r.ResourceType, r.Token, r.Status)
}

// Get a single resource
r, err := client.GetResource(ctx, token)

// Delete a resource
err = client.DeleteResource(ctx, token)

// Rotate credentials (generates a new password, returns updated connection URL)
result, err := client.RotateCredentials(ctx, token)
fmt.Println("new URL:", result.ConnectionURL)
```

---

## Claim

Convert anonymous resources into permanent ones by claiming them with an email address.
The upgrade URL is embedded in every provision response's `Note` field:

```
Works now. Free forever with a free account: https://instanode.dev/start?t=<jwt>
```

Extract the `t` query parameter and call `Claim`:

```go
result, err := client.Claim(ctx, instant.ClaimOpts{
    JWT:      upgradeToken,     // the "t=" value from the upgrade URL
    Email:    "dev@example.com",
    TeamName: "Acme Corp",      // optional
})
if instant.IsConflict(err) {
    fmt.Println("already claimed — log in at https://instanode.dev")
    return
}
if err != nil {
    log.Fatal(err)
}
fmt.Println("team_id:", result.TeamID)
```

---

## Client configuration

```go
// All options (all are optional)
client := instant.New(
    instant.WithAPIKey("inst_live_..."),           // default: INSTANT_API_KEY env var
    instant.WithBaseURL("http://localhost:8080"),   // default: INSTANT_API_URL or https://api.instanode.dev (port-forward svc/instant-api for local k8s)
    instant.WithTimeout(15 * time.Second),         // governs reads AND provisioning; defaults: reads 30s, provisioning 120s
    instant.WithHTTPClient(myClient),              // custom transport (tracing, TLS, etc.)
    instant.WithLogger(slog.Default()),            // advisory notices and upgrade prompts
)
```

Environment variables read by `New()` (overridden by explicit options):

| Variable | Purpose |
|---|---|
| `INSTANT_API_KEY` | Bearer token for authenticated requests |
| `INSTANT_API_URL` | Override base URL (useful for local dev) |

---

## Error handling

All errors returned by the SDK are either `*instant.APIError` (server-side) or a standard
Go error (network failure, context cancellation). Use the typed helpers to branch on status:

```go
_, err := client.GetResource(ctx, token)
if instant.IsNotFound(err) {
    fmt.Println("resource does not exist")
} else if instant.IsUnauthorized(err) {
    fmt.Println("invalid or missing API key")
} else if instant.IsRateLimited(err) {
    fmt.Println("slow down — daily limit reached")
} else if err != nil {
    log.Fatal(err)
}
```

| Helper | HTTP status |
|---|---|
| `IsNotFound(err)` | 404 |
| `IsUnauthorized(err)` | 401 |
| `IsForbidden(err)` | 403 |
| `IsRateLimited(err)` | 429 |
| `IsConflict(err)` | 409 |
| `IsServiceUnavailable(err)` | 503 |

`*APIError` exposes `StatusCode`, `Code` (machine-readable), and `Message` (human-readable).

---

## Tier limits

| Tier | Postgres | Redis | MongoDB | Price |
|---|---|---|---|---|
| anonymous | 10 MB / 2 conn | 5 MB | 5 MB / 2 conn | free, 24h TTL |
| hobby | 1 GB / 8 conn | 50 MB | 100 MB / 5 conn | $9 / mo |
| pro | 10 GB / 20 conn | 512 MB | 5 GB / 20 conn | $49 / mo |

Limits come from the live plan registry, not the SDK — see
[instanode.dev/pricing](https://instanode.dev/pricing) for the canonical, always-current
table (including the intermediate upsell tiers). Anonymous resources expire after 24h;
claim them with a free account to keep them.

---

## Examples

Runnable examples are in `examples/`:

```bash
# Provision Postgres, Redis, and NATS in parallel
go run ./examples/provision-all

# Agent-bootstrap: idempotent full-stack provisioning to .env
go run ./examples/agent-bootstrap
```

---

## Local development

Point the client at your local instanode.dev cluster (the in-cluster Service is ClusterIP,
so port-forward `svc/instant-api` first):

```bash
kubectl port-forward -n instant svc/instant-api 8080:8080 &
export INSTANT_API_URL=http://localhost:8080
go run ./examples/provision-all
```

Or in code:

```go
client := instant.New(instant.WithBaseURL("http://localhost:8080"))
```

---

## Running tests

```bash
go test ./...
```

Tests use `httptest.NewServer` — no network access required.

---

## Links

- Website: [https://instanode.dev](https://instanode.dev)
- Dashboard: [https://instanode.dev/dashboard](https://instanode.dev/dashboard)
- Upgrade / pricing: [https://instanode.dev/pricing](https://instanode.dev/pricing)
- Claim anonymous resources: [https://instanode.dev/start](https://instanode.dev/start)
