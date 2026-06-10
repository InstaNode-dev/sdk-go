package instant

// vault.go — write-side client for the encrypted secret vault
// (api/internal/handlers/vault.go). Secrets stored here are referenced from
// deploys as `vault://<env>/<KEY>` values in env_vars; the API decrypts them
// at deploy time so plaintext never lands in a deployment row.
//
// The vault is a paid-tier feature: anonymous/free callers receive a 403
// (vault_not_available). Both write verbs below require a valid API key.

import (
	"context"
	"fmt"
	"net/url"
)

const (
	// vaultPathPrefix is the authenticated vault endpoint family. Secret
	// coordinates (env, key) are appended as path-escaped segments.
	vaultPathPrefix = "/api/v1/vault/"

	// vaultRotateSuffix distinguishes POST .../rotate from the plain PUT —
	// functionally identical (both mint a new version) but recorded under a
	// distinct audit action so an intentional rotation is distinguishable
	// from an ordinary write in the vault audit log.
	vaultRotateSuffix = "/rotate"
)

// VaultWriteResult is returned by [Client.SetVaultKey] and
// [Client.RotateVaultKey]. The plaintext secret value is NEVER echoed back —
// only its coordinates and the freshly minted version.
type VaultWriteResult struct {
	// OK is always true on success.
	OK bool `json:"ok"`

	// Key is the secret key name as stored (e.g. "DATABASE_URL").
	Key string `json:"key"`

	// Env is the environment scope the secret lives under
	// (production / staging / development / ...).
	Env string `json:"env"`

	// Version is the version minted by this write. Every write creates a NEW
	// version: 1 on first create, 2+ on subsequent writes / rotates. Existing
	// deployments keep reading the version they resolved at deploy time until
	// they redeploy.
	Version int `json:"version"`
}

// vaultWriteBody is the JSON body for PUT /api/v1/vault/:env/:key and
// POST /api/v1/vault/:env/:key/rotate.
type vaultWriteBody struct {
	Value string `json:"value"`
}

// vaultSecretPath assembles /api/v1/vault/<env>/<key> with each user-supplied
// segment path-escaped so keys containing '.' (or any future characters)
// round-trip cleanly.
func vaultSecretPath(env, key string) string {
	return vaultPathPrefix + url.PathEscape(env) + "/" + url.PathEscape(key)
}

// SetVaultKey stores an encrypted secret via PUT /api/v1/vault/:env/:key.
//
// The API encrypts value with AES-256-GCM and stores it as a NEW version —
// v1 on first create, v2+ on subsequent writes. Old versions remain queryable
// until the key is deleted. Requires a valid API key; the vault is a
// paid-tier feature (anonymous/free callers receive a 403 *APIError).
//
// Store a secret here, then reference it as "vault://<env>/<KEY>" in
// [DeployOpts.EnvVars] (or a stack service's Env) and the API resolves the
// plaintext at deploy time.
//
// Example:
//
//	res, err := client.SetVaultKey(ctx, "production", "STRIPE_KEY", "sk_live_...")
//	if err != nil { log.Fatal(err) }
//	fmt.Println("stored version:", res.Version)
func (c *Client) SetVaultKey(ctx context.Context, env, key, value string) (*VaultWriteResult, error) {
	if err := validateVaultWriteArgs(env, key, value); err != nil {
		return nil, fmt.Errorf("SetVaultKey: %w", err)
	}
	var out VaultWriteResult
	if err := c.putJSON(ctx, vaultSecretPath(env, key), vaultWriteBody{Value: value}, &out); err != nil {
		return nil, fmt.Errorf("SetVaultKey: %w", err)
	}
	return &out, nil
}

// RotateVaultKey rotates a secret's value via
// POST /api/v1/vault/:env/:key/rotate.
//
// Functionally identical to [Client.SetVaultKey] (every write mints a new
// version) but recorded under a distinct audit action so the vault audit log
// distinguishes an intentional rotation from an ordinary write. value is the
// NEW secret value. Existing deployments continue to read the previous
// version until they redeploy — follow up with a redeploy to apply.
//
// Example:
//
//	res, err := client.RotateVaultKey(ctx, "production", "STRIPE_KEY", "sk_live_new")
//	if err != nil { log.Fatal(err) }
//	fmt.Println("rotated to version:", res.Version)
func (c *Client) RotateVaultKey(ctx context.Context, env, key, value string) (*VaultWriteResult, error) {
	if err := validateVaultWriteArgs(env, key, value); err != nil {
		return nil, fmt.Errorf("RotateVaultKey: %w", err)
	}
	var out VaultWriteResult
	if err := c.postJSON(ctx, vaultSecretPath(env, key)+vaultRotateSuffix, vaultWriteBody{Value: value}, &out); err != nil {
		return nil, fmt.Errorf("RotateVaultKey: %w", err)
	}
	return &out, nil
}

// validateVaultWriteArgs rejects empty coordinates / values client-side so a
// malformed call fails fast with an actionable message instead of a 400.
func validateVaultWriteArgs(env, key, value string) error {
	if env == "" {
		return fmt.Errorf("env is required")
	}
	if key == "" {
		return fmt.Errorf("key is required")
	}
	if value == "" {
		return fmt.Errorf("value is required")
	}
	return nil
}
