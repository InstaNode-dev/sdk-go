package instant

// resource_lifecycle.go — pause / resume operate verbs on a provisioned
// resource (POST /api/v1/resources/:id/{pause,resume},
// api/internal/handlers/resource.go Pause/Resume).

import (
	"context"
	"fmt"
	"net/url"
)

const (
	// resourcePathPrefix is the authenticated resource-management endpoint
	// family. The resource token is appended as a path-escaped segment.
	resourcePathPrefix = "/api/v1/resources/"

	// resourcePauseSuffix is the suspend sub-resource.
	resourcePauseSuffix = "/pause"

	// resourceResumeSuffix is the un-pause sub-resource.
	resourceResumeSuffix = "/resume"
)

// PauseResumeResult is returned by [Client.PauseResource] and
// [Client.ResumeResource].
type PauseResumeResult struct {
	// OK is always true on success.
	OK bool `json:"ok"`

	// ID is the internal resource UUID.
	ID string `json:"id"`

	// Token is the resource token (echo of the value passed in).
	Token string `json:"token"`

	// Status is the resulting lifecycle state: "paused" after a pause,
	// "active" after a resume.
	Status string `json:"status"`

	// Message is the server's human-readable confirmation.
	Message string `json:"message,omitempty"`

	// Resource is the refreshed structured resource record. Nil if the
	// server omitted it.
	Resource *Resource `json:"resource,omitempty"`
}

// PauseResource suspends a resource WITHOUT deleting it via
// POST /api/v1/resources/:id/pause.
//
// token is the resource token (the same value the Provision* methods return).
// Storage is preserved and the connection URL is unchanged; the provider-side
// credential is revoked so new connections are refused until
// [Client.ResumeResource]. Paused resources stop counting against the
// per-type resource quota, but their storage still counts toward the storage
// cap.
//
// Tier-gated to Pro+ — lower tiers receive a 402 *APIError with an
// agent_action / upgrade_url. Pausing an already-paused resource returns a
// 409 ("already_paused"). Requires a valid API key; a missing or other-team
// token returns 404.
//
// Example:
//
//	res, err := client.PauseResource(ctx, db.Token)
//	if err != nil { log.Fatal(err) }
//	fmt.Println(res.Status) // "paused"
func (c *Client) PauseResource(ctx context.Context, token string) (*PauseResumeResult, error) {
	if token == "" {
		return nil, fmt.Errorf("PauseResource: token is required")
	}
	var out PauseResumeResult
	if err := c.post(ctx, resourcePathPrefix+url.PathEscape(token)+resourcePauseSuffix, &out); err != nil {
		return nil, fmt.Errorf("PauseResource: %w", err)
	}
	return &out, nil
}

// ResumeResource flips a paused resource back to "active" via
// POST /api/v1/resources/:id/resume.
//
// The connection URL is preserved unchanged — same password, same host, same
// database name — so any existing client config keeps working. There is no
// tier gate on resume: a team that owns a paused resource can always un-pause
// it regardless of its current plan tier (the Pro+ gate is enforced at pause
// time). Resuming a resource that is not paused returns a 409 *APIError
// ("not_paused"). Requires a valid API key; a missing or other-team token
// returns 404.
//
// Example:
//
//	res, err := client.ResumeResource(ctx, db.Token)
//	if err != nil { log.Fatal(err) }
//	fmt.Println(res.Status) // "active"
func (c *Client) ResumeResource(ctx context.Context, token string) (*PauseResumeResult, error) {
	if token == "" {
		return nil, fmt.Errorf("ResumeResource: token is required")
	}
	var out PauseResumeResult
	if err := c.post(ctx, resourcePathPrefix+url.PathEscape(token)+resourceResumeSuffix, &out); err != nil {
		return nil, fmt.Errorf("ResumeResource: %w", err)
	}
	return &out, nil
}
