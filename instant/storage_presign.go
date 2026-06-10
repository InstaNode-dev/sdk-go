package instant

// storage_presign.go — broker-mode presigned URL minting
// (POST /storage/:token/presign, api/internal/handlers/storage_presign.go).
//
// When the configured object-store backend cannot enforce per-tenant
// prefix-scoping at the IAM layer (DO Spaces today), /storage/new returns no
// long-lived credential (mode "broker") and the caller fetches one signed URL
// per object operation here instead.

import (
	"context"
	"fmt"
	"net/url"
)

const (
	// storagePathPrefix is the storage operate-verb endpoint family. The
	// storage resource token is appended as a path-escaped segment.
	storagePathPrefix = "/storage/"

	// storagePresignSuffix is the signed-URL minting sub-resource.
	storagePresignSuffix = "/presign"
)

// PresignOpts are the parameters for [Client.PresignStorage].
type PresignOpts struct {
	// Operation is the S3 verb the signed URL authorises: "GET" or "PUT"
	// (REQUIRED). DELETE is intentionally not permitted server-side — a
	// leaked URL must not be able to wipe a prefix.
	Operation string `json:"operation"`

	// Key is the object key, relative to the resource's tenant prefix
	// (REQUIRED). Leading slashes and "../" path-traversal components are
	// stripped server-side.
	Key string `json:"key"`

	// ExpiresIn is the lifetime of the signed URL in seconds. 0 means the
	// server default (600); the server clamps values above 3600 (1h).
	ExpiresIn int `json:"expires_in,omitempty"`
}

// PresignResult is returned by [Client.PresignStorage].
type PresignResult struct {
	// OK is always true on success.
	OK bool `json:"ok"`

	// URL is the signed S3 URL — usable with any plain HTTP client for the
	// given Method until ExpiresAt.
	URL string `json:"url"`

	// Method is the echo of the resolved HTTP verb the URL authorises.
	Method string `json:"method"`

	// Key is the object key relative to the tenant prefix, as resolved.
	Key string `json:"key"`

	// ObjectKey is the fully-qualified object key (prefix + key) the URL
	// signs.
	ObjectKey string `json:"object_key"`

	// ExpiresAt is the RFC3339 UTC expiry — the URL is invalid after this.
	ExpiresAt string `json:"expires_at"`
}

// PresignStorage mints a short-lived presigned S3 URL via
// POST /storage/:token/presign.
//
// token is the storage resource token from [Client.ProvisionStorage] — the
// token in the URL IS the credential (broker mode), so no API key is required
// and an anonymous caller can presign against a prefix it just provisioned.
//
// Error surfaces (all *APIError): 400 for an invalid token / operation / key,
// 404 when the resource doesn't exist, 410 when it is paused / expired /
// deleted, and 503 when object storage is not configured or signing failed.
//
// Example:
//
//	signed, err := client.PresignStorage(ctx, store.Token, instant.PresignOpts{
//	    Operation: "PUT",
//	    Key:       "uploads/avatar.png",
//	    ExpiresIn: 900,
//	})
//	if err != nil { log.Fatal(err) }
//	// http.NewRequest(signed.Method, signed.URL, body) — valid until signed.ExpiresAt
func (c *Client) PresignStorage(ctx context.Context, token string, opts PresignOpts) (*PresignResult, error) {
	if token == "" {
		return nil, fmt.Errorf("PresignStorage: token is required")
	}
	if opts.Operation == "" {
		return nil, fmt.Errorf("PresignStorage: Operation is required")
	}
	if opts.Key == "" {
		return nil, fmt.Errorf("PresignStorage: Key is required")
	}
	var out PresignResult
	path := storagePathPrefix + url.PathEscape(token) + storagePresignSuffix
	if err := c.postJSON(ctx, path, opts, &out); err != nil {
		return nil, fmt.Errorf("PresignStorage: %w", err)
	}
	return &out, nil
}
