# instanode.dev Go SDK

Zero-friction developer infrastructure in a single HTTP call.
Provision real Postgres databases, Redis caches, MongoDB databases,
and NATS queues — no account, no Docker, no setup.

**[https://instanode.dev](https://instanode.dev)**

```
go get instanode.dev/sdk/go
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

    "instanode.dev/sdk/go/instant"
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

### Resource Management (requires API key)

| Method | Signature | Description |
|---|---|---|
| `ListResources` | `(ctx) (*ResourceList, error)` | List all team resources |
| `GetResource` | `(ctx, token string) (*Resource, error)` | Get a resource by token |
| `DeleteResource` | `(ctx, token string) error` | Soft-delete a resource |
| `RotateCredentials` | `(ctx, token string) (*RotateResult, error)` | New password → return updated connection URL |

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
```

Anonymous resources expire after **24 hours**. Claim them permanently with a free account
(see [Claim](#claim) below or visit the URL in `result.Note`).

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
    instant.WithBaseURL("http://localhost:8080"),   // default: INSTANT_API_URL or https://instanode.dev (port-forward svc/instant-api for local k8s)
    instant.WithTimeout(15 * time.Second),         // default: 30s
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

| Tier | Postgres | Redis | MongoDB |
|---|---|---|---|
| anonymous | 10 MB / 2 conn | 5 MB | 5 MB / 2 conn |
| hobby | 500 MB / 5 conn | 25 MB | 100 MB / 5 conn |
| pro | 5 120 MB / 20 conn | 256 MB | 2 048 MB / 20 conn |
| team | unlimited | unlimited | unlimited |

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
