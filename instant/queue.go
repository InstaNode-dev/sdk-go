package instant

import (
	"context"
	"fmt"
)

// ProvisionQueue provisions a new NATS JetStream stream with scoped credentials.
// No account is required. Anonymous resources expire after 24h unless claimed.
//
// Tier limits (see api/plans.yaml for the source of truth — fetch live via
// GET /api/v1/capabilities for runtime decisions):
//   Anonymous: 1 GB, 24h TTL
//   Hobby:     5 GB
//   Pro:       10 GB
//   Team:      unlimited
//
// The response carries an `auth_mode` field: "isolated" (per-tenant JWT/NKEY,
// the default for new provisions) or "legacy_open" (grandfathered, no auth).
// New provisions land in isolated mode and the response includes nats_jwt /
// nats_nkey / creds_file for client wiring.
//
// opts is REQUIRED and opts.Name must be a valid resource name (1–64 chars,
// matching ^[A-Za-z0-9][A-Za-z0-9 _-]*$). An invalid or missing name returns
// an error before any network request is made.
//
// Example:
//
//	q, err := client.ProvisionQueue(ctx, &instant.ProvisionOpts{Name: "app-queue"})
//	if err != nil { log.Fatal(err) }
//	fmt.Println("nats URL:", q.ConnectionURL)
//
//	// Connect with nats.go:
//	nc, err := nats.Connect(q.ConnectionURL)
func (c *Client) ProvisionQueue(ctx context.Context, opts *ProvisionOpts) (*ProvisionResult, error) {
	body, err := provisionBody(opts)
	if err != nil {
		return nil, fmt.Errorf("ProvisionQueue: %w", err)
	}

	var result ProvisionResult
	if err := c.postJSON(ctx, "/queue/new", body, &result); err != nil {
		return nil, fmt.Errorf("ProvisionQueue: %w", err)
	}
	if result.Token == "" {
		return nil, fmt.Errorf("ProvisionQueue: server returned empty token")
	}
	if result.ConnectionURL == "" {
		return nil, fmt.Errorf("ProvisionQueue: server returned empty connection_url")
	}
	if result.Note != "" {
		c.logger.Info("instant.dev queue provisioned",
			"token", result.Token,
			"tier", result.Tier,
			"note", result.Note,
		)
	}
	return &result, nil
}
