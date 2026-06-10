package instant

import (
	"context"
	"fmt"
)

// VectorOpts are the parameters for [Client.ProvisionVector].
//
// It embeds [ProvisionOpts] (Name is REQUIRED; IdempotencyKey is optional) and
// adds the pgvector-specific Dimensions hint.
type VectorOpts struct {
	ProvisionOpts

	// Dimensions is the embedding width hint echoed back on the response.
	//
	// It is metadata only — pgvector lets you pick the column width at
	// table-create time, so this does not constrain the database. 0 leaves the
	// field off the request and the API defaults it to 1536 (OpenAI
	// text-embedding-ada-002). The API rejects values outside 1..16000
	// (pgvector's hard upper bound) with a 400 invalid_dimensions.
	Dimensions int `json:"dimensions,omitempty"`
}

// ProvisionVector provisions a pgvector-enabled Postgres database via
// POST /vector/new. The provisioning pipeline, connection-string format,
// AES-at-rest storage, and tier limits are identical to [Client.ProvisionDatabase]
// — the only deltas are the resource_type tag and the extra Extension /
// Dimensions fields on the response.
//
// No account is required. Anonymous resources expire after 24h unless claimed.
//
// Tier limits mirror Postgres exactly (the underlying storage IS Postgres):
//
//	Anonymous: 10 MB, 2 connections, 24h TTL
//	Hobby:     1 GB, 8 connections
//	Pro:       10 GB, 20 connections
//	Team:      unlimited
//
// opts is REQUIRED and opts.Name must be a valid resource name (1–64 chars,
// matching ^[A-Za-z0-9][A-Za-z0-9 _-]*$). An invalid or missing name returns
// an error before any network request is made.
//
// Example:
//
//	vdb, err := client.ProvisionVector(ctx, &instant.VectorOpts{
//	    ProvisionOpts: instant.ProvisionOpts{Name: "embeddings"},
//	    Dimensions:    1536,
//	})
//	if err != nil { log.Fatal(err) }
//	fmt.Println("pgvector URL:", vdb.ConnectionURL, "dims:", vdb.Dimensions)
func (c *Client) ProvisionVector(ctx context.Context, opts *VectorOpts) (*VectorResult, error) {
	if opts == nil {
		return nil, fmt.Errorf("ProvisionVector: opts is required: a non-nil *VectorOpts with a valid Name must be supplied")
	}
	if err := validateResourceName(opts.Name); err != nil {
		return nil, fmt.Errorf("ProvisionVector: %w", err)
	}
	if opts.Dimensions < 0 {
		return nil, fmt.Errorf("ProvisionVector: Dimensions must be >= 0 (0 = server default), got %d", opts.Dimensions)
	}

	body := map[string]any{"name": opts.Name}
	if opts.Dimensions > 0 {
		body["dimensions"] = opts.Dimensions
	}

	var result VectorResult
	if err := c.provisionJSONWithHeaders(ctx, "/vector/new", body, provisionHeaders(&opts.ProvisionOpts), &result); err != nil {
		return nil, fmt.Errorf("ProvisionVector: %w", err)
	}
	if result.Token == "" {
		return nil, fmt.Errorf("ProvisionVector: server returned empty token")
	}
	if result.ConnectionURL == "" {
		return nil, fmt.Errorf("ProvisionVector: server returned empty connection_url")
	}
	if result.Note != "" {
		c.logger.Info("instant.dev vector provisioned",
			"token", result.Token,
			"tier", result.Tier,
			"dimensions", result.Dimensions,
			"note", result.Note,
		)
	}
	return &result, nil
}
