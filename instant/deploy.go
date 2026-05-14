package instant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

// Deployment is the shape returned by Deploy and the deployment management
// endpoints. Mirrors the API's DeployResponse.item.
type Deployment struct {
	// ID is the deployment row UUID.
	ID string `json:"id"`

	// AppID is the 8-char public slug baked into the URL.
	AppID string `json:"app_id"`

	// URL is the live HTTPS endpoint. Empty until Status reaches "healthy".
	URL string `json:"url"`

	// Status is one of "building", "deploying", "healthy", "failed", "stopped".
	Status string `json:"status"`

	// Tier is the plan tier the deploy was created under.
	Tier string `json:"tier"`

	// Environment is the env scope (production / staging / development / ...).
	// Distinct from Env, which is the env-var map.
	Environment string `json:"environment"`

	// Env is the resolved env-var map injected into the deployed pod.
	Env map[string]string `json:"env,omitempty"`

	// Port is the container port the build exposes.
	Port int `json:"port"`

	// Private is true when the Ingress is restricted via allowed_ips.
	Private bool `json:"private,omitempty"`

	// AllowedIPs are the CIDRs / IPs whitelisted on a private deploy.
	AllowedIPs []string `json:"allowed_ips,omitempty"`

	// TeamID is the owning team UUID.
	TeamID string `json:"team_id,omitempty"`
}

// deployResponse is the wire shape: { ok, item, note }.
type deployResponse struct {
	OK   bool       `json:"ok"`
	Item Deployment `json:"item"`
	Note string     `json:"note,omitempty"`
}

// DeployOpts are the parameters for Deploy.
//
// Tarball is the gzipped build-context (Dockerfile + source) the API hands to
// kaniko. The SDK reads it once and uploads it as the 'tarball' multipart field.
type DeployOpts struct {
	// Tarball is the gzipped tar archive containing the Dockerfile + source
	// (max 50 MB enforced by the API).
	Tarball io.Reader

	// Name is an optional human-readable label.
	Name string

	// Port is the container port (defaults to 8080 server-side when 0).
	Port int

	// Env is the environment scope (production / staging / development).
	// Empty resolves to "development" server-side (migration 026).
	Env string

	// EnvVars is the env-var map injected into the deployed pod on the first
	// build. Avoids the deploy → patch env → redeploy round-trip. Values may
	// use the 'vault://KEY' form to reference secrets stored via /api/v1/vault.
	EnvVars map[string]string

	// IdempotencyKey is an optional Stripe/AWS-style replay guard. First
	// response is cached for 24h; replays return the cached body with
	// X-Idempotent-Replay: true.
	IdempotencyKey string
}

// Deploy provisions an application from a gzipped build context.
//
// The SDK uploads the tarball as multipart/form-data to POST /deploy/new,
// optionally with env_vars on the first build. The returned Deployment
// reflects the API's accepted-state (typically Status="building" /
// "deploying"); poll the same record by ID via the live API to watch it
// reach "healthy" or "failed".
//
// Deploy returns an *APIError on 4xx/5xx — distinguishing 402 from 409 lets
// callers branch on tier-gate vs idempotency-conflict without parsing strings.
//
// Example:
//
//	f, _ := os.Open("build.tar.gz")
//	defer f.Close()
//	d, err := client.Deploy(ctx, instant.DeployOpts{
//	    Tarball: f,
//	    Name:    "my-api",
//	    Env:     "production",
//	    EnvVars: map[string]string{"PORT": "8080"},
//	})
//	if err != nil { log.Fatal(err) }
//	fmt.Println("deploy id:", d.ID, "status:", d.Status)
func (c *Client) Deploy(ctx context.Context, opts DeployOpts) (*Deployment, error) {
	if opts.Tarball == nil {
		return nil, fmt.Errorf("instant: Deploy requires a non-nil Tarball reader")
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	// tarball field — required
	tarballPart, err := mw.CreateFormFile("tarball", "build.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("instant: building multipart tarball field: %w", err)
	}
	if _, err := io.Copy(tarballPart, opts.Tarball); err != nil {
		return nil, fmt.Errorf("instant: reading tarball: %w", err)
	}

	// Optional fields — all string form values per the API contract.
	if opts.Name != "" {
		if err := mw.WriteField("name", opts.Name); err != nil {
			return nil, fmt.Errorf("instant: writing name field: %w", err)
		}
	}
	if opts.Port > 0 {
		if err := mw.WriteField("port", fmt.Sprintf("%d", opts.Port)); err != nil {
			return nil, fmt.Errorf("instant: writing port field: %w", err)
		}
	}
	if opts.Env != "" {
		if err := mw.WriteField("env", opts.Env); err != nil {
			return nil, fmt.Errorf("instant: writing env field: %w", err)
		}
	}
	if len(opts.EnvVars) > 0 {
		envJSON, err := json.Marshal(opts.EnvVars)
		if err != nil {
			return nil, fmt.Errorf("instant: marshalling env_vars: %w", err)
		}
		if err := mw.WriteField("env_vars", string(envJSON)); err != nil {
			return nil, fmt.Errorf("instant: writing env_vars field: %w", err)
		}
	}

	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("instant: closing multipart writer: %w", err)
	}

	url := c.baseURL + "/deploy/new"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return nil, fmt.Errorf("instant: building deploy request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if opts.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", opts.IdempotencyKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("instant: deploy request failed: %w", err)
	}
	defer resp.Body.Close()

	c.logHeaders(resp)

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		apiErr := &APIError{StatusCode: resp.StatusCode, raw: string(raw)}
		_ = json.Unmarshal(raw, apiErr)
		return nil, apiErr
	}

	var out deployResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("instant: decoding deploy response: %w", err)
	}
	return &out.Item, nil
}
