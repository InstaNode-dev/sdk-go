package instant_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"instant.dev/sdk/go/instant"
)

// TestDefaultBaseURLIsCanonical guards Wave FIX-E #C2 — DefaultBaseURL used to
// point at https://instant.dev (dead-brand 404). A package-level constant is
// the single biggest contract surface this SDK exposes; assert it's wired to
// the live host so a future "just rename one string" doesn't reintroduce the
// 404 footgun.
func TestDefaultBaseURLIsCanonical(t *testing.T) {
	if instant.DefaultBaseURL != "https://api.instanode.dev" {
		t.Errorf("DefaultBaseURL = %q; want https://api.instanode.dev (dead-brand https://instant.dev returns 404)", instant.DefaultBaseURL)
	}
}

// TestDeploy_HappyPath spins up a httptest server that mimics POST /deploy/new,
// inspects the multipart payload the SDK sent, and verifies the parsed
// Deployment matches the API contract. This is a contract test, not an
// integration test — it pins the wire shape so a future API drift surfaces as
// a unit-test failure, not a 2 am production page.
func TestDeploy_HappyPath(t *testing.T) {
	var (
		gotTarball  []byte
		gotName     string
		gotPort     string
		gotEnv      string
		gotEnvVars  string
		gotIdemKey  string
		contentType string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/deploy/new" {
			t.Errorf("path = %q; want /deploy/new", r.URL.Path)
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
		gotName = r.FormValue("name")
		gotPort = r.FormValue("port")
		gotEnv = r.FormValue("env")
		gotEnvVars = r.FormValue("env_vars")

		file, _, err := r.FormFile("tarball")
		if err != nil {
			t.Errorf("FormFile tarball: %v", err)
			http.Error(w, "no tarball", http.StatusBadRequest)
			return
		}
		defer file.Close()
		gotTarball, _ = io.ReadAll(file)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"item": map[string]any{
				"id":          "11111111-2222-3333-4444-555555555555",
				"app_id":      "6fffcc21",
				"url":         "",
				"status":      "building",
				"tier":        "hobby",
				"environment": "production",
				"env":         map[string]string{"PORT": "8080"},
				"port":        8080,
				"team_id":     "00000000-0000-0000-0000-000000000001",
			},
			"note": "deploy queued",
		})
	}))
	defer srv.Close()

	client := instant.New(instant.WithBaseURL(srv.URL))
	tar := bytes.NewBufferString("fake-tarball-contents")

	d, err := client.Deploy(context.Background(), instant.DeployOpts{
		Tarball:        tar,
		Name:           "my-api",
		Port:           8080,
		Env:            "production",
		EnvVars:        map[string]string{"PORT": "8080"},
		IdempotencyKey: "deploy-key-1",
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if d.ID != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("ID = %q", d.ID)
	}
	if d.AppID != "6fffcc21" {
		t.Errorf("AppID = %q", d.AppID)
	}
	if d.Status != "building" {
		t.Errorf("Status = %q; want building", d.Status)
	}
	if d.Environment != "production" {
		t.Errorf("Environment = %q", d.Environment)
	}
	if d.Port != 8080 {
		t.Errorf("Port = %d", d.Port)
	}

	// Wire-shape assertions.
	if !strings.HasPrefix(contentType, "multipart/form-data;") {
		t.Errorf("Content-Type = %q; want multipart/form-data;...", contentType)
	}
	if gotIdemKey != "deploy-key-1" {
		t.Errorf("Idempotency-Key = %q", gotIdemKey)
	}
	if string(gotTarball) != "fake-tarball-contents" {
		t.Errorf("tarball bytes = %q", gotTarball)
	}
	if gotName != "my-api" {
		t.Errorf("name field = %q", gotName)
	}
	if gotPort != "8080" {
		t.Errorf("port field = %q", gotPort)
	}
	if gotEnv != "production" {
		t.Errorf("env field = %q", gotEnv)
	}
	// env_vars is JSON-encoded on the wire (per the API contract).
	var envVarsParsed map[string]string
	if err := json.Unmarshal([]byte(gotEnvVars), &envVarsParsed); err != nil {
		t.Fatalf("env_vars not JSON: %v (%q)", err, gotEnvVars)
	}
	if envVarsParsed["PORT"] != "8080" {
		t.Errorf("env_vars[PORT] = %q", envVarsParsed["PORT"])
	}
}

// TestDeploy_RequiresTarball guards against nil-Reader call-sites that would
// otherwise hit the network with an empty multipart field and get a confusing
// 400 back. Fail fast in the client.
func TestDeploy_RequiresTarball(t *testing.T) {
	client := instant.New(instant.WithBaseURL("http://unused"))
	_, err := client.Deploy(context.Background(), instant.DeployOpts{})
	if err == nil {
		t.Fatal("Deploy(nil tarball) returned nil err — want a clear pre-flight error")
	}
	if !strings.Contains(err.Error(), "Tarball") {
		t.Errorf("expected error to mention Tarball; got: %v", err)
	}
}

// TestDeploy_SurfacesAPIError verifies that a 402 (tier gate) surfaces as
// *APIError with StatusCode 402 — so callers can branch on it without parsing
// the wire body. This is the property the MCP fix relies on (#C7).
func TestDeploy_SurfacesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":           false,
			"error":        "deployment_limit_reached",
			"message":      "Hobby tier allows 1 deployment",
			"agent_action": "Tell the user to upgrade at https://instanode.dev/pricing.",
			"upgrade_url":  "https://instanode.dev/pricing",
		})
	}))
	defer srv.Close()

	client := instant.New(instant.WithBaseURL(srv.URL))
	_, err := client.Deploy(context.Background(), instant.DeployOpts{
		Tarball: bytes.NewBufferString("tar"),
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

// Verify that the multipart writer wraps fields correctly when env_vars is
// omitted — we should NOT send an empty 'env_vars' part because the server
// would JSON-parse "" and reject it. (Field skipped when len(map) == 0.)
func TestDeploy_OmitsEmptyOptionalFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(10 << 20)
		if _, ok := r.MultipartForm.Value["env_vars"]; ok {
			t.Error("env_vars should be omitted when DeployOpts.EnvVars is empty")
		}
		if _, ok := r.MultipartForm.Value["name"]; ok {
			t.Error("name should be omitted when DeployOpts.Name is empty")
		}
		if _, ok := r.MultipartForm.Value["port"]; ok {
			t.Error("port should be omitted when DeployOpts.Port == 0")
		}
		if _, ok := r.MultipartForm.Value["env"]; ok {
			t.Error("env should be omitted when DeployOpts.Env is empty")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":   true,
			"item": map[string]any{"id": "abc", "app_id": "00000000", "status": "building"},
		})
	}))
	defer srv.Close()

	client := instant.New(instant.WithBaseURL(srv.URL))
	_, err := client.Deploy(context.Background(), instant.DeployOpts{
		Tarball: bytes.NewBufferString("tar"),
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
}

// Sanity probe — make sure multipart writer is reachable from this package's
// imports so we don't accidentally bury a future test dependency change.
var _ = multipart.NewWriter

// TestStreamDeploymentLogs_ParsesSSEAndWritesLines covers B17-P1-7. The SDK
// previously had no helper for /deploy/:id/logs — callers had to hand-roll
// SSE parsing. This test pins the contract: data:-prefixed lines surface as
// newline-terminated log lines on the supplied writer; comment lines (`:
// ping`) and other SSE control fields are dropped.
func TestStreamDeploymentLogs_ParsesSSEAndWritesLines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/deploy/abc12345/logs" {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Accept"); !strings.Contains(got, "text/event-stream") {
			t.Errorf("Accept = %q; want text/event-stream hint", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Mix of data lines, keepalive comments, and other SSE controls
		// the parser should ignore.
		_, _ = io.WriteString(w, ": ping\n\n")
		_, _ = io.WriteString(w, "data: line one\n\n")
		_, _ = io.WriteString(w, "event: heartbeat\n\n")
		_, _ = io.WriteString(w, "data: line two\n\n")
		_, _ = io.WriteString(w, "data:line three\n\n") // no space after colon
	}))
	defer srv.Close()

	client := instant.New(instant.WithBaseURL(srv.URL))
	var out bytes.Buffer
	if err := client.StreamDeploymentLogs(context.Background(), "abc12345", &out); err != nil {
		t.Fatalf("StreamDeploymentLogs: %v", err)
	}
	want := "line one\nline two\nline three\n"
	if out.String() != want {
		t.Errorf("output = %q; want %q", out.String(), want)
	}
}

// TestStreamDeploymentLogs_SurfacesAPIError pins the 4xx-on-stream branch:
// when the API returns 404 (deployment-not-found) before any SSE bytes, the
// helper must return an *APIError, not nil.
func TestStreamDeploymentLogs_SurfacesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"not_found","message":"Deployment not found"}`)
	}))
	defer srv.Close()

	client := instant.New(instant.WithBaseURL(srv.URL))
	var out bytes.Buffer
	err := client.StreamDeploymentLogs(context.Background(), "missing0", &out)
	if err == nil {
		t.Fatalf("expected APIError, got nil")
	}
	var apiErr *instant.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("StatusCode = %d; want 404", apiErr.StatusCode)
	}
}

// TestStreamDeploymentLogs_RejectsEmptyAppID guards the precondition — the
// helper must not even attempt a network call when appID is empty (would
// otherwise hit /deploy//logs which fasthttp dislikes).
func TestStreamDeploymentLogs_RejectsEmptyAppID(t *testing.T) {
	client := instant.New(instant.WithBaseURL("http://example.invalid"))
	if err := client.StreamDeploymentLogs(context.Background(), "", &bytes.Buffer{}); err == nil {
		t.Fatal("expected validation error for empty appID")
	}
}
