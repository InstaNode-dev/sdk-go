package instant

import (
	"context"
	"fmt"
)

// WebhookLimits describes the limits for a provisioned webhook receiver.
type WebhookLimits struct {
	// RequestsStored is the maximum number of inbound requests retained for
	// later inspection. The oldest requests are dropped past this cap.
	RequestsStored int `json:"requests_stored,omitempty"`

	// ExpiresIn is the TTL for anonymous webhooks (e.g. "24h").
	ExpiresIn string `json:"expires_in,omitempty"`
}

// WebhookResult is returned by ProvisionWebhook.
//
// A webhook resource is an inbound HTTP receiver. Unlike the database, cache,
// mongodb, and queue results, it exposes a ReceiveURL (the public URL that
// accepts inbound payloads) instead of a connection string, and carries no
// internal_url.
type WebhookResult struct {
	// OK is always true on success.
	OK bool `json:"ok"`

	// ID is the internal resource UUID.
	ID string `json:"id"`

	// Token is the unique resource identifier used to reference this resource.
	Token string `json:"token"`

	// Name is the human-readable label, if one was set at provision time.
	Name string `json:"name,omitempty"`

	// ReceiveURL is the public URL that accepts inbound webhook payloads.
	// POST any payload here; retrieve stored requests via the resource API.
	ReceiveURL string `json:"receive_url"`

	// Tier is the plan tier this resource was provisioned under.
	Tier string `json:"tier"`

	// Env is the environment scope this resource was provisioned in
	// (development, staging, or production).
	Env string `json:"env,omitempty"`

	// Limits describes the request-retention limits for this webhook.
	Limits WebhookLimits `json:"limits"`

	// Note contains an upgrade CTA or advisory message from the server.
	Note string `json:"note,omitempty"`

	// Upgrade is the URL the user can visit to upgrade their plan.
	Upgrade string `json:"upgrade,omitempty"`

	// UpgradeJWT is the raw onboarding JWT for programmatic claiming.
	UpgradeJWT string `json:"upgrade_jwt,omitempty"`

	// ExpiresAt is when the resource will be deleted (empty for permanent resources).
	ExpiresAt string `json:"expires_at,omitempty"`
}

// ProvisionWebhook provisions a new inbound webhook receiver.
// No account is required. Anonymous resources expire after 24h unless claimed.
//
// Tier limits (see api/plans.yaml for the source of truth — fetch live via
// GET /api/v1/capabilities for runtime decisions):
//   Anonymous: 100 stored requests, 24h TTL
//   Hobby:     1 000 stored
//   Pro:       10k stored
//   Team:      unlimited
//
// The returned [WebhookResult] exposes ReceiveURL — the public URL that
// accepts inbound payloads. Stored requests can be retrieved through the
// resource management API.
//
// opts is REQUIRED and opts.Name must be a valid resource name (1–64 chars,
// matching ^[A-Za-z0-9][A-Za-z0-9 _-]*$). An invalid or missing name returns
// an error before any network request is made.
//
// Example:
//
//	wh, err := client.ProvisionWebhook(ctx, &instant.ProvisionOpts{Name: "stripe-hook"})
//	if err != nil { log.Fatal(err) }
//	fmt.Println("receive URL:", wh.ReceiveURL)
func (c *Client) ProvisionWebhook(ctx context.Context, opts *ProvisionOpts) (*WebhookResult, error) {
	body, err := provisionBody(opts)
	if err != nil {
		return nil, fmt.Errorf("ProvisionWebhook: %w", err)
	}

	var result WebhookResult
	if err := c.postJSON(ctx, "/webhook/new", body, &result); err != nil {
		return nil, fmt.Errorf("ProvisionWebhook: %w", err)
	}
	if result.Token == "" {
		return nil, fmt.Errorf("ProvisionWebhook: server returned empty token")
	}
	if result.ReceiveURL == "" {
		return nil, fmt.Errorf("ProvisionWebhook: server returned empty receive_url")
	}
	if result.Note != "" {
		c.logger.Info("instant.dev webhook provisioned",
			"token", result.Token,
			"tier", result.Tier,
			"note", result.Note,
		)
	}
	return &result, nil
}
