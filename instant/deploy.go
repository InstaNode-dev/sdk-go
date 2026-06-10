package instant

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
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

	// Status is the lifecycle state of the deployment. The API contract
	// (OpenAPI DeploymentItem.status enum) emits exactly one of:
	//   "building"   — Kaniko is building the image
	//   "deploying"  — image built; k8s Deployment is rolling out
	//   "healthy"    — pod is Ready and serving on Port
	//   "failed"     — build or rollout failed; see logs for cause
	//   "stopped"    — deploy was administratively stopped or paused (scaled to zero)
	//   "expired"    — 24h TTL elapsed; the teardown reconciler will reap it
	//
	// There is no "queued" or "succeeded" status — the API never emits them.
	// "healthy" is the live-and-serving terminal-success state (do not branch
	// on "succeeded"). Still, callers branching on Status should default
	// unknown values to "still in flight" rather than failing — the API may
	// grow this set over time. Use [Deployment.URL] != "" as the canonical
	// "deploy is live" gate.
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

	// Deploy is a synchronous create like the /*/new provisioning endpoints:
	// the API blocks while the Kaniko build is kicked off and the row is
	// written. Use the provisioning client (no 30 s read-path cap) plus the
	// longer provisioning deadline so a slow accept under load doesn't time out
	// client-side and strand the build behind a 409 idempotency conflict.
	pctx, cancel := c.provisionContext(ctx)
	defer cancel()

	url := c.baseURL + "/deploy/new"
	req, err := http.NewRequestWithContext(pctx, http.MethodPost, url, &buf)
	if err != nil {
		return nil, fmt.Errorf("instant: building deploy request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if opts.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", opts.IdempotencyKey)
	}

	resp, err := c.provisionClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("instant: deploy request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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

// StreamDeploymentLogs streams Server-Sent Events from
// GET /deploy/:id/logs and writes each `data:` line to w, one log line per
// write (newline-terminated). Requires a valid API key (Bearer token).
//
// The connection is held open until any of:
//   - the deployment terminates (server closes the stream),
//   - ctx is cancelled,
//   - w returns an error on Write,
//   - the server returns 4xx/5xx (the function returns an *APIError immediately).
//
// On a 2xx response the function returns nil when the stream ends cleanly,
// or the first error encountered. Callers wanting to see "follow" behaviour
// for a running deployment should pass a long-running ctx and accept that
// this is a long-lived call.
//
// Example — tail until the deployment finishes:
//
//	if err := client.StreamDeploymentLogs(ctx, d.AppID, os.Stdout); err != nil {
//	    log.Fatal(err)
//	}
//
// Closes the gap noted in B17-P1-7: previously SDK callers had to drop down
// to raw http.Client to consume /deploy/:id/logs.
func (c *Client) StreamDeploymentLogs(ctx context.Context, appID string, w io.Writer) error {
	if appID == "" {
		return fmt.Errorf("instant: StreamDeploymentLogs requires a non-empty appID")
	}
	if w == nil {
		return fmt.Errorf("instant: StreamDeploymentLogs requires a non-nil writer")
	}
	url := c.baseURL + "/deploy/" + appID + "/logs"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("instant: building stream request: %w", err)
	}
	// Hint the server we expect SSE; the handler already sets the response
	// Content-Type to text/event-stream but Accept makes our intent explicit
	// to any intermediate proxies.
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("instant: stream request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	c.logHeaders(resp)

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		apiErr := &APIError{StatusCode: resp.StatusCode, raw: string(raw)}
		_ = json.Unmarshal(raw, apiErr)
		return apiErr
	}

	// Parse the SSE wire format: every event ends with a blank line; only
	// `data: ` lines carry payload. Comment lines (`: ping`) keep the
	// connection alive — drop them. Anything else (`event:`, `id:`,
	// `retry:`) is ignored.
	scanner := bufio.NewScanner(resp.Body)
	// Bump the max line size from the 64 KB default — build log lines can
	// exceed that (stack traces, pretty-printed JSON in customer logs).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimPrefix(line, "data:")
		payload = strings.TrimPrefix(payload, " ") // single optional space per RFC
		if _, err := io.WriteString(w, payload+"\n"); err != nil {
			return fmt.Errorf("instant: writing log line: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		// io.EOF from a clean server close is not surfaced by bufio.Scanner;
		// any error here is a real read/parse problem.
		return fmt.Errorf("instant: reading log stream: %w", err)
	}
	return nil
}
