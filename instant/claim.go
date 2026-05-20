package instant

import (
	"context"
	"fmt"
)

// Claim converts an anonymous session into a registered team account.
//
// The JWT is the onboarding token obtained from the upgrade URL. When
// instanode.dev provisions an anonymous resource it returns a Note field
// containing a URL like https://instanode.dev/start?t=<jwt>. Extract the
// "t" query parameter and pass it here.
//
// Claim is one-time: anonymous (24h TTL) resources associated with the JWT's
// fingerprint are transferred to the new team and given a permanent (no-expiry)
// lifetime on the free tier. No trial period is started — paid tiers (hobby,
// pro, team) require a separate Razorpay checkout from the dashboard.
//
// Returns [*APIError] with StatusCode 409 if the JWT has already been claimed.
//
// Example:
//
//	result, err := client.Claim(ctx, instant.ClaimOpts{
//	    JWT:      upgradeToken,  // from "t" query param of the upgrade URL
//	    Email:    "dev@example.com",
//	    TeamName: "Acme Corp",   // optional; defaults to email
//	})
//	if instant.IsConflict(err) {
//	    fmt.Println("already claimed — log in instead")
//	    return
//	}
//	if err != nil { log.Fatal(err) }
//	fmt.Println("team_id:", result.TeamID)
func (c *Client) Claim(ctx context.Context, opts ClaimOpts) (*ClaimResult, error) {
	if opts.JWT == "" {
		return nil, fmt.Errorf("Claim: JWT is required")
	}
	if opts.Email == "" {
		return nil, fmt.Errorf("Claim: Email is required")
	}

	body := map[string]string{
		"jwt":   opts.JWT,
		"email": opts.Email,
	}
	if opts.TeamName != "" {
		body["team_name"] = opts.TeamName
	}

	var result ClaimResult
	if err := c.postJSON(ctx, "/claim", body, &result); err != nil {
		return nil, fmt.Errorf("Claim: %w", err)
	}
	return &result, nil
}

// ClaimTokens associates a list of anonymous resource tokens with an existing
// authenticated team. The caller supplies their API key explicitly, allowing
// this to be called from a context where the client was constructed without one.
//
// tokens must be non-empty. Each token is a value previously returned in a
// [ProvisionResult].
//
// Returns [*APIError] with StatusCode 409 if any token has already been claimed.
//
// Example:
//
//	result, err := client.ClaimTokens(ctx, "sk_live_...", []string{cache.Token, db.Token})
//	if err != nil { log.Fatal(err) }
//	fmt.Println("claimed:", result.Message)
func (c *Client) ClaimTokens(ctx context.Context, apiKey string, tokens []string) (*ClaimResult, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("ClaimTokens: apiKey is required")
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("ClaimTokens: tokens must not be empty")
	}

	body := map[string]interface{}{
		"tokens": tokens,
	}

	// Use a one-shot client that carries the supplied API key, leaving the
	// receiver's key untouched (the receiver may be anonymous).
	keyed := New(WithBaseURL(c.baseURL), WithAPIKey(apiKey), WithHTTPClient(c.httpClient))

	var result ClaimResult
	if err := keyed.postJSON(ctx, "/claim", body, &result); err != nil {
		return nil, fmt.Errorf("ClaimTokens: %w", err)
	}
	return &result, nil
}
