package instant

import (
	"context"
	"fmt"
)

// ProvisionCache provisions a new Redis cache namespace.
// No account is required. Anonymous resources expire after 24h unless claimed.
//
// Tier limits (see api/plans.yaml for the source of truth — fetch live via
// GET /api/v1/capabilities for runtime decisions):
//   Anonymous: 5 MB memory, 24h TTL
//   Hobby:     50 MB
//   Pro:       512 MB
//   Team:      unlimited
//
// The returned [ProvisionResult] may include a KeyPrefix field when the
// server uses key-namespace isolation instead of ACL isolation. In that case,
// prefix all Redis keys with this value.
//
// opts is REQUIRED and opts.Name must be a valid resource name (1–64 chars,
// matching ^[A-Za-z0-9][A-Za-z0-9 _-]*$). An invalid or missing name returns
// an error before any network request is made.
//
// Example:
//
//	cache, err := client.ProvisionCache(ctx, &instant.ProvisionOpts{Name: "app-cache"})
//	if err != nil { log.Fatal(err) }
//	fmt.Println("redis URL:", cache.ConnectionURL)
//
//	// Connect with go-redis:
//	rdb := redis.NewClient(&redis.Options{Addr: cache.ConnectionURL})
func (c *Client) ProvisionCache(ctx context.Context, opts *ProvisionOpts) (*ProvisionResult, error) {
	body, err := provisionBody(opts)
	if err != nil {
		return nil, fmt.Errorf("ProvisionCache: %w", err)
	}

	var result ProvisionResult
	if err := c.provisionJSONWithHeaders(ctx, "/cache/new", body, provisionHeaders(opts), &result); err != nil {
		return nil, fmt.Errorf("ProvisionCache: %w", err)
	}
	if result.Token == "" {
		return nil, fmt.Errorf("ProvisionCache: server returned empty token")
	}
	if result.ConnectionURL == "" {
		return nil, fmt.Errorf("ProvisionCache: server returned empty connection_url")
	}
	if result.Note != "" {
		c.logger.Info("instant.dev cache provisioned",
			"token", result.Token,
			"tier", result.Tier,
			"note", result.Note,
		)
	}
	return &result, nil
}
