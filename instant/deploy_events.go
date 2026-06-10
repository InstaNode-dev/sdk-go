package instant

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// DeploymentEvents fetches a deployment's failure-autopsy timeline via
// GET /api/v1/deployments/:id/events. It is the read surface behind rule 27:
// when a deploy fails silently (build pod GC'd, runtime never came up), the
// worker captures the exit reason, last log lines, and a remediation hint as
// deployment events — and this method exposes them so an agent can read the
// timeline and self-correct rather than re-uploading the same broken build.
//
// id is the deployment's public app id (the 8-char slug, [Deployment.AppID]).
// Requires a valid API key (Bearer token); a missing or other-team deployment
// returns an *APIError with StatusCode 404 — branch on [IsNotFound].
//
// limit caps the number of events returned. Pass 0 for the server default
// (currently 50); any value <= 0 is sent as the default.
//
// Example — inspect why a deploy failed:
//
//	evs, err := client.DeploymentEvents(ctx, d.AppID, 0)
//	if err != nil { log.Fatal(err) }
//	for _, e := range evs.Events {
//	    fmt.Printf("%s/%s: %s\n%s\n", e.Kind, e.Reason, e.Hint, e.LastLines)
//	}
func (c *Client) DeploymentEvents(ctx context.Context, id string, limit int) (*DeploymentEventList, error) {
	if id == "" {
		return nil, fmt.Errorf("DeploymentEvents: id is required")
	}
	path := "/api/v1/deployments/" + id + "/events"
	if limit > 0 {
		q := url.Values{}
		q.Set("limit", strconv.Itoa(limit))
		path += "?" + q.Encode()
	}

	var out DeploymentEventList
	if err := c.get(ctx, path, &out); err != nil {
		return nil, fmt.Errorf("DeploymentEvents: %w", err)
	}
	return &out, nil
}
