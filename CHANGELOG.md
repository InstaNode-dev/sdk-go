# Changelog

All notable changes to the Go SDK for instanode.dev are documented here.

The SDK follows semver: minor bumps add new API surface, major bumps break
existing callers.

## v0.3.0 — 2026-05-20 (BugBash B17)

### Fixed

- **P0:** `WithHTTPClient` no longer silently discards the caller's
  `*http.Client.Transport`. The SDK's auth transport is now layered on top of
  the caller's `Transport` so OpenTelemetry instrumentation, custom TLS, proxy
  injection, and other `RoundTripper` wrappers reach every request. Pre-fix
  behavior preserved only `Timeout`. Nil `*http.Client` arguments are now a
  no-op. `Jar` and `CheckRedirect` are also preserved.
- **P1:** Dropped the "14-day pro trial" claim from `Client.Claim` godoc.
  `Claim` is a one-time anonymous→free-but-claimed conversion; paid tiers
  require a separate Razorpay checkout.

### Added

- **`instant.SDKVersion`** — single-source-of-truth version constant. The
  User-Agent header is now `instant-go-sdk/<SDKVersion>`. CI fails when a
  `vX.Y.Z` git tag on the head commit does not match the constant.
- **`ProvisionOpts.IdempotencyKey`** (optional `string` field). When set the
  SDK forwards it as the `Idempotency-Key` header on every `/db/new`,
  `/cache/new`, `/nosql/new`, `/queue/new`, `/webhook/new`, and `/storage/new`
  call — matching the existing `DeployOpts.IdempotencyKey` behavior.
- **`Client.ListResourcesPage(ctx, ListResourcesOpts{Cursor, Limit})`** — paginated
  resource listing. The existing `Client.ListResources(ctx)` is now a
  zero-value wrapper. The returned `ResourceList` carries `NextCursor`.
- **`StorageResult.PresignURL`** — broker-mode presign endpoint. The SDK
  rewrites relative server-provided paths to absolute URLs against the
  client's base URL before returning.
- **`StorageResult.Mode`** — the credential-isolation mode the server chose
  (`shared-master-key`, `prefix-scoped`, `prefix-scoped-temporary`, `broker`).

### Behaviour notes for existing callers

- No existing public API was removed.
- Every new field on `ProvisionOpts` / `ResourceList` / `StorageResult` is
  zero-value-safe; existing callers do not need to change anything to keep
  working on v0.3.0.
- `ListResources` continues to fetch the first page with server-default page
  size when called with no options.

## v0.2.x and earlier

See git history.
