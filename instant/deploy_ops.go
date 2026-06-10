package instant

// deploy_ops.go — operate verbs on an existing deployment: env-var mutation
// (PATCH /deploy/:id/env) and the scale-to-zero explicit wake
// (POST /deploy/:id/wake). Both live on the /deploy group alongside
// /deploy/new — NOT under /api/v1/deployments — mirroring the API's routing
// (api/internal/handlers/deploy.go).

import (
	"context"
	"fmt"
	"net/url"
)

const (
	// deployPathPrefix is the deployment operate-verb endpoint family. The
	// deployment's public app_id is appended as a path-escaped segment.
	deployPathPrefix = "/deploy/"

	// deployEnvSuffix is the PATCH env-merge sub-resource.
	deployEnvSuffix = "/env"

	// deployWakeSuffix is the scale-to-zero explicit-wake sub-resource.
	deployWakeSuffix = "/wake"
)

// DeployEnvUpdate is returned by [Client.UpdateDeployEnv].
type DeployEnvUpdate struct {
	// OK is always true on success.
	OK bool `json:"ok"`

	// Env is the FULL merged env map after the update, with secret values
	// redacted (consistent with GET /deploy/:id). The stored server-side map
	// is unredacted; only the response JSON is masked.
	Env map[string]string `json:"env"`

	// Note reminds the caller that a redeploy is required to apply the
	// change (e.g. "Run POST /deploy/<id>/redeploy to apply changes.").
	Note string `json:"note,omitempty"`
}

// WakeResult is returned by [Client.WakeDeployment].
type WakeResult struct {
	// OK is always true on success.
	OK bool `json:"ok"`

	// Message is the server's human-readable confirmation.
	Message string `json:"message,omitempty"`

	// Deployment is the refreshed deployment record (sleeping state cleared).
	// Nil if the server omitted it.
	Deployment *Deployment `json:"deployment,omitempty"`
}

// envUpdateBody is the JSON body for PATCH /deploy/:id/env and
// PATCH /stacks/:slug/env — both take {"env": {...}}.
type envUpdateBody struct {
	Env map[string]string `json:"env"`
}

// UpdateDeployEnv merges env vars into an existing deployment via
// PATCH /deploy/:id/env.
//
// id is the deployment's public app id ([Deployment.AppID]). The API MERGES
// the supplied keys into the deployment's existing env vars (incoming wins on
// collision) and returns the full merged map with secret values redacted.
// Values prefixed with "vault://" are stored verbatim and resolved at the
// next redeploy. Requires a valid API key; a missing or other-team deployment
// returns a 404 *APIError.
//
// The change is persisted but NOT applied until the deployment is redeployed
// — the returned [DeployEnvUpdate.Note] says so.
//
// Example:
//
//	res, err := client.UpdateDeployEnv(ctx, d.AppID, map[string]string{
//	    "FEATURE_X": "on",
//	    "API_KEY":   "vault://production/API_KEY",
//	})
//	if err != nil { log.Fatal(err) }
//	fmt.Println(res.Note)
func (c *Client) UpdateDeployEnv(ctx context.Context, id string, env map[string]string) (*DeployEnvUpdate, error) {
	if id == "" {
		return nil, fmt.Errorf("UpdateDeployEnv: id is required")
	}
	if len(env) == 0 {
		return nil, fmt.Errorf("UpdateDeployEnv: env must be a non-empty map")
	}
	var out DeployEnvUpdate
	path := deployPathPrefix + url.PathEscape(id) + deployEnvSuffix
	if err := c.patchJSON(ctx, path, envUpdateBody{Env: env}, &out); err != nil {
		return nil, fmt.Errorf("UpdateDeployEnv: %w", err)
	}
	return &out, nil
}

// WakeDeployment explicitly wakes a scaled-to-zero (sleeping) deployment via
// POST /deploy/:id/wake.
//
// id is the deployment's public app id ([Deployment.AppID]). On success the
// API scales the app back to one replica and refreshes its last-activity
// marker; the app becomes reachable once its pod is Ready (a one-time cold
// start — a request racing the wake gets the ingress's upstream-down response
// until then). Idempotent: waking an already-awake app just refreshes the
// activity marker.
//
// The wake surface is FLAG-GATED server-side: when scale-to-zero is not
// enabled on the platform (the default) the API returns a 501 *APIError with
// code "scale_to_zero_disabled". A transient scaling failure returns 503
// ("wake_failed") — safe to retry. Requires a valid API key; cross-tenant ids
// return 404.
//
// Example:
//
//	res, err := client.WakeDeployment(ctx, d.AppID)
//	if err != nil { log.Fatal(err) }
//	fmt.Println(res.Message)
func (c *Client) WakeDeployment(ctx context.Context, id string) (*WakeResult, error) {
	if id == "" {
		return nil, fmt.Errorf("WakeDeployment: id is required")
	}
	var out WakeResult
	if err := c.post(ctx, deployPathPrefix+url.PathEscape(id)+deployWakeSuffix, &out); err != nil {
		return nil, fmt.Errorf("WakeDeployment: %w", err)
	}
	return &out, nil
}
