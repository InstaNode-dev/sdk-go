package instant

import (
	"context"
	"fmt"
)

// ProvisionQueue provisions a new NATS JetStream stream with scoped credentials.
// No account is required. The stream expires after 24 h unless claimed.
//
// Anonymous limits: 1 024 MB storage.
// Pro/Team: configurable via the dashboard.
//
// Example:
//
//	q, err := client.ProvisionQueue(ctx, nil)
//	if err != nil { log.Fatal(err) }
//	fmt.Println("nats URL:", q.ConnectionURL)
//
//	// Connect with nats.go:
//	nc, err := nats.Connect(q.ConnectionURL)
func (c *Client) ProvisionQueue(ctx context.Context, opts *ProvisionOpts) (*ProvisionResult, error) {
	body := map[string]string{}
	if opts != nil && opts.Name != "" {
		body["name"] = opts.Name
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
