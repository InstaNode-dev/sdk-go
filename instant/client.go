// Package instant is the official Go SDK for instant.dev.
//
// instant.dev provisions real developer infrastructure — databases, caches,
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
	// DefaultBaseURL is the production instant.dev API endpoint.
	DefaultBaseURL = "https://instant.dev"

	// defaultTimeout is applied to every HTTP request.
	defaultTimeout = 30 * time.Second

	// sdkVersion is sent in the User-Agent header.
	sdkVersion = "instant-go-sdk/0.2"
)

// Client is the instant.dev API client.
// Construct one with [New]; all methods are safe for concurrent use.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	userAgent  string
	logger     *slog.Logger
}

// Option configures a [Client]. Pass options to [New].
type Option func(*Client)

// WithAPIKey sets the Bearer token used for authenticated requests.
// Obtain one from the instant.dev dashboard or via the [Client.Claim] flow.
func WithAPIKey(key string) Option {
	return func(c *Client) { c.apiKey = key }
}

// WithBaseURL overrides the default API base URL (https://instant.dev).
// Useful for pointing at a local development server:
//
//	client := instant.New(instant.WithBaseURL("http://localhost:30080"))
func WithBaseURL(url string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(url, "/") }
}

// WithHTTPClient replaces the default HTTP client. Useful for custom transports
// such as distributed tracing or custom TLS configuration.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithTimeout sets the per-request HTTP timeout. Default is 30 s.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.httpClient.Timeout = d }
}

// WithLogger sets the structured logger used for advisory notices and upgrade prompts.
// Defaults to [slog.Default].
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) { c.logger = l }
}

// New creates an instant.dev Client.
//
// Configuration is resolved in order:
//  1. Options passed to New
//  2. INSTANT_API_KEY environment variable
//  3. INSTANT_API_URL environment variable
//  4. Built-in defaults (anonymous mode, https://instant.dev, 30 s timeout)
func New(opts ...Option) *Client {
	c := &Client{
		baseURL:   DefaultBaseURL,
		userAgent: sdkVersion,
		logger:    slog.Default(),
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
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

	// Wire auth transport
	c.httpClient = &http.Client{
		Timeout: c.httpClient.Timeout,
		Transport: &authTransport{
			base:      http.DefaultTransport,
			apiKey:    c.apiKey,
			userAgent: c.userAgent,
		},
	}

	return c
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
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshalling request body: %w", err)
		}
		r = bytes.NewReader(b)
	}
	return c.do(ctx, http.MethodPost, path, r, out)
}

// delete executes a DELETE request and decodes the JSON response into out.
func (c *Client) delete(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodDelete, path, nil, out)
}

// do executes an HTTP request. It retries once on 5xx responses.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader, out any) error {
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

		resp, err := c.httpClient.Do(req)
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
			resp.Body.Close()
			lastErr = fmt.Errorf("server error %d", resp.StatusCode)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}

		defer resp.Body.Close()

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
		c.logger.Warn("instant.dev notice", "notice", notice)
	}
	if upgradeURL := resp.Header.Get("X-Instant-Upgrade"); upgradeURL != "" {
		c.logger.Warn("instant.dev upgrade available", "url", upgradeURL)
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
