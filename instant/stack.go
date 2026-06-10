package instant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sort"
	"strings"
)

// StackServiceSpec declares one service in a multi-service stack create.
//
// Each service ships a gzipped build context (Dockerfile + source) the API
// hands to Kaniko. The service Name keys both the manifest entry and the
// multipart tarball field — the API looks the tarball up by service name.
type StackServiceSpec struct {
	// Name is the service identifier. REQUIRED, must be unique within the
	// stack, and is used as the multipart field name for this service's
	// tarball. The API rejects an empty service name with a 400.
	Name string

	// Tarball is the gzipped tar archive (Dockerfile + source) for this
	// service. REQUIRED. The aggregate of every service tarball in one
	// CreateStack call must stay under the API's 50 MiB multipart cap.
	Tarball io.Reader

	// Port is the container port this service listens on (manifest `port`).
	// 0 leaves it off the manifest and the API defaults it to 8080.
	Port int

	// Expose, when true, fronts the service with an Ingress + TLS so it gets a
	// public URL (manifest `expose`). Internal-only services leave it false.
	Expose bool

	// Needs lists resource tokens (from ProvisionDatabase / Cache / …) whose
	// connection URLs are injected into this service's environment as
	// DATABASE_URL / REDIS_URL / … (manifest `needs`). Anonymous stacks may
	// only reference anonymous resources.
	Needs []string

	// Env are extra environment variables for this service (manifest `env`).
	// Keys must be POSIX env-var names; values may use the "vault://KEY" form
	// on authenticated stacks. The "service://other" form resolves to the
	// in-cluster URL of a sibling service.
	Env map[string]string
}

// CreateStackOpts are the parameters for [Client.CreateStack].
type CreateStackOpts struct {
	// Name is the human-readable stack label. REQUIRED (1–64 chars,
	// ^[A-Za-z0-9][A-Za-z0-9 _-]*$) — validated client-side before any
	// network call, mirroring the provisioning endpoints.
	Name string

	// Services are the services to deploy. At least one is REQUIRED.
	Services []StackServiceSpec

	// Env is the environment scope (production / staging / development).
	// Empty resolves to "development" server-side (migration 026).
	Env string

	// IdempotencyKey is an optional replay guard forwarded as the
	// Idempotency-Key header. First response is cached for 24h; replays return
	// the cached body with X-Idempotent-Replay: true.
	IdempotencyKey string
}

// Stack is the response shape from [Client.CreateStack] and [Client.GetStack].
//
// The two endpoints return slightly different field sets:
//   - POST /stacks/new returns Slug, Env, Status, Tier, ExpiresIn, Note.
//   - GET  /stacks/:slug returns Slug, Status, Tier, Name, Services, ExpiresAt.
//
// All fields are decoded onto this one struct; a field absent from a given
// response stays at its zero value.
type Stack struct {
	// OK is always true on success.
	OK bool `json:"ok"`

	// Slug is the public stack identifier (the API's `stack_id`). It is the
	// secret used to fetch / poll an anonymous stack, and the path segment for
	// GetStack.
	Slug string `json:"stack_id"`

	// Status is the lifecycle state: "building" (create accepted, build pods
	// launching), "healthy" (all services up), "failed", "deleting", or
	// "stopped". Poll GetStack until it leaves "building".
	Status string `json:"status"`

	// Tier is the plan tier the stack was created under ("anonymous" for an
	// unauthenticated create).
	Tier string `json:"tier"`

	// Name is the human-readable label (populated by GetStack).
	Name string `json:"name,omitempty"`

	// Env is the resolved environment scope (populated by CreateStack).
	Env string `json:"env,omitempty"`

	// Services lists the per-service status + URL (populated by GetStack).
	Services []StackService `json:"services,omitempty"`

	// ExpiresIn is the human TTL string for an anonymous stack ("6h"),
	// populated by CreateStack. Empty for authenticated stacks.
	ExpiresIn string `json:"expires_in,omitempty"`

	// ExpiresAt is the RFC3339 expiry timestamp, populated by GetStack for
	// stacks that have one. Empty for permanent (paid-tier) stacks.
	ExpiresAt string `json:"expires_at,omitempty"`

	// Note is an advisory / upgrade message from the server (populated by
	// CreateStack).
	Note string `json:"note,omitempty"`
}

// StackService is one service within a [Stack] as returned by GetStack.
type StackService struct {
	// Name is the service identifier from the manifest.
	Name string `json:"name"`

	// Status is the per-service lifecycle state.
	Status string `json:"status"`

	// Expose reports whether the service has a public Ingress.
	Expose bool `json:"expose"`

	// Port is the container port the service listens on.
	Port int `json:"port"`

	// URL is the public HTTPS endpoint for an exposed service once healthy.
	// Empty for internal services or before the service is serving.
	URL string `json:"url"`
}

// CreateStack deploys a multi-service stack via POST /stacks/new.
//
// This is the documented ANONYMOUS deploy path: /deploy/new requires
// authentication, but /stacks/new accepts anonymous callers (a single-service
// stack is the way an unauthenticated agent ships an app). The SDK synthesises
// an instant.yaml manifest from the supplied services and uploads it alongside
// each service's tarball as multipart/form-data.
//
// The call is accepted asynchronously: the returned Stack reflects the
// build-accepted state (Status="building"). Poll [Client.GetStack] with the
// returned Slug until Status leaves "building".
//
// CreateStack returns an *APIError on 4xx/5xx — e.g. 402 deployment_limit_reached
// (tier gate) or 429 rate_limit_exceeded (anonymous deploy cap) — so callers can
// branch on the status code without parsing strings.
//
// Example:
//
//	f, _ := os.Open("api.tar.gz")
//	defer f.Close()
//	st, err := client.CreateStack(ctx, instant.CreateStackOpts{
//	    Name: "my-app",
//	    Env:  "production",
//	    Services: []instant.StackServiceSpec{{
//	        Name:    "api",
//	        Tarball: f,
//	        Port:    8080,
//	        Expose:  true,
//	    }},
//	})
//	if err != nil { log.Fatal(err) }
//	fmt.Println("stack:", st.Slug, "status:", st.Status)
func (c *Client) CreateStack(ctx context.Context, opts CreateStackOpts) (*Stack, error) {
	if err := validateResourceName(opts.Name); err != nil {
		return nil, fmt.Errorf("CreateStack: %w", err)
	}
	if len(opts.Services) == 0 {
		return nil, fmt.Errorf("CreateStack: at least one service is required")
	}
	seen := make(map[string]struct{}, len(opts.Services))
	for i, svc := range opts.Services {
		if svc.Name == "" {
			return nil, fmt.Errorf("CreateStack: services[%d] requires a non-empty Name", i)
		}
		if svc.Tarball == nil {
			return nil, fmt.Errorf("CreateStack: service %q requires a non-nil Tarball reader", svc.Name)
		}
		if _, dup := seen[svc.Name]; dup {
			return nil, fmt.Errorf("CreateStack: duplicate service name %q", svc.Name)
		}
		seen[svc.Name] = struct{}{}
	}

	var buf bytes.Buffer
	contentType, err := writeStackMultipart(&buf, opts)
	if err != nil {
		return nil, err
	}

	// Stacks build pods — a synchronous accept under hot-pool load can be slow,
	// so route through the provisioning client (no 30 s read-path cap) plus the
	// longer provisioning deadline, exactly like Deploy and the /*/new helpers.
	pctx, cancel := c.provisionContext(ctx)
	defer cancel()

	url := c.baseURL + "/stacks/new"
	req, err := http.NewRequestWithContext(pctx, http.MethodPost, url, &buf)
	if err != nil {
		return nil, fmt.Errorf("CreateStack: building request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	if opts.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", opts.IdempotencyKey)
	}

	resp, err := c.provisionClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("CreateStack: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	c.logHeaders(resp)

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		apiErr := &APIError{StatusCode: resp.StatusCode, raw: string(raw)}
		_ = json.Unmarshal(raw, apiErr)
		return nil, apiErr
	}

	var out Stack
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("CreateStack: decoding response: %w", err)
	}
	if out.Slug == "" {
		return nil, fmt.Errorf("CreateStack: server returned empty stack_id")
	}
	if out.Note != "" {
		c.logger.Info("instant.dev stack created",
			"slug", out.Slug,
			"tier", out.Tier,
			"status", out.Status,
			"note", out.Note,
		)
	}
	return &out, nil
}

// GetStack fetches a stack's status + per-service detail via
// GET /stacks/:slug. Use it to poll a stack returned by [Client.CreateStack]
// until its Status leaves "building".
//
// An anonymous caller may fetch an anonymous stack by its slug (the slug is the
// secret); an authenticated caller may only fetch stacks owned by its team.
// A missing or other-team stack returns an *APIError with StatusCode 404 —
// branch on [IsNotFound].
//
// Example:
//
//	st, err := client.GetStack(ctx, slug)
//	if instant.IsNotFound(err) { fmt.Println("stack gone") }
//	for _, svc := range st.Services {
//	    fmt.Printf("%s  %s  %s\n", svc.Name, svc.Status, svc.URL)
//	}
func (c *Client) GetStack(ctx context.Context, slug string) (*Stack, error) {
	if slug == "" {
		return nil, fmt.Errorf("GetStack: slug is required")
	}
	var out Stack
	if err := c.get(ctx, "/stacks/"+slug, &out); err != nil {
		return nil, fmt.Errorf("GetStack: %w", err)
	}
	return &out, nil
}

// writeStackMultipart writes the /stacks/new multipart body (manifest + name +
// optional env + one tarball part per service) to w and returns the
// Content-Type header value carrying the boundary. Taking an io.Writer (rather
// than building into a concrete *bytes.Buffer inline) keeps the multipart
// error arms reachable: a writer that fails mid-stream exercises the
// WriteField / CreateFormFile / Close failure paths a *bytes.Buffer can never
// reach. opts is assumed already validated by the CreateStack pre-flight.
func writeStackMultipart(w io.Writer, opts CreateStackOpts) (string, error) {
	mw := multipart.NewWriter(w)

	if err := mw.WriteField("manifest", buildStackManifest(opts.Services)); err != nil {
		return "", fmt.Errorf("CreateStack: writing manifest field: %w", err)
	}
	if err := mw.WriteField("name", opts.Name); err != nil {
		return "", fmt.Errorf("CreateStack: writing name field: %w", err)
	}
	if opts.Env != "" {
		if err := mw.WriteField("env", opts.Env); err != nil {
			return "", fmt.Errorf("CreateStack: writing env field: %w", err)
		}
	}

	// One tarball part per service, keyed by service name — the API looks the
	// tarball up by the service name (form.File[name]).
	for _, svc := range opts.Services {
		part, err := mw.CreateFormFile(svc.Name, svc.Name+".tar.gz")
		if err != nil {
			return "", fmt.Errorf("CreateStack: building tarball field for %q: %w", svc.Name, err)
		}
		if _, err := io.Copy(part, svc.Tarball); err != nil {
			return "", fmt.Errorf("CreateStack: reading tarball for %q: %w", svc.Name, err)
		}
	}

	if err := mw.Close(); err != nil {
		return "", fmt.Errorf("CreateStack: closing multipart writer: %w", err)
	}
	return mw.FormDataContentType(), nil
}

// buildStackManifest renders the instant.yaml the API expects from the supplied
// service specs. Services are emitted in name order so the manifest is
// deterministic (stable test output, reproducible idempotency). YAML scalars
// are quoted so a value containing a colon or other YAML metacharacter can't
// corrupt the document — the manifest is machine-generated from user input.
func buildStackManifest(services []StackServiceSpec) string {
	ordered := make([]StackServiceSpec, len(services))
	copy(ordered, services)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })

	var b strings.Builder
	b.WriteString("services:\n")
	for _, svc := range ordered {
		fmt.Fprintf(&b, "  %s:\n", yamlScalar(svc.Name))
		// build points at the in-tarball context root; the API keys the
		// tarball by service name, so "." (the tarball root) is correct.
		b.WriteString("    build: \".\"\n")
		if svc.Port > 0 {
			fmt.Fprintf(&b, "    port: %d\n", svc.Port)
		}
		if svc.Expose {
			b.WriteString("    expose: true\n")
		}
		if len(svc.Needs) > 0 {
			b.WriteString("    needs:\n")
			for _, n := range svc.Needs {
				fmt.Fprintf(&b, "      - %s\n", yamlScalar(n))
			}
		}
		if len(svc.Env) > 0 {
			b.WriteString("    env:\n")
			keys := make([]string, 0, len(svc.Env))
			for k := range svc.Env {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(&b, "      %s: %s\n", yamlScalar(k), yamlScalar(svc.Env[k]))
			}
		}
	}
	return b.String()
}

// yamlScalar double-quotes a string for safe embedding as a YAML scalar,
// escaping backslashes and double-quotes. Keeps a generated manifest valid even
// when a service name, env key, or value contains YAML-significant characters.
func yamlScalar(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
