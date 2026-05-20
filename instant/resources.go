package instant

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// listResponse is the raw response shape from GET /api/v1/resources.
type listResponse struct {
	OK         bool       `json:"ok"`
	Items      []Resource `json:"items"`
	Total      int        `json:"total"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

// getResponse is the raw response shape from GET /api/v1/resources/:token.
type getResponse struct {
	OK   bool     `json:"ok"`
	Item Resource `json:"item"`
}

// deleteResponse is the raw response shape from DELETE /api/v1/resources/:token.
type deleteResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// ListResources returns the first page of resources belonging to the
// authenticated team. Requires a valid API key (Bearer token).
//
// This is a thin wrapper around [Client.ListResourcesPage] with a zero-value
// [ListResourcesOpts]. Use [Client.ListResourcesPage] (or pass options
// directly) when you need to iterate beyond the first page or set a smaller
// page size.
//
// Example:
//
//	list, err := client.ListResources(ctx)
//	if err != nil { log.Fatal(err) }
//	for _, r := range list.Items {
//	    fmt.Printf("%s  %s  %s\n", r.ResourceType, r.Token, r.Status)
//	}
func (c *Client) ListResources(ctx context.Context) (*ResourceList, error) {
	return c.ListResourcesPage(ctx, ListResourcesOpts{})
}

// ListResourcesPage returns one page of resources belonging to the
// authenticated team, honouring the supplied cursor + limit. Requires a valid
// API key (Bearer token).
//
// To iterate every page:
//
//	opts := instant.ListResourcesOpts{Limit: 50}
//	for {
//	    page, err := client.ListResourcesPage(ctx, opts)
//	    if err != nil { return err }
//	    for _, r := range page.Items {
//	        // ...
//	    }
//	    if page.NextCursor == "" { break }
//	    opts.Cursor = page.NextCursor
//	}
func (c *Client) ListResourcesPage(ctx context.Context, opts ListResourcesOpts) (*ResourceList, error) {
	path := "/api/v1/resources"
	q := url.Values{}
	if opts.Cursor != "" {
		q.Set("cursor", opts.Cursor)
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}

	var raw listResponse
	if err := c.get(ctx, path, &raw); err != nil {
		return nil, fmt.Errorf("ListResources: %w", err)
	}
	return &ResourceList{
		OK:         raw.OK,
		Items:      raw.Items,
		Total:      raw.Total,
		NextCursor: raw.NextCursor,
	}, nil
}

// GetResource returns a single resource by token.
// Requires a valid API key. Returns [*APIError] with StatusCode 404 if not found.
//
// Example:
//
//	r, err := client.GetResource(ctx, "3f4a7b2c-...")
//	if instant.IsNotFound(err) {
//	    fmt.Println("resource not found")
//	}
func (c *Client) GetResource(ctx context.Context, token string) (*Resource, error) {
	var raw getResponse
	if err := c.get(ctx, "/api/v1/resources/"+token, &raw); err != nil {
		return nil, fmt.Errorf("GetResource: %w", err)
	}
	return &raw.Item, nil
}

// DeleteResource soft-deletes a resource by token.
// Requires a valid API key and ownership of the resource.
// Returns [*APIError] with StatusCode 404 if not found, 403 if not owned.
//
// Example:
//
//	if err := client.DeleteResource(ctx, token); err != nil {
//	    log.Fatal("delete failed:", err)
//	}
func (c *Client) DeleteResource(ctx context.Context, token string) error {
	var raw deleteResponse
	if err := c.delete(ctx, "/api/v1/resources/"+token, &raw); err != nil {
		return fmt.Errorf("DeleteResource: %w", err)
	}
	return nil
}

// RotateCredentials generates a new password for a resource and returns the updated
// connection URL. This is the only endpoint that returns a plaintext connection_url
// for an existing resource.
//
// Requires a valid API key and ownership of the resource.
// The resource must expose a connection URL (some resource types do not).
//
// Example:
//
//	result, err := client.RotateCredentials(ctx, token)
//	if err != nil { log.Fatal("rotate failed:", err) }
//	fmt.Println("new URL:", result.ConnectionURL)
func (c *Client) RotateCredentials(ctx context.Context, token string) (*RotateResult, error) {
	var result RotateResult
	if err := c.post(ctx, "/api/v1/resources/"+token+"/rotate-credentials", &result); err != nil {
		return nil, fmt.Errorf("RotateCredentials: %w", err)
	}
	if result.ConnectionURL == "" {
		return nil, fmt.Errorf("RotateCredentials: server returned empty connection_url")
	}
	return &result, nil
}
