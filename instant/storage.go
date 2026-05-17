package instant

import (
	"context"
	"fmt"
)

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
// No account is required. The bucket prefix expires after 24 h unless claimed.
//
// Anonymous limits: 10 MB storage.
// Paid tiers raise the storage cap — see the dashboard for the current limits.
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
	if err := c.postJSON(ctx, "/storage/new", body, &result); err != nil {
		return nil, fmt.Errorf("ProvisionStorage: %w", err)
	}
	if result.Token == "" {
		return nil, fmt.Errorf("ProvisionStorage: server returned empty token")
	}
	if result.Endpoint == "" {
		return nil, fmt.Errorf("ProvisionStorage: server returned empty endpoint")
	}
	if result.Note != "" {
		c.logger.Info("instant.dev storage provisioned",
			"token", result.Token,
			"tier", result.Tier,
			"note", result.Note,
		)
	}
	return &result, nil
}
