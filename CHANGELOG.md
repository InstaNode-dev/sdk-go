# Changelog

All notable changes to the Go SDK for instanode.dev are documented here.

The SDK follows semver: minor bumps add new API surface, major bumps break
existing callers.

## Unreleased

### Fixed

- **`Client.Claim` now sends the canonical `token` wire field instead of the
  deprecated `jwt` alias.** The api ClaimRequest doc names the Go SDK as one
  of three drift sources for the legacy `jwt` name (alongside the dashboard
  and MCP). The server still accepts both, so this is wire-compatible, but
  closes the drift.
- **`Deployment.Status` doc comment now matches the API contract.** It
  previously claimed the API emits `"queued"` (accepted-not-yet-built) and
  `"succeeded"` (terminal alias for healthy). Neither is in the OpenAPI
  `DeploymentItem.status` enum
  (`["building","deploying","healthy","failed","stopped","expired"]`) nor in
  any api code path — they were fictional, so a caller writing
  `if d.Status == "succeeded"` had a branch that never fired. The doc now
  lists exactly the contract statuses (and adds the previously-omitted
  `"expired"`). A registry-honesty test
  (`TestDeploymentStatusDocMatchesAPIContract`) parses the field's doc
  comment from the AST and fails if either ghost status reappears or a
  contract status is dropped. No wire/behavior change.
- **P0: `APIError` no longer drops the agent-native error envelope.** The api
  replies to every 4xx/5xx with
  `{ok, error, error_code, message, agent_action, upgrade_url,
  retry_after_seconds, request_id}`, but `APIError` captured only `error` +
  `message` — silently dropping `agent_action`, `error_code`, `upgrade_url`,
  `retry_after_seconds`, and `request_id`. Worse, `Code` was tagged
  `json:"error"` so it held the *category* (`"unauthorized"`) rather than the
  canonical machine code (`"missing_credentials"` in `error_code`). All five
  dropped fields now have tagged homes: `ErrorCode`, `AgentAction`,
  `UpgradeURL`, `RetryAfterSeconds *int`, `RequestID`. `Code`/`Message` are
  retained for back-compat. New `APIError.CanonicalCode()` returns the
  finer-grained `ErrorCode`, falling back to `Code`. `Error()` now folds
  `agent_action` + `upgrade_url` into the string so logs are actionable; the
  legacy `(code): message` shape is unchanged when neither is present. A
  registry test (`TestAPIError_EnvelopeKeysAllHaveAHome` +
  `…RegistryIsComplete`) asserts every envelope key the API can emit has a
  tagged field and round-trips, so a future field can't silently drop.

### Added

- **`Client.Capabilities(ctx)`** → `GET /api/v1/capabilities`. Returns the full
  tier matrix (`*Capabilities` with typed `[]TierCapabilities`: storage,
  connection, resource-count, and deployment caps per tier, plus durability
  and pricing) so an agent can discover "what can I do at which tier" without
  provisioning-and-failing. Public/unauthenticated — works in anonymous mode.
  The complete decoded JSON is also preserved on `Capabilities.Raw` for
  forward compatibility. Six provider doc comments already referenced this
  endpoint; this is the first method that calls it.
- **`APIError.ErrorCode` / `.AgentAction` / `.UpgradeURL` /
  `.RetryAfterSeconds` / `.RequestID`** — the previously-dropped error-envelope
  fields (see Fixed above), plus `APIError.CanonicalCode()` and the exported
  `APIErrorEnvelopeKeys` registry.

- **`ClaimResult.SessionToken`** (`string`, `json:"session_token,omitempty"`).
  Populated when the api mints a session JWT for the newly created team on
  `POST /claim`. Callers can use it as the Bearer token for follow-up
  authenticated requests with no separate login round-trip. 24h TTL.
- **`ClaimOpts.Token`** (`string`, `json:"token,omitempty"`) — canonical
  onboarding-token field, mirrors api `ClaimRequest.Token`. The existing
  `ClaimOpts.JWT` field is retained as a deprecated fallback (no JSON tag —
  read-only by the SDK on the client side); new callers should use `Token`.
  When both are set, `Token` wins.

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
