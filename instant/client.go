// Package instant is the official Go SDK for instanode.dev.
//
// instanode.dev provisions real developer infrastructure — databases, caches,
// queues, document stores, and more — with a single HTTP call. No account required.
// Anonymous resources work immediately; claim them with an email to make them permanent.
//
// # Quickstart
//
//	client := instant.New()
//
//	// Provision a Postgres database — a name is required
//	db, err := client.ProvisionDatabase(ctx, &instant.ProvisionOpts{Name: "app-db"})
//	if err != nil { log.Fatal(err) }
//	fmt.Println("postgres://...", db.ConnectionURL)
//
// # Authentication
//
// Set INSTANT_API_KEY in your environment, or pass [WithAPIKey] to [New].
// Without a key the client operates in anonymous mode (24 h TTL).
//
// # Zero dependencies
//
// The SDK uses only the Go standard library.
package instant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the production instanode.dev API endpoint.
	//
	// Historical note: this used to be https://instant.dev, but that hostname
	// was retired during the rebrand and now serves a parking page that
	// returns 404 for every API path. SDK callers on the old default got a
	// non-actionable "404 Not Found" with no path forward. The canonical
	// production host is api.instanode.dev — fronts the same backend, valid
	// TLS, full OpenAPI surface.
	DefaultBaseURL = "https://api.instanode.dev"

	// defaultTimeout is the per-request deadline applied to read-style calls
	// (list, get, claim, delete, rotate). 30 s is plenty for a JSON round trip.
	defaultTimeout = 30 * time.Second

	// defaultProvisioningTimeout is the per-request deadline applied to the
	// synchronous provisioning + deploy endpoints.
	//
	// Provisioning is synchronous on the server: POST /db/new (and the other
	// /*/new endpoints, plus /deploy/new) blocks the handler while the real
	// Postgres / Redis / Mongo / NATS / bucket / pod is created. Under prod
	// hot-pool contention a *fresh* Postgres provision can exceed 30 s. When
	// the client gave up at the read-path 30 s the server kept working and
	// held a 60 s in-flight idempotency marker, so the caller's retry hit
	// `409 idempotency_key_in_progress` instead of succeeding. A 120 s
	// provisioning deadline comfortably outlives both the slow provision and
	// the server's in-flight window, so the first call returns the resource
	// rather than orphaning it behind a conflicting retry.
	//
	// Overridable: WithTimeout sets an explicit per-request deadline that
	// governs BOTH read and provisioning calls (see [WithTimeout]).
	defaultProvisioningTimeout = 120 * time.Second
)

// Client is the instant.dev API client.
// Construct one with [New]; all methods are safe for concurrent use.
type Client struct {
	baseURL string
	apiKey  string

	// httpClient governs read-style calls. Its Timeout is the read-path
	// per-request deadline (defaultTimeout unless WithTimeout overrides it).
	httpClient *http.Client

	// provisionClient governs the synchronous provisioning + deploy calls. It
	// shares httpClient's transport (so WithHTTPClient's transport chaining,
	// auth header, and User-Agent all apply identically) but carries no
	// client-wide Timeout cap — the per-request provisioning deadline is
	// enforced via the request context instead. This lets provisioning run for
	// up to provisionTimeout even though reads stay capped at the shorter
	// read-path Timeout.
	provisionClient *http.Client

	// provisionTimeout is the per-request deadline applied to provisioning +
	// deploy calls (defaultProvisioningTimeout unless WithTimeout overrides it).
	provisionTimeout time.Duration

	userAgent string
	logger    *slog.Logger
}

// Option configures a [Client]. Pass options to [New].
type Option func(*Client)

// WithAPIKey sets the Bearer token used for authenticated requests.
// Obtain one from the instant.dev dashboard or via the [Client.Claim] flow.
func WithAPIKey(key string) Option {
	return func(c *Client) { c.apiKey = key }
}

// WithBaseURL overrides the default API base URL (https://api.instanode.dev).
// Useful for pointing at a local development server:
//
//	client := instant.New(instant.WithBaseURL("http://localhost:8080"))
//
// The earlier `:30080` NodePort was retired 2026-05-11; the agent API now
// runs on a ClusterIP Service inside k8s and is reached locally via
// `kubectl port-forward -n instant svc/instant-api 8080:8080`.
func WithBaseURL(url string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(url, "/") }
}

// WithHTTPClient replaces the default HTTP client. Useful for custom transports
// such as distributed tracing (OpenTelemetry), custom TLS, or proxy injection.
//
// The caller's [http.Client] fields — Timeout, Transport, CheckRedirect, Jar —
// are preserved. The SDK's auth transport (which sets User-Agent and the
// Authorization header) is layered on top of the caller's Transport so the
// caller's RoundTripper still observes every request and can wrap, observe,
// or modify it before it hits the wire.
//
// If hc is nil this option is a no-op.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc == nil {
			return
		}
		c.httpClient = hc
	}
}

// WithTimeout sets the per-request HTTP timeout.
//
// It governs BOTH read-style calls and the synchronous provisioning / deploy
// calls. Without this option reads default to 30 s and provisioning defaults to
// 120 s (synchronous provisioning can exceed 30 s under prod hot-pool
// contention — see [defaultProvisioningTimeout]). Passing WithTimeout collapses
// both onto the single value you supply, so set it high enough to outlive a
// slow provision if you provision through this client:
//
//	// give every call — reads and provisioning alike — a 90 s budget
//	client := instant.New(instant.WithTimeout(90 * time.Second))
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.httpClient.Timeout = d
		c.provisionTimeout = d
	}
}

// WithLogger sets the structured logger used for advisory notices and upgrade prompts.
// Defaults to [slog.Default].
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) { c.logger = l }
}

// New creates an instanode.dev Client.
//
// Configuration is resolved in order:
//  1. Options passed to New
//  2. INSTANT_API_KEY environment variable
//  3. INSTANT_API_URL environment variable
//  4. Built-in defaults (anonymous mode, https://api.instanode.dev, 30 s timeout)
func New(opts ...Option) *Client {
	c := &Client{
		baseURL:   DefaultBaseURL,
		userAgent: userAgentString(),
		logger:    slog.Default(),
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		// 0 is the "not explicitly set" sentinel. WithTimeout overwrites it;
		// otherwise it is resolved below to either the default provisioning
		// timeout or a caller-supplied WithHTTPClient Timeout.
		provisionTimeout: 0,
	}

	// Environment defaults (explicit options below override these)
	if key := os.Getenv("INSTANT_API_KEY"); key != "" {
		c.apiKey = key
	}
	if url := os.Getenv("INSTANT_API_URL"); url != "" {
		c.baseURL = strings.TrimRight(url, "/")
	}

	for _, opt := range opts {
		opt(c)
	}

	// Resolve the provisioning deadline. Precedence:
	//  1. WithTimeout — already stamped both httpClient.Timeout and
	//     provisionTimeout to the caller's explicit value (non-zero here).
	//  2. WithHTTPClient with a non-zero Timeout — the caller set a deliberate
	//     per-request budget on their own client; honour it for provisioning
	//     too rather than silently widening it to 120 s.
	//  3. Neither — provisioning gets the 120 s default while reads keep 30 s.
	if c.provisionTimeout == 0 {
		if c.httpClient.Timeout != defaultTimeout && c.httpClient.Timeout > 0 {
			c.provisionTimeout = c.httpClient.Timeout
		} else {
			c.provisionTimeout = defaultProvisioningTimeout
		}
	}

	// Wire the auth transport on top of whatever the caller supplied. We
	// preserve every field on the caller's *http.Client (Timeout,
	// CheckRedirect, Jar, Transport) and chain the caller's Transport as the
	// inner RoundTripper so OpenTelemetry instrumentation, custom TLS, proxy
	// injection, and other RoundTripper wrappers keep working. If the caller
	// did not set a Transport, we fall through to http.DefaultTransport.
	//
	// Previous behavior — silently discarded the caller's Transport, keeping
	// only Timeout — broke every legitimate use of WithHTTPClient (B17-P0).
	base := c.httpClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	sharedTransport := &authTransport{
		base:      base,
		apiKey:    c.apiKey,
		userAgent: c.userAgent,
	}
	c.httpClient = &http.Client{
		Timeout:       c.httpClient.Timeout,
		CheckRedirect: c.httpClient.CheckRedirect,
		Jar:           c.httpClient.Jar,
		Transport:     sharedTransport,
	}

	// provisionClient shares the same (auth-wrapped) transport but carries NO
	// client-wide Timeout. Provisioning deadlines are enforced per-request via
	// the request context (see provisionContext) so a slow synchronous
	// provision can run for the full provisionTimeout instead of being killed
	// at the shorter read-path Timeout. CheckRedirect / Jar are preserved so
	// the two clients behave identically apart from the timeout cap.
	c.provisionClient = &http.Client{
		Timeout:       0,
		CheckRedirect: c.httpClient.CheckRedirect,
		Jar:           c.httpClient.Jar,
		Transport:     sharedTransport,
	}

	return c
}

// provisionContext derives a child context carrying the provisioning deadline.
//
// It never *extends* a deadline the caller already set: if ctx already has an
// earlier deadline (the caller passed context.WithTimeout themselves) that
// tighter deadline wins. It only adds the provisioning budget when the caller
// left the context open-ended. The returned cancel func must always be called.
func (c *Client) provisionContext(ctx context.Context) (context.Context, context.CancelFunc) {
	d := c.provisionTimeout
	if d <= 0 {
		d = defaultProvisioningTimeout
	}
	if existing, ok := ctx.Deadline(); ok {
		// Caller set their own deadline; only shorten ours to honour it, never
		// override a tighter caller budget.
		if remaining := time.Until(existing); remaining <= d {
			return context.WithCancel(ctx)
		}
	}
	return context.WithTimeout(ctx, d)
}

// ─── internal HTTP helpers ────────────────────────────────────────────────────

// get executes a GET request and decodes the JSON response into out.
func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

// post executes a POST request with no body and decodes the JSON response into out.
func (c *Client) post(ctx context.Context, path string, out any) error {
	return c.postJSON(ctx, path, nil, out)
}

// postJSON executes a POST request with a JSON-encoded body and decodes the response.
func (c *Client) postJSON(ctx context.Context, path string, body any, out any) error {
	return c.postJSONWithHeaders(ctx, path, body, nil, out)
}

// postJSONWithHeaders is like postJSON but also sets extra request headers.
// It runs on the read-path client (defaultTimeout). Provisioning helpers must
// use provisionJSONWithHeaders instead so the synchronous provision gets the
// longer provisioning deadline.
func (c *Client) postJSONWithHeaders(ctx context.Context, path string, body any, headers map[string]string, out any) error {
	r, err := jsonBodyReader(body)
	if err != nil {
		return err
	}
	return c.doWithHeaders(ctx, http.MethodPost, path, r, headers, out)
}

// putJSON executes a PUT request with a JSON-encoded body and decodes the
// response. It runs on the read-path client (defaultTimeout) — every PUT on
// the API surface is a quick mutation, not a synchronous provision.
func (c *Client) putJSON(ctx context.Context, path string, body any, out any) error {
	r, err := jsonBodyReader(body)
	if err != nil {
		return err
	}
	return c.doWithHeaders(ctx, http.MethodPut, path, r, nil, out)
}

// patchJSON executes a PATCH request with a JSON-encoded body and decodes the
// response. Read-path timeout class, same rationale as putJSON.
func (c *Client) patchJSON(ctx context.Context, path string, body any, out any) error {
	r, err := jsonBodyReader(body)
	if err != nil {
		return err
	}
	return c.doWithHeaders(ctx, http.MethodPatch, path, r, nil, out)
}

// provisionJSONWithHeaders POSTs a JSON body for a synchronous provisioning
// call. It routes through provisionClient (no client-wide Timeout cap) and
// applies the longer provisioning deadline via the request context, so a slow
// hot-pool provision can complete instead of timing out at the read-path 30 s
// and orphaning the resource behind a 409 idempotency_key_in_progress retry.
func (c *Client) provisionJSONWithHeaders(ctx context.Context, path string, body any, headers map[string]string, out any) error {
	r, err := jsonBodyReader(body)
	if err != nil {
		return err
	}
	pctx, cancel := c.provisionContext(ctx)
	defer cancel()
	return c.doWithClient(pctx, c.provisionClient, http.MethodPost, path, r, headers, out)
}

// jsonBodyReader marshals body to a re-readable JSON reader. A nil body yields
// a nil reader (no Content-Type is set downstream in that case).
func jsonBodyReader(body any) (io.Reader, error) {
	if body == nil {
		return nil, nil
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshalling request body: %w", err)
	}
	return bytes.NewReader(b), nil
}

// delete executes a DELETE request and decodes the JSON response into out.
func (c *Client) delete(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodDelete, path, nil, out)
}

// do executes an HTTP request. It retries once on 5xx responses.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader, out any) error {
	return c.doWithHeaders(ctx, method, path, body, nil, out)
}

// doWithHeaders is like do but allows the caller to attach extra request
// headers (e.g. Idempotency-Key). headers may be nil. It runs on the read-path
// httpClient (read-path Timeout).
func (c *Client) doWithHeaders(ctx context.Context, method, path string, body io.Reader, headers map[string]string, out any) error {
	return c.doWithClient(ctx, c.httpClient, method, path, body, headers, out)
}

// doWithClient executes an HTTP request on the supplied client, retrying once
// on a transport error or 5xx. It is the shared core behind doWithHeaders
// (read path) and provisionJSONWithHeaders (provisioning path); the only
// difference between the two is the *http.Client (hence the timeout regime)
// and the request context.
func (c *Client) doWithClient(ctx context.Context, hc *http.Client, method, path string, body io.Reader, headers map[string]string, out any) error {
	rawURL := c.baseURL + path

	// If we have a body reader we need to be able to re-read it on retry.
	// Buffer it upfront so the retry can rewind.
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = io.ReadAll(body)
		if err != nil {
			return fmt.Errorf("reading request body: %w", err)
		}
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
		if err != nil {
			return fmt.Errorf("building request: %w", err)
		}
		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range headers {
			if v != "" {
				req.Header.Set(k, v)
			}
		}

		resp, err := hc.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			if attempt == 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(300 * time.Millisecond):
				}
				continue
			}
			return lastErr
		}

		// Surface advisory headers
		c.logHeaders(resp)

		if resp.StatusCode >= 500 && attempt == 0 {
			io.Copy(io.Discard, resp.Body) //nolint:errcheck
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("server error %d", resp.StatusCode)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}

		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode >= 400 {
			raw, _ := io.ReadAll(resp.Body)
			apiErr := &APIError{StatusCode: resp.StatusCode, raw: string(raw)}
			// Try to decode structured error body
			_ = json.Unmarshal(raw, apiErr)
			return apiErr
		}

		if out != nil {
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				return fmt.Errorf("decoding response: %w", err)
			}
		}
		return nil
	}
	return lastErr
}

// logHeaders surfaces advisory response headers to the configured logger.
func (c *Client) logHeaders(resp *http.Response) {
	if notice := resp.Header.Get("X-Instant-Notice"); notice != "" {
		c.logger.Warn("instanode.dev notice", "notice", notice)
	}
	if upgradeURL := resp.Header.Get("X-Instant-Upgrade"); upgradeURL != "" {
		c.logger.Warn("instanode.dev upgrade available", "url", upgradeURL)
	}
}

// ─── auth transport ───────────────────────────────────────────────────────────

type authTransport struct {
	base      http.RoundTripper
	apiKey    string
	userAgent string
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("User-Agent", t.userAgent)
	if t.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
	}
	return t.base.RoundTrip(req)
}
