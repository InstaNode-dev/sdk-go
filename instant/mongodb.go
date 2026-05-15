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
// opts is REQUIRED and opts.Name must be a valid resource name (1–64 chars,
// matching ^[A-Za-z0-9][A-Za-z0-9 _-]*$). An invalid or missing name returns
// an error before any network request is made.
//
// Example:
//
//	mdb, err := client.ProvisionMongoDB(ctx, &instant.ProvisionOpts{Name: "app-mongo"})
//	if err != nil { log.Fatal(err) }
//	fmt.Println("mongodb URL:", mdb.ConnectionURL)
//
//	// Connect with mongo-driver:
//	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mdb.ConnectionURL))
func (c *Client) ProvisionMongoDB(ctx context.Context, opts *ProvisionOpts) (*ProvisionResult, error) {
	body, err := provisionBody(opts)
	if err != nil {
		return nil, fmt.Errorf("ProvisionMongoDB: %w", err)
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
