package instant

import (
	"context"
	"fmt"
)

// ProvisionCache provisions a new Redis cache namespace.
// No account is required. The cache expires after 24 h unless claimed.
//
// Anonymous limits: 5 MB memory.
// Hobby limits: 25 MB. Pro: 256 MB. Team: unlimited.
//
// The returned [ProvisionResult] may include a KeyPrefix field when the
// server uses key-namespace isolation instead of ACL isolation. In that case,
// prefix all Redis keys with this value.
//
// Example:
//
//	cache, err := client.ProvisionCache(ctx, nil)
//	if err != nil { log.Fatal(err) }
//	fmt.Println("redis URL:", cache.ConnectionURL)
//
//	// Connect with go-redis:
//	rdb := redis.NewClient(&redis.Options{Addr: cache.ConnectionURL})
func (c *Client) ProvisionCache(ctx context.Context, opts *ProvisionOpts) (*ProvisionResult, error) {
	body := map[string]string{}
	if opts != nil && opts.Name != "" {
		body["name"] = opts.Name
	}

	var result ProvisionResult
	if err := c.postJSON(ctx, "/cache/new", body, &result); err != nil {
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
