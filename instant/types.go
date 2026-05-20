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

	// KeyPrefix is set for Redis resources that use key-namespace isolation.
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
type ClaimOpts struct {
	// JWT is the onboarding token obtained from the upgrade URL query parameter (required).
	JWT string `json:"jwt"`

	// Email is the user's email address (required).
	Email string `json:"email"`

	// TeamName is an optional team name. Defaults to the email if not provided.
	TeamName string `json:"team_name,omitempty"`
}

// APIError is returned when the server responds with a 4xx or 5xx status code.
// It implements the error interface so it can be used directly in error comparisons.
type APIError struct {
	// StatusCode is the HTTP status code returned by the server.
	StatusCode int

	// Code is the machine-readable error code from the server (e.g. "not_found").
	Code string `json:"error"`

	// Message is the human-readable error description.
	Message string `json:"message"`

	// raw is the full response body for debugging.
	raw string
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("instant.dev API error %d (%s): %s", e.StatusCode, e.Code, e.Message)
	}
	if e.raw != "" {
		return fmt.Sprintf("instant.dev API error %d: %s", e.StatusCode, e.raw)
	}
	return fmt.Sprintf("instant.dev API error %d", e.StatusCode)
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
