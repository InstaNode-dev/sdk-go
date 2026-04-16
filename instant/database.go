package instant

import (
	"context"
	"fmt"
)

// ProvisionDatabase provisions a new Postgres database.
// No account is required. The database expires after 24 h unless claimed.
//
// Anonymous limits: 10 MB storage, 2 connections.
// Hobby limits: 500 MB, 5 connections. Pro: 5 120 MB, 20 connections.
//
// Example:
//
//	db, err := client.ProvisionDatabase(ctx, nil)
//	if err != nil { log.Fatal(err) }
//	fmt.Println("postgres URL:", db.ConnectionURL)
//
//	// Connect with database/sql:
//	sqlDB, err := sql.Open("postgres", db.ConnectionURL)
func (c *Client) ProvisionDatabase(ctx context.Context, opts *ProvisionOpts) (*ProvisionResult, error) {
	body := map[string]string{}
	if opts != nil && opts.Name != "" {
		body["name"] = opts.Name
	}

	var result ProvisionResult
	if err := c.postJSON(ctx, "/db/new", body, &result); err != nil {
		return nil, fmt.Errorf("ProvisionDatabase: %w", err)
	}
	if result.Token == "" {
		return nil, fmt.Errorf("ProvisionDatabase: server returned empty token")
	}
	if result.ConnectionURL == "" {
		return nil, fmt.Errorf("ProvisionDatabase: server returned empty connection_url")
	}
	if result.Note != "" {
		c.logger.Info("instant.dev database provisioned",
			"token", result.Token,
			"tier", result.Tier,
			"note", result.Note,
		)
	}
	return &result, nil
}
