package instant

import (
	"context"
	"fmt"
	"strings"
)

// storagePath is the agent-API endpoint that provisions an S3-compatible
// storage bucket prefix.
const storagePath = "/storage/new"

// StorageResult is returned by ProvisionStorage.
//
// A storage resource is an S3-compatible bucket prefix. Unlike the database,
// cache, mongodb, and queue results, it carries S3 credentials (an access key
// pair) and a per-token key prefix rather than a single connection string.
type StorageResult struct {
	// OK is always true on success.
	OK bool `json:"ok"`

	// ID is the internal resource UUID.
	ID string `json:"id"`

	// Token is the unique resource identifier used to reference this resource.
	Token string `json:"token"`

	// Name is the human-readable label, if one was set at provision time.
	Name string `json:"name,omitempty"`

	// ConnectionURL is the bucket URL scoped to this resource's prefix
	// (e.g. https://nyc3.digitaloceanspaces.com/instant-shared/abc12345/).
	ConnectionURL string `json:"connection_url"`

	// Endpoint is the S3-compatible API endpoint to point an S3 client at.
	Endpoint string `json:"endpoint"`

	// AccessKeyID is the S3 access key id for this resource.
	AccessKeyID string `json:"access_key_id"`

	// SecretAccessKey is the S3 secret access key for this resource.
	SecretAccessKey string `json:"secret_access_key"`

	// Prefix is the object-key prefix this resource is scoped to. All object
	// keys written by this resource must start with this prefix.
	Prefix string `json:"prefix"`

	// PresignURL is the absolute URL for the broker-mode presign endpoint.
	// Populated when the backend has no per-tenant credential model (DO
	// Spaces' shared-master-key mode): the tenant calls this URL with
	// {"key":"<object-key>","method":"GET|PUT"} to mint a short-lived
	// presigned S3 URL. Empty when the backend issued real credentials
	// (AccessKeyID / SecretAccessKey populated).
	//
	// Always absolute — the SDK rewrites the server's relative path
	// (e.g. "/storage/<token>/presign") to the configured base URL.
	PresignURL string `json:"presign_url,omitempty"`

	// Mode is the credential-isolation mode the server chose for this
	// resource. One of: "shared-master-key", "prefix-scoped",
	// "prefix-scoped-temporary", "broker". Surfaces what isolation the
	// tenant actually has; broker mode means AccessKeyID/SecretAccessKey
	// are empty and the tenant must use PresignURL instead.
	Mode string `json:"mode,omitempty"`

	// Tier is the plan tier this resource was provisioned under.
	Tier string `json:"tier"`

	// Env is the environment scope this resource was provisioned in
	// (development, staging, or production).
	Env string `json:"env,omitempty"`

	// Limits describes the storage limits for this resource.
	Limits ResourceLimits `json:"limits"`

	// Note contains an upgrade CTA or advisory message from the server.
	Note string `json:"note,omitempty"`

	// Upgrade is the URL the user can visit to upgrade their plan.
	Upgrade string `json:"upgrade,omitempty"`

	// UpgradeJWT is the raw onboarding JWT for programmatic claiming.
	UpgradeJWT string `json:"upgrade_jwt,omitempty"`

	// ExpiresAt is when the resource will be deleted (empty for permanent resources).
	ExpiresAt string `json:"expires_at,omitempty"`
}

// ProvisionStorage provisions a new S3-compatible storage bucket prefix.
// No account is required. Anonymous resources expire after 24h unless claimed.
//
// Tier limits (see api/plans.yaml for the source of truth — fetch live via
// GET /api/v1/capabilities for runtime decisions):
//   Anonymous: 10 MB, 24h TTL
//   Hobby:     512 MB
//   Pro:       50 GB
//   Team:      unlimited
//
// The response carries a `mode` field describing the credential isolation
// level: "shared-master-key" / "prefix-scoped" / "prefix-scoped-temporary"
// / "broker". Prod default is "prefix-scoped" (DO Spaces backend).
//
// The returned [StorageResult] carries S3 credentials (Endpoint, AccessKeyID,
// SecretAccessKey) and a per-token key Prefix. Configure any S3 client with
// the endpoint + credentials, and scope every object key under Prefix.
//
// opts is REQUIRED and opts.Name must be a valid resource name (1–64 chars,
// matching ^[A-Za-z0-9][A-Za-z0-9 _-]*$). An invalid or missing name returns
// an error before any network request is made.
//
// Example:
//
//	st, err := client.ProvisionStorage(ctx, &instant.ProvisionOpts{Name: "app-assets"})
//	if err != nil { log.Fatal(err) }
//	fmt.Println("endpoint:", st.Endpoint, "prefix:", st.Prefix)
func (c *Client) ProvisionStorage(ctx context.Context, opts *ProvisionOpts) (*StorageResult, error) {
	body, err := provisionBody(opts)
	if err != nil {
		return nil, fmt.Errorf("ProvisionStorage: %w", err)
	}

	var result StorageResult
	if err := c.postJSONWithHeaders(ctx, storagePath, body, provisionHeaders(opts), &result); err != nil {
		return nil, fmt.Errorf("ProvisionStorage: %w", err)
	}
	if result.Token == "" {
		return nil, fmt.Errorf("ProvisionStorage: server returned empty token")
	}
	// The secondary success invariant is connection_url, NOT endpoint.
	// On the fingerprint-dedup path (HTTP 200, the 6th-call response that
	// returns an already-provisioned storage resource) the agent API echoes
	// the resource's connection_url but omits the S3 credential fields
	// (endpoint, access_key_id, secret_access_key, prefix) — those are not
	// reconstructable from the stored resource row. Checking Endpoint here
	// turned every legitimate dedup response into a spurious error, unlike
	// ProvisionDatabase/Cache/MongoDB/Queue whose connection_url check is
	// satisfied on both the fresh and dedup paths. Mirror that contract.
	if result.ConnectionURL == "" {
		return nil, fmt.Errorf("ProvisionStorage: server returned empty connection_url")
	}
	// Broker mode (DO Spaces shared-master-key) returns a relative presign
	// path like "/storage/<token>/presign". SDK callers should never have to
	// reassemble URLs by hand — absolutize against the client's BaseURL so
	// every returned URL is directly usable.
	result.PresignURL = absoluteURL(c.baseURL, result.PresignURL)
	if result.Note != "" {
		c.logger.Info("instant.dev storage provisioned",
			"token", result.Token,
			"tier", result.Tier,
			"note", result.Note,
		)
	}
	return &result, nil
}

// absoluteURL converts a possibly-relative URL into an absolute one by joining
// it onto base. Returns the empty string unchanged, returns urlStr unchanged
// when it already carries a scheme. Exported helpers can rely on this for
// consistent absolute-URL behavior.
func absoluteURL(base, urlStr string) string {
	if urlStr == "" {
		return ""
	}
	if strings.HasPrefix(urlStr, "http://") || strings.HasPrefix(urlStr, "https://") {
		return urlStr
	}
	// Trim a leading slash so we don't double-slash when base already
	// carries the host root.
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(urlStr, "/")
}
