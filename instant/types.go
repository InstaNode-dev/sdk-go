package instant

import (
	"errors"
	"fmt"
)

// ProvisionResult is returned by ProvisionDatabase, ProvisionCache, ProvisionMongoDB,
// and ProvisionQueue.
type ProvisionResult struct {
	// OK is always true on success.
	OK bool `json:"ok"`

	// ID is the internal resource UUID.
	ID string `json:"id"`

	// Token is the unique resource identifier used to reference this resource.
	Token string `json:"token"`

	// ConnectionURL is the plaintext connection string for the provisioned resource.
	// For Postgres: postgres://user:pass@host:5432/dbname
	// For Redis:    redis://:pass@host:6379
	// For MongoDB:  mongodb://user:pass@host:27017/dbname
	// For NATS:     nats://user:pass@host:4222
	ConnectionURL string `json:"connection_url"`

	// Tier is the plan tier this resource was provisioned under.
	Tier string `json:"tier"`

	// Name is the human-readable label, if one was set at provision time.
	Name string `json:"name,omitempty"`

	// Limits describes the storage and connection limits for this resource.
	Limits ResourceLimits `json:"limits"`

	// KeyPrefix is set for Redis resources on legacy backends that use
	// key-namespace isolation. Empty for the current Redis backend, which
	// isolates tenants via dedicated ACL users (no shared keyspace, no
	// prefix needed) — and empty for every non-Redis resource type. Kept
	// for backward compatibility with callers reading older responses.
	KeyPrefix string `json:"key_prefix,omitempty"`

	// Note contains an upgrade CTA or advisory message from the server.
	Note string `json:"note,omitempty"`

	// Warning is set when a limit is approaching or has been reached.
	Warning string `json:"warning,omitempty"`

	// Upgrade is the URL the user can visit to upgrade their plan.
	Upgrade string `json:"upgrade,omitempty"`

	// ExpiresAt is when the resource will be deleted (empty for permanent resources).
	ExpiresAt string `json:"expires_at,omitempty"`
}

// ResourceLimits describes the storage, memory, or connection limits for a provisioned resource.
type ResourceLimits struct {
	// StorageMB is the storage limit in megabytes (Postgres, MongoDB, NATS).
	StorageMB int `json:"storage_mb,omitempty"`

	// MemoryMB is the memory limit in megabytes (Redis).
	MemoryMB int `json:"memory_mb,omitempty"`

	// Connections is the maximum number of concurrent database connections.
	Connections int `json:"connections,omitempty"`

	// ExpiresIn is the TTL for anonymous resources (e.g. "24h").
	ExpiresIn string `json:"expires_in,omitempty"`
}

// Resource represents a provisioned resource as returned by the resource management API.
type Resource struct {
	// ID is the internal resource UUID.
	ID string `json:"id"`

	// Token is the unique resource identifier.
	Token string `json:"token"`

	// ResourceType is one of: postgres, redis, mongodb, queue, webhook, storage, etc.
	ResourceType string `json:"resource_type"`

	// Tier is the plan tier (anonymous, hobby, pro, team).
	Tier string `json:"tier"`

	// Status is the resource lifecycle state (active, deleted, suspended).
	Status string `json:"status"`

	// Name is the human-readable label, if one was set at provision time.
	Name string `json:"name,omitempty"`

	// CloudVendor is the cloud provider detected at provision time (e.g. "aws", "gcp").
	CloudVendor string `json:"cloud_vendor,omitempty"`

	// CountryCode is the ISO 3166-1 alpha-2 country code detected at provision time.
	CountryCode string `json:"country_code,omitempty"`

	// StorageBytes is the current storage usage in bytes.
	StorageBytes int64 `json:"storage_bytes,omitempty"`

	// StorageExceeded is true when the resource has exceeded its storage limit.
	StorageExceeded bool `json:"storage_exceeded,omitempty"`

	// ExpiresAt is when the resource will be deleted (empty for permanent resources).
	ExpiresAt string `json:"expires_at,omitempty"`

	// CreatedAt is the ISO 8601 timestamp when the resource was provisioned.
	CreatedAt string `json:"created_at"`
}

// ResourceList is returned by ListResources.
type ResourceList struct {
	// OK is always true on success.
	OK bool `json:"ok"`

	// Items contains the resources in this page.
	Items []Resource `json:"items"`

	// Total is the total count across all pages (when the server reports one),
	// or the number of items in this page if the server did not surface a
	// total. Callers should not rely on Total being non-zero — page using
	// NextCursor instead.
	Total int `json:"total"`

	// NextCursor is the cursor to pass to a subsequent ListResources call to
	// fetch the next page. Empty when there are no more pages. Older API
	// versions that do not paginate leave this empty on the first response.
	NextCursor string `json:"next_cursor,omitempty"`
}

// ListResourcesOpts are the parameters for paginated [Client.ListResources].
//
// Zero-value is the legacy "fetch first page with the server's default page
// size" behaviour. Page forward by re-issuing the call with
// Cursor=prevResult.NextCursor until NextCursor is empty.
type ListResourcesOpts struct {
	// Cursor is the opaque pagination token returned in the previous
	// response's NextCursor field. Empty asks for the first page.
	Cursor string

	// Limit caps the page size. 0 = server default (currently 100). The
	// server may clamp very large values; the SDK passes the value through
	// without client-side validation.
	Limit int
}

// ClaimResult is returned by Claim.
type ClaimResult struct {
	// OK is always true on success.
	OK bool `json:"ok"`

	// TeamID is the UUID of the newly created team.
	TeamID string `json:"team_id"`

	// UserID is the UUID of the newly created user.
	UserID string `json:"user_id"`

	// SessionToken is a freshly minted session JWT for the newly created
	// team, suitable for immediate use as the bearer token on follow-up
	// authenticated requests. Empty when the server elected not to mint one
	// (e.g. the [Client.ClaimTokens] path that supplies its own API key).
	//
	// 24h TTL; re-login on expiry (the API exposes no refresh endpoint).
	// Treat as a secret.
	SessionToken string `json:"session_token,omitempty"`

	// Message is a human-readable confirmation message.
	Message string `json:"message"`
}

// RotateResult is returned by RotateCredentials.
type RotateResult struct {
	// OK is always true on success.
	OK bool `json:"ok"`

	// ConnectionURL is the new plaintext connection string.
	ConnectionURL string `json:"connection_url"`
}

// ProvisionOpts are the parameters shared by all provision methods.
//
// Name is REQUIRED. Every provisioning endpoint rejects a request with a
// missing or invalid name with an HTTP 400. Callers must pass a non-nil
// *ProvisionOpts with a valid Name to every Provision* method.
type ProvisionOpts struct {
	// Name is a human-readable label stored with the resource (REQUIRED).
	//
	// It must be 1–64 characters and match ^[A-Za-z0-9][A-Za-z0-9 _-]*$
	// (start with a letter or digit; letters, digits, spaces, underscores,
	// and hyphens thereafter). The SDK validates this client-side before
	// sending the request.
	Name string `json:"name"`

	// IdempotencyKey is an optional Stripe/AWS-style replay guard. When set,
	// the SDK forwards it as the `Idempotency-Key` HTTP header. The API
	// caches the first response for 24h and replays return the cached body
	// with `X-Idempotent-Replay: true`.
	//
	// Recommended whenever the caller might retry a provision after a network
	// flake; without it, a retry can create a duplicate resource on the
	// server even when the first call succeeded.
	//
	// Not serialized into the JSON body.
	IdempotencyKey string `json:"-"`
}

// ClaimOpts are the parameters for the Claim method.
//
// Field-name policy (matches api ClaimRequest, 2026-05-20): Token is the
// canonical onboarding-token field. JWT is the deprecated alias kept so
// existing callers compile unchanged — when both are set, Token wins. New
// code should set Token only. The SDK now sends the canonical `token` wire
// field on every request, closing the three-name drift (jwt / token /
// INSTANODE_TOKEN) the api ClaimRequest doc explicitly flags.
type ClaimOpts struct {
	// Token is the canonical onboarding token obtained from the upgrade URL
	// query parameter "t" (required when JWT is unset).
	Token string `json:"token,omitempty"`

	// JWT is the deprecated alias for Token. Provided for backward
	// compatibility with existing code; new callers should use Token.
	// When both are set, Token wins.
	//
	// Deprecated: use Token.
	JWT string `json:"-"`

	// Email is the user's email address (required).
	Email string `json:"email"`

	// TeamName is an optional team name. Defaults to the email if not provided.
	TeamName string `json:"team_name,omitempty"`
}

// claimToken returns the canonical onboarding token from a ClaimOpts,
// preferring the new Token field and falling back to the deprecated JWT
// field. Centralised so every read site agrees on the precedence (mirrors
// api/internal/handlers/onboarding.go: ClaimRequest.claimToken).
func (o ClaimOpts) claimToken() string {
	if o.Token != "" {
		return o.Token
	}
	return o.JWT
}

// APIError is returned when the server responds with a 4xx or 5xx status code.
// It implements the error interface so it can be used directly in error comparisons.
//
// The instanode.dev API replies to every error with the canonical envelope:
//
//	{
//	  "ok": false,
//	  "error": "unauthorized",                 // category (Code below)
//	  "error_code": "missing_credentials",      // canonical machine code (ErrorCode below)
//	  "message": "...",                          // human-readable description
//	  "agent_action": "Have the user log in ...",// LLM-ready next step
//	  "upgrade_url": "https://instanode.dev/...",// where to upgrade/claim, if applicable
//	  "retry_after_seconds": 30,                 // null on 4xx, set on 429/502/503/504
//	  "request_id": "req_..."                    // correlation id for support
//	}
//
// Every field above has a home on this struct so the agent-native contract
// (machine code + the next action to take + where to upgrade) survives the
// round trip. APIErrorEnvelopeKeys is the authoritative list of envelope keys
// the SDK maps; a registry test asserts each one round-trips so a future API
// field can't be silently dropped.
type APIError struct {
	// StatusCode is the HTTP status code returned by the server.
	StatusCode int

	// Code is the error category from the server's "error" field
	// (e.g. "unauthorized", "not_found"). For the canonical machine-readable
	// code, prefer [APIError.CanonicalCode], which returns ErrorCode when the
	// server supplied the finer-grained "error_code" field and falls back to
	// this category otherwise.
	Code string `json:"error"`

	// ErrorCode is the canonical machine-readable error code from the server's
	// "error_code" field (e.g. "missing_credentials"). It is finer-grained than
	// Code (the category). Empty when the server only sent the category.
	ErrorCode string `json:"error_code"`

	// Message is the human-readable error description.
	Message string `json:"message"`

	// AgentAction is the LLM-ready next step the API recommends — a full
	// sentence an agent can relay to the user, usually carrying a concrete
	// instanode.dev URL. Empty when the server did not supply one.
	AgentAction string `json:"agent_action"`

	// UpgradeURL points at the page where the user can upgrade their plan or
	// claim their resources to clear the error. Empty when not applicable.
	UpgradeURL string `json:"upgrade_url"`

	// RetryAfterSeconds is the number of seconds the client should wait before
	// retrying. It is a pointer so the SDK can distinguish "retry in 0s" from
	// "do not retry" (null). Non-nil on 429/502/503/504; nil on 4xx that the
	// caller must fix rather than retry.
	RetryAfterSeconds *int `json:"retry_after_seconds"`

	// RequestID is the server-side correlation id for this request. Include it
	// when contacting support so the request can be traced.
	RequestID string `json:"request_id"`

	// raw is the full response body for debugging.
	raw string
}

// APIErrorEnvelopeKeys is the authoritative list of JSON keys the
// instanode.dev error envelope can emit that this SDK maps onto [APIError].
// Every key here MUST have a tagged field on APIError; the registry test
// (TestAPIError_EnvelopeKeysAllHaveAHome) iterates this list against the
// struct tags so a newly added API field can't silently drop. When the API
// adds an envelope key, add it here AND give it a tagged field on APIError in
// the same change.
//
// claim_url is intentionally excluded: it is a recycle-gate-only alias of
// upgrade_url and the SDK folds that surface into UpgradeURL.
var APIErrorEnvelopeKeys = []string{
	"error",
	"error_code",
	"message",
	"agent_action",
	"upgrade_url",
	"retry_after_seconds",
	"request_id",
}

// CanonicalCode returns the most specific machine-readable error code the
// server supplied: the finer-grained ErrorCode ("error_code") when present,
// falling back to the Code category ("error") otherwise. Branch on this rather
// than on Code directly so a caller keying off "missing_credentials" keeps
// working even though the category is the coarser "unauthorized".
func (e *APIError) CanonicalCode() string {
	if e.ErrorCode != "" {
		return e.ErrorCode
	}
	return e.Code
}

// Error implements the error interface. The canonical machine code, the
// human message, and — when the server supplied them — the agent_action and
// upgrade_url are folded into one line so a log entry is actionable without a
// second lookup.
func (e *APIError) Error() string {
	var s string
	if code := e.CanonicalCode(); code != "" {
		s = fmt.Sprintf("instant.dev API error %d (%s): %s", e.StatusCode, code, e.Message)
	} else if e.raw != "" {
		s = fmt.Sprintf("instant.dev API error %d: %s", e.StatusCode, e.raw)
	} else {
		s = fmt.Sprintf("instant.dev API error %d", e.StatusCode)
	}
	if e.AgentAction != "" {
		s += " | agent_action: " + e.AgentAction
	}
	if e.UpgradeURL != "" {
		s += " | upgrade_url: " + e.UpgradeURL
	}
	return s
}

// IsNotFound reports whether the error is a 404 Not Found.
func IsNotFound(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.StatusCode == 404
}

// IsUnauthorized reports whether the error is a 401 Unauthorized.
func IsUnauthorized(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.StatusCode == 401
}

// IsForbidden reports whether the error is a 403 Forbidden.
func IsForbidden(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.StatusCode == 403
}

// IsRateLimited reports whether the error is a 429 Too Many Requests.
func IsRateLimited(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.StatusCode == 429
}

// IsConflict reports whether the error is a 409 Conflict (e.g. JWT already claimed).
func IsConflict(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.StatusCode == 409
}

// IsServiceUnavailable reports whether the error is a 503 Service Unavailable.
func IsServiceUnavailable(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.StatusCode == 503
}
