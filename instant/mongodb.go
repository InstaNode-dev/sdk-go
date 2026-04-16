package instant

import (
	"context"
	"fmt"
)

// ProvisionMongoDB provisions a new MongoDB database and scoped user.
// No account is required. The database expires after 24 h unless claimed.
//
// Anonymous limits: 5 MB storage, 2 connections.
// Hobby limits: 100 MB, 5 connections. Pro: 2 048 MB, 20 connections.
//
// Example:
//
//	mdb, err := client.ProvisionMongoDB(ctx, nil)
//	if err != nil { log.Fatal(err) }
//	fmt.Println("mongodb URL:", mdb.ConnectionURL)
//
//	// Connect with mongo-driver:
//	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mdb.ConnectionURL))
func (c *Client) ProvisionMongoDB(ctx context.Context, opts *ProvisionOpts) (*ProvisionResult, error) {
	body := map[string]string{}
	if opts != nil && opts.Name != "" {
		body["name"] = opts.Name
	}

	var result ProvisionResult
	if err := c.postJSON(ctx, "/nosql/new", body, &result); err != nil {
		return nil, fmt.Errorf("ProvisionMongoDB: %w", err)
	}
	if result.Token == "" {
		return nil, fmt.Errorf("ProvisionMongoDB: server returned empty token")
	}
	if result.ConnectionURL == "" {
		return nil, fmt.Errorf("ProvisionMongoDB: server returned empty connection_url")
	}
	if result.Note != "" {
		c.logger.Info("instant.dev mongodb provisioned",
			"token", result.Token,
			"tier", result.Tier,
			"note", result.Note,
		)
	}
	return &result, nil
}
