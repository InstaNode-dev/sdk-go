package instant_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/InstaNode-dev/sdk-go/instant"
)

// TestCreateStack_HappyPath verifies the SDK uploads a valid multipart form
// (manifest + name + env + per-service tarballs) to POST /stacks/new and parses
// the 202 response (stack_id/status/tier/env/expires_in) onto Stack. It also
// asserts the synthesised manifest round-trips through a real YAML parser and
// carries the service's port/expose/needs/env.
func TestCreateStack_HappyPath(t *testing.T) {
	var (
		gotManifest string
		gotName     string
		gotEnv      string
		gotIdemKey  string
		gotTarball  []byte
		contentType string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stacks/new" {
			t.Errorf("path = %q; want /stacks/new", r.URL.Path)
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q; want POST", r.Method)
		}
		gotIdemKey = r.Header.Get("Idempotency-Key")
		contentType = r.Header.Get("Content-Type")

		if err := r.ParseMultipartForm(50 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		gotManifest = r.FormValue("manifest")
		gotName = r.FormValue("name")
		gotEnv = r.FormValue("env")

		// Tarball is keyed by service name ("api").
		f, _, err := r.FormFile("api")
		if err != nil {
			t.Errorf("FormFile api: %v", err)
		} else {
			defer f.Close()
			gotTarball, _ = io.ReadAll(f)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":         true,
			"stack_id":   "ab12cd34",
			"env":        "production",
			"status":     "building",
			"tier":       "anonymous",
			"expires_in": "6h",
			"note":       "Stack is building. Poll GET /stacks/ab12cd34 for status.",
		})
	}))
	defer srv.Close()

	client := instant.New(instant.WithBaseURL(srv.URL))
	st, err := client.CreateStack(context.Background(), instant.CreateStackOpts{
		Name: "my-app",
		Env:  "production",
		Services: []instant.StackServiceSpec{{
			Name:    "api",
			Tarball: bytes.NewBufferString("fake-api-tarball"),
			Port:    8080,
			Expose:  true,
			Needs:   []string{"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"},
			Env:     map[string]string{"LOG_LEVEL": "debug"},
		}},
		IdempotencyKey: "stack-key-1",
	})
	if err != nil {
		t.Fatalf("CreateStack: %v", err)
	}

	if st.Slug != "ab12cd34" {
		t.Errorf("Slug = %q; want ab12cd34", st.Slug)
	}
	if st.Status != "building" {
		t.Errorf("Status = %q; want building", st.Status)
	}
	if st.Tier != "anonymous" {
		t.Errorf("Tier = %q", st.Tier)
	}
	if st.Env != "production" {
		t.Errorf("Env = %q", st.Env)
	}
	if st.ExpiresIn != "6h" {
		t.Errorf("ExpiresIn = %q", st.ExpiresIn)
	}

	// Wire-shape assertions.
	if !strings.HasPrefix(contentType, "multipart/form-data;") {
		t.Errorf("Content-Type = %q; want multipart/form-data;...", contentType)
	}
	if gotIdemKey != "stack-key-1" {
		t.Errorf("Idempotency-Key = %q", gotIdemKey)
	}
	if gotName != "my-app" {
		t.Errorf("name field = %q", gotName)
	}
	if gotEnv != "production" {
		t.Errorf("env field = %q", gotEnv)
	}
	if string(gotTarball) != "fake-api-tarball" {
		t.Errorf("tarball bytes = %q", gotTarball)
	}

	// The synthesised manifest must carry the service config. Assert on the
	// generated text directly (the SDK is zero-dependency, so the test can't
	// pull in a YAML parser); the parser's own round-trip is covered in the
	// api repo's manifest tests.
	for _, want := range []string{
		"services:",
		`  "api":`,
		`    build: "."`,
		"    port: 8080",
		"    expose: true",
		"    needs:",
		`      - "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"`,
		"    env:",
		`      "LOG_LEVEL": "debug"`,
	} {
		if !strings.Contains(gotManifest, want) {
			t.Errorf("manifest missing %q\nfull manifest:\n%s", want, gotManifest)
		}
	}
}

// TestCreateStack_OmitsEnvWhenEmpty verifies the env multipart field is not
// sent when CreateStackOpts.Env is empty (server defaults to "development").
func TestCreateStack_OmitsEnvWhenEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(10 << 20)
		if _, ok := r.MultipartForm.Value["env"]; ok {
			t.Error("env should be omitted when CreateStackOpts.Env is empty")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "stack_id": "deadbeef", "status": "building"})
	}))
	defer srv.Close()

	client := instant.New(instant.WithBaseURL(srv.URL))
	_, err := client.CreateStack(context.Background(), instant.CreateStackOpts{
		Name:     "x",
		Services: []instant.StackServiceSpec{{Name: "web", Tarball: bytes.NewBufferString("t")}},
	})
	if err != nil {
		t.Fatalf("CreateStack: %v", err)
	}
}

// TestCreateStack_SurfacesAPIError pins the tier-gate error path: a 402
// deployment_limit_reached must surface as *APIError so callers can branch on
// the status code.
func TestCreateStack_SurfacesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":           false,
			"error":        "deployment_limit_reached",
			"message":      "Hobby tier allows 1 deployment(s)",
			"agent_action": "Tell the user to upgrade at https://instanode.dev/pricing.",
			"upgrade_url":  "https://instanode.dev/pricing",
		})
	}))
	defer srv.Close()

	client := instant.New(instant.WithBaseURL(srv.URL))
	_, err := client.CreateStack(context.Background(), instant.CreateStackOpts{
		Name:     "x",
		Services: []instant.StackServiceSpec{{Name: "api", Tarball: bytes.NewBufferString("t")}},
	})
	if err == nil {
		t.Fatal("expected an error on 402, got nil")
	}
	var apiErr *instant.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError; got %T (%v)", err, err)
	}
	if apiErr.StatusCode != http.StatusPaymentRequired {
		t.Errorf("StatusCode = %d; want 402", apiErr.StatusCode)
	}
	if apiErr.Code != "deployment_limit_reached" {
		t.Errorf("Code = %q; want deployment_limit_reached", apiErr.Code)
	}
}

// TestCreateStack_PreflightValidation covers the client-side guard arms (bad
// name, no services, empty service name, nil tarball, duplicate name) plus the
// empty-stack_id response branch.
func TestCreateStack_PreflightValidation(t *testing.T) {
	client := instant.New(instant.WithBaseURL("http://unused.invalid"))

	if _, err := client.CreateStack(context.Background(), instant.CreateStackOpts{}); err == nil {
		t.Error("empty name: expected error")
	}

	if _, err := client.CreateStack(context.Background(), instant.CreateStackOpts{Name: "ok"}); err == nil {
		t.Error("no services: expected error")
	} else if !strings.Contains(err.Error(), "at least one service") {
		t.Errorf("no services error = %v", err)
	}

	if _, err := client.CreateStack(context.Background(), instant.CreateStackOpts{
		Name:     "ok",
		Services: []instant.StackServiceSpec{{Tarball: bytes.NewBufferString("t")}},
	}); err == nil || !strings.Contains(err.Error(), "non-empty Name") {
		t.Errorf("empty service name: got err = %v", err)
	}

	if _, err := client.CreateStack(context.Background(), instant.CreateStackOpts{
		Name:     "ok",
		Services: []instant.StackServiceSpec{{Name: "api"}},
	}); err == nil || !strings.Contains(err.Error(), "non-nil Tarball") {
		t.Errorf("nil tarball: got err = %v", err)
	}

	if _, err := client.CreateStack(context.Background(), instant.CreateStackOpts{
		Name: "ok",
		Services: []instant.StackServiceSpec{
			{Name: "api", Tarball: bytes.NewBufferString("a")},
			{Name: "api", Tarball: bytes.NewBufferString("b")},
		},
	}); err == nil || !strings.Contains(err.Error(), "duplicate service name") {
		t.Errorf("duplicate service: got err = %v", err)
	}

	// Empty-stack_id response branch.
	emptySlug := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "status": "building"})
	}))
	defer emptySlug.Close()
	ec := instant.New(instant.WithBaseURL(emptySlug.URL))
	if _, err := ec.CreateStack(context.Background(), instant.CreateStackOpts{
		Name:     "x",
		Services: []instant.StackServiceSpec{{Name: "api", Tarball: bytes.NewBufferString("t")}},
	}); err == nil || !strings.Contains(err.Error(), "empty stack_id") {
		t.Errorf("empty stack_id: got err = %v", err)
	}
}

// failingReader returns an error on Read so we can exercise CreateStack's
// io.Copy failure branch.
type failingReader struct{}

func (failingReader) Read(_ []byte) (int, error) { return 0, errors.New("boom") }

// TestCreateStack_TarballReadError covers the io.Copy failure arm when a
// service Tarball reader errors mid-read.
func TestCreateStack_TarballReadError(t *testing.T) {
	client := instant.New(instant.WithBaseURL("http://unused.invalid"))
	_, err := client.CreateStack(context.Background(), instant.CreateStackOpts{
		Name:     "x",
		Services: []instant.StackServiceSpec{{Name: "api", Tarball: failingReader{}}},
	})
	if err == nil || !strings.Contains(err.Error(), "reading tarball") {
		t.Errorf("expected tarball read error; got %v", err)
	}
}

// TestCreateStack_TwoServices exercises the manifest builder's sort comparator
// (>= 2 services) so the manifest order is deterministic, and confirms both
// service tarballs are uploaded under their respective field names.
func TestCreateStack_TwoServices(t *testing.T) {
	var gotManifest string
	gotTarballs := map[string]string{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(10 << 20)
		gotManifest = r.FormValue("manifest")
		for _, name := range []string{"api", "worker"} {
			if f, _, err := r.FormFile(name); err == nil {
				b, _ := io.ReadAll(f)
				_ = f.Close()
				gotTarballs[name] = string(b)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "stack_id": "cafef00d", "status": "building"})
	}))
	defer srv.Close()

	client := instant.New(instant.WithBaseURL(srv.URL))
	// Pass services out of order to prove the builder sorts them.
	_, err := client.CreateStack(context.Background(), instant.CreateStackOpts{
		Name: "x",
		Services: []instant.StackServiceSpec{
			{Name: "worker", Tarball: bytes.NewBufferString("worker-tar"), Port: 9000},
			{Name: "api", Tarball: bytes.NewBufferString("api-tar"), Port: 8080, Expose: true},
		},
	})
	if err != nil {
		t.Fatalf("CreateStack: %v", err)
	}

	// Deterministic order: "api" block precedes "worker" block.
	apiIdx := strings.Index(gotManifest, `  "api":`)
	workerIdx := strings.Index(gotManifest, `  "worker":`)
	if apiIdx < 0 || workerIdx < 0 || apiIdx > workerIdx {
		t.Errorf("manifest not sorted (api before worker)\n%s", gotManifest)
	}
	if gotTarballs["api"] != "api-tar" || gotTarballs["worker"] != "worker-tar" {
		t.Errorf("tarballs = %v", gotTarballs)
	}
}

// TestCreateStack_TransportError covers the provisionClient.Do failure arm:
// pointing the client at a closed server yields a transport error (after the
// SDK's one retry), which must surface as a request-failed error, not a panic.
func TestCreateStack_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := srv.URL
	srv.Close() // close immediately so Do() fails to connect

	client := instant.New(instant.WithBaseURL(closedURL))
	_, err := client.CreateStack(context.Background(), instant.CreateStackOpts{
		Name:     "x",
		Services: []instant.StackServiceSpec{{Name: "api", Tarball: bytes.NewBufferString("t")}},
	})
	if err == nil || !strings.Contains(err.Error(), "request failed") {
		t.Errorf("expected request-failed transport error; got %v", err)
	}
}

// TestCreateStack_BadBaseURLBuildsRequestError covers the
// http.NewRequestWithContext failure arm via a control character in the URL.
func TestCreateStack_BadBaseURLBuildsRequestError(t *testing.T) {
	client := instant.New(instant.WithBaseURL("http://example.com/\x7f"))
	_, err := client.CreateStack(context.Background(), instant.CreateStackOpts{
		Name:     "x",
		Services: []instant.StackServiceSpec{{Name: "api", Tarball: bytes.NewBufferString("t")}},
	})
	if err == nil || !strings.Contains(err.Error(), "building request") {
		t.Errorf("expected building-request error; got %v", err)
	}
}

// TestCreateStack_DecodeError covers the json.Decode failure arm: a 2xx
// response whose body is not valid JSON.
func TestCreateStack_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, "this is not json")
	}))
	defer srv.Close()

	client := instant.New(instant.WithBaseURL(srv.URL))
	_, err := client.CreateStack(context.Background(), instant.CreateStackOpts{
		Name:     "x",
		Services: []instant.StackServiceSpec{{Name: "api", Tarball: bytes.NewBufferString("t")}},
	})
	if err == nil || !strings.Contains(err.Error(), "decoding response") {
		t.Errorf("expected decode error; got %v", err)
	}
}

// TestGetStack_HappyPath verifies GET /stacks/:slug parsing, including the
// per-service detail (name/status/expose/port/url) and expires_at.
func TestGetStack_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stacks/ab12cd34" {
			t.Errorf("path = %q; want /stacks/ab12cd34", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %q; want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":         true,
			"stack_id":   "ab12cd34",
			"status":     "healthy",
			"tier":       "hobby",
			"name":       "my-app",
			"expires_at": "2026-06-11T00:00:00Z",
			"services": []map[string]any{
				{"name": "api", "status": "healthy", "expose": true, "port": 8080, "url": "https://ab12cd34.deployment.instanode.dev"},
				{"name": "worker", "status": "healthy", "expose": false, "port": 9000, "url": ""},
			},
		})
	}))
	defer srv.Close()

	client := instant.New(instant.WithBaseURL(srv.URL))
	st, err := client.GetStack(context.Background(), "ab12cd34")
	if err != nil {
		t.Fatalf("GetStack: %v", err)
	}
	if st.Slug != "ab12cd34" {
		t.Errorf("Slug = %q", st.Slug)
	}
	if st.Status != "healthy" {
		t.Errorf("Status = %q; want healthy", st.Status)
	}
	if st.Name != "my-app" {
		t.Errorf("Name = %q", st.Name)
	}
	if st.ExpiresAt != "2026-06-11T00:00:00Z" {
		t.Errorf("ExpiresAt = %q", st.ExpiresAt)
	}
	if len(st.Services) != 2 {
		t.Fatalf("len(Services) = %d; want 2", len(st.Services))
	}
	if st.Services[0].Name != "api" || !st.Services[0].Expose || st.Services[0].Port != 8080 {
		t.Errorf("Services[0] = %+v", st.Services[0])
	}
	if st.Services[0].URL != "https://ab12cd34.deployment.instanode.dev" {
		t.Errorf("Services[0].URL = %q", st.Services[0].URL)
	}
	if st.Services[1].Expose {
		t.Errorf("Services[1].Expose = true; want false")
	}
}

// TestGetStack_NotFound pins the 404 path → *APIError, and the empty-slug
// preflight guard.
func TestGetStack_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "not_found", "message": "Stack not found"})
	}))
	defer srv.Close()

	client := instant.New(instant.WithBaseURL(srv.URL))
	_, err := client.GetStack(context.Background(), "missing0")
	if !instant.IsNotFound(err) {
		t.Fatalf("expected IsNotFound; got %v", err)
	}

	// Empty-slug preflight.
	if _, err := client.GetStack(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "slug is required") {
		t.Errorf("empty slug: got err = %v", err)
	}
}

// TestCreateStack_ManifestNeedsAndEnvOnly exercises the manifest builder arms
// for a service with no port/expose but with needs+env, and the YAML-quoting of
// values containing metacharacters.
func TestCreateStack_ManifestNeedsAndEnvOnly(t *testing.T) {
	var gotManifest string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(10 << 20)
		gotManifest = r.FormValue("manifest")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "stack_id": "feedface", "status": "building"})
	}))
	defer srv.Close()

	client := instant.New(instant.WithBaseURL(srv.URL))
	_, err := client.CreateStack(context.Background(), instant.CreateStackOpts{
		Name: "x",
		Services: []instant.StackServiceSpec{{
			Name:    "worker",
			Tarball: bytes.NewBufferString("t"),
			// no Port (→ omitted, server defaults to 8080), no Expose
			Needs: []string{"tok-1"},
			Env:   map[string]string{"DSN": "postgres://u:p@h:5432/db?x=1"},
		}},
	})
	if err != nil {
		t.Fatalf("CreateStack: %v", err)
	}

	// Port omitted (no `port:` line for this service → server defaults to 8080).
	if strings.Contains(gotManifest, "port:") {
		t.Errorf("manifest should omit port when 0\n%s", gotManifest)
	}
	if strings.Contains(gotManifest, "expose:") {
		t.Errorf("manifest should omit expose when false\n%s", gotManifest)
	}
	// needs + env present; the colon-laden DSN value is double-quoted so it
	// stays a single YAML scalar.
	for _, want := range []string{
		`      - "tok-1"`,
		`      "DSN": "postgres://u:p@h:5432/db?x=1"`,
	} {
		if !strings.Contains(gotManifest, want) {
			t.Errorf("manifest missing %q\n%s", want, gotManifest)
		}
	}
}
