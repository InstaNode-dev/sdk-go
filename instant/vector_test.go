package instant_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/InstaNode-dev/sdk-go/instant"
)

// TestProvisionVector_HappyPath spins up a httptest server mimicking
// POST /vector/new, asserts the SDK sent name + dimensions in the JSON body,
// and verifies the parsed VectorResult (including the pgvector-specific
// Extension + Dimensions fields) matches the API contract.
func TestProvisionVector_HappyPath(t *testing.T) {
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vector/new" {
			t.Errorf("path = %q; want /vector/new", r.URL.Path)
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q; want POST", r.Method)
		}
		if got := r.Header.Get("Idempotency-Key"); got != "vec-key-1" {
			t.Errorf("Idempotency-Key = %q; want vec-key-1", got)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":             true,
			"id":             "11111111-2222-3333-4444-555555555555",
			"token":          "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			"name":           "embeddings",
			"connection_url": "postgres://usr_x:pw@host:5432/db_x",
			"tier":           "anonymous",
			"env":            "development",
			"extension":      "pgvector",
			"dimensions":     768,
			"limits":         map[string]any{"storage_mb": 10, "connections": 2, "expires_in": "24h"},
			"note":           "Claim at https://instanode.dev/start?t=...",
		})
	}))
	defer srv.Close()

	client := instant.New(instant.WithBaseURL(srv.URL))
	res, err := client.ProvisionVector(context.Background(), &instant.VectorOpts{
		ProvisionOpts: instant.ProvisionOpts{Name: "embeddings", IdempotencyKey: "vec-key-1"},
		Dimensions:    768,
	})
	if err != nil {
		t.Fatalf("ProvisionVector: %v", err)
	}

	if res.Token != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Errorf("Token = %q", res.Token)
	}
	if res.ConnectionURL != "postgres://usr_x:pw@host:5432/db_x" {
		t.Errorf("ConnectionURL = %q", res.ConnectionURL)
	}
	if res.Extension != "pgvector" {
		t.Errorf("Extension = %q; want pgvector", res.Extension)
	}
	if res.Dimensions != 768 {
		t.Errorf("Dimensions = %d; want 768", res.Dimensions)
	}
	if res.Tier != "anonymous" {
		t.Errorf("Tier = %q", res.Tier)
	}

	// Wire-body assertions.
	if gotBody["name"] != "embeddings" {
		t.Errorf("body name = %v; want embeddings", gotBody["name"])
	}
	// JSON numbers decode as float64.
	if gotBody["dimensions"] != float64(768) {
		t.Errorf("body dimensions = %v; want 768", gotBody["dimensions"])
	}
}

// TestProvisionVector_OmitsDimensionsWhenZero verifies the SDK leaves the
// dimensions field off the wire when the caller passes 0, so the server
// applies its own default (1536) instead of receiving an explicit 0 (which it
// would reject as out-of-range).
func TestProvisionVector_OmitsDimensionsWhenZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, present := body["dimensions"]; present {
			t.Errorf("dimensions should be omitted when Dimensions == 0, body = %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":             true,
			"token":          "tok",
			"connection_url": "postgres://x",
			"extension":      "pgvector",
			"dimensions":     1536,
		})
	}))
	defer srv.Close()

	client := instant.New(instant.WithBaseURL(srv.URL))
	res, err := client.ProvisionVector(context.Background(), &instant.VectorOpts{
		ProvisionOpts: instant.ProvisionOpts{Name: "v"},
	})
	if err != nil {
		t.Fatalf("ProvisionVector: %v", err)
	}
	if res.Dimensions != 1536 {
		t.Errorf("Dimensions = %d; want server default 1536", res.Dimensions)
	}
}

// TestProvisionVector_SurfacesAPIError pins the error-envelope path: a 400
// invalid_dimensions must surface as *APIError with the canonical code intact.
func TestProvisionVector_SurfacesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":           false,
			"error":        "invalid_dimensions",
			"message":      "dimensions must be between 1 and 16000",
			"agent_action": "Pick a dimension in 1..16000 and retry.",
		})
	}))
	defer srv.Close()

	client := instant.New(instant.WithBaseURL(srv.URL))
	_, err := client.ProvisionVector(context.Background(), &instant.VectorOpts{
		ProvisionOpts: instant.ProvisionOpts{Name: "v"},
		Dimensions:    99999, // server-side range error
	})
	if err == nil {
		t.Fatal("expected an error on 400, got nil")
	}
	var apiErr *instant.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError; got %T (%v)", err, err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d; want 400", apiErr.StatusCode)
	}
	if apiErr.Code != "invalid_dimensions" {
		t.Errorf("Code = %q; want invalid_dimensions", apiErr.Code)
	}
}

// TestProvisionVector_PreflightValidation covers the client-side guard arms
// (nil opts, bad name, negative dimensions, empty token, empty connection_url)
// that fail before / after the network call.
func TestProvisionVector_PreflightValidation(t *testing.T) {
	client := instant.New(instant.WithBaseURL("http://unused.invalid"))

	if _, err := client.ProvisionVector(context.Background(), nil); err == nil {
		t.Error("nil opts: expected error")
	} else if !strings.Contains(err.Error(), "opts is required") {
		t.Errorf("nil opts error = %v", err)
	}

	if _, err := client.ProvisionVector(context.Background(), &instant.VectorOpts{}); err == nil {
		t.Error("empty name: expected error")
	}

	if _, err := client.ProvisionVector(context.Background(), &instant.VectorOpts{
		ProvisionOpts: instant.ProvisionOpts{Name: "ok"},
		Dimensions:    -5,
	}); err == nil {
		t.Error("negative dimensions: expected error")
	} else if !strings.Contains(err.Error(), "Dimensions must be >= 0") {
		t.Errorf("negative dimensions error = %v", err)
	}

	// Empty-token response branch.
	emptyTok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "connection_url": "postgres://x"})
	}))
	defer emptyTok.Close()
	tc := instant.New(instant.WithBaseURL(emptyTok.URL))
	if _, err := tc.ProvisionVector(context.Background(), &instant.VectorOpts{
		ProvisionOpts: instant.ProvisionOpts{Name: "v"},
	}); err == nil || !strings.Contains(err.Error(), "empty token") {
		t.Errorf("empty token: got err = %v", err)
	}

	// Empty-connection-url response branch.
	emptyURL := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "token": "tok"})
	}))
	defer emptyURL.Close()
	uc := instant.New(instant.WithBaseURL(emptyURL.URL))
	if _, err := uc.ProvisionVector(context.Background(), &instant.VectorOpts{
		ProvisionOpts: instant.ProvisionOpts{Name: "v"},
	}); err == nil || !strings.Contains(err.Error(), "empty connection_url") {
		t.Errorf("empty connection_url: got err = %v", err)
	}
}
