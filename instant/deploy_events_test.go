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

// TestDeploymentEvents_HappyPath verifies GET /api/v1/deployments/:id/events
// parsing: the autopsy timeline (kind/reason/exit_code/last_lines/hint), the
// nullable exit_code (present on one row, null on another), and the
// deployment_id + count envelope. It also asserts the limit query is sent.
func TestDeploymentEvents_HappyPath(t *testing.T) {
	var gotLimit string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/deployments/6fffcc21/events" {
			t.Errorf("path = %q; want /api/v1/deployments/6fffcc21/events", r.URL.Path)
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %q; want GET", r.Method)
		}
		gotLimit = r.URL.Query().Get("limit")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":            true,
			"deployment_id": "11111111-2222-3333-4444-555555555555",
			"count":         2,
			"events": []map[string]any{
				{
					"kind":       "build",
					"reason":     "BackoffLimitExceeded",
					"event":      "Job has reached the specified backoff limit",
					"exit_code":  1,
					"last_lines": "npm ERR! missing script: build",
					"hint":       "Add a build script to package.json",
					"created_at": "2026-06-10T12:00:00Z",
				},
				{
					"kind":       "rollout",
					"reason":     "ProgressDeadlineExceeded",
					"event":      "Deployment exceeded its progress deadline",
					"exit_code":  nil,
					"last_lines": "",
					"hint":       "Check the container's readiness probe",
					"created_at": "2026-06-10T12:05:00Z",
				},
			},
		})
	}))
	defer srv.Close()

	client := instant.New(instant.WithBaseURL(srv.URL))
	list, err := client.DeploymentEvents(context.Background(), "6fffcc21", 25)
	if err != nil {
		t.Fatalf("DeploymentEvents: %v", err)
	}

	if gotLimit != "25" {
		t.Errorf("limit query = %q; want 25", gotLimit)
	}
	if list.DeploymentID != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("DeploymentID = %q", list.DeploymentID)
	}
	if list.Count != 2 {
		t.Errorf("Count = %d; want 2", list.Count)
	}
	if len(list.Events) != 2 {
		t.Fatalf("len(Events) = %d; want 2", len(list.Events))
	}

	first := list.Events[0]
	if first.Kind != "build" || first.Reason != "BackoffLimitExceeded" {
		t.Errorf("Events[0] kind/reason = %q/%q", first.Kind, first.Reason)
	}
	if first.ExitCode == nil || *first.ExitCode != 1 {
		t.Errorf("Events[0].ExitCode = %v; want 1", first.ExitCode)
	}
	if first.LastLines != "npm ERR! missing script: build" {
		t.Errorf("Events[0].LastLines = %q", first.LastLines)
	}
	if first.Hint != "Add a build script to package.json" {
		t.Errorf("Events[0].Hint = %q", first.Hint)
	}

	second := list.Events[1]
	if second.ExitCode != nil {
		t.Errorf("Events[1].ExitCode = %v; want nil (null exit_code)", *second.ExitCode)
	}
	if second.Reason != "ProgressDeadlineExceeded" {
		t.Errorf("Events[1].Reason = %q", second.Reason)
	}
}

// TestDeploymentEvents_DefaultLimit verifies that passing limit <= 0 omits the
// limit query so the server applies its own default (50).
func TestDeploymentEvents_DefaultLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q; want empty (no limit) when limit <= 0", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "deployment_id": "d", "count": 0, "events": []any{},
		})
	}))
	defer srv.Close()

	client := instant.New(instant.WithBaseURL(srv.URL))
	list, err := client.DeploymentEvents(context.Background(), "6fffcc21", 0)
	if err != nil {
		t.Fatalf("DeploymentEvents: %v", err)
	}
	if list.Count != 0 || len(list.Events) != 0 {
		t.Errorf("expected empty timeline, got count=%d len=%d", list.Count, len(list.Events))
	}
}

// TestDeploymentEvents_NotFound pins the 404 path → *APIError (IsNotFound), and
// the empty-id preflight guard.
func TestDeploymentEvents_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "not_found", "message": "Deployment not found"})
	}))
	defer srv.Close()

	client := instant.New(instant.WithBaseURL(srv.URL))
	_, err := client.DeploymentEvents(context.Background(), "missing0", 0)
	if !instant.IsNotFound(err) {
		t.Fatalf("expected IsNotFound; got %v", err)
	}
	var apiErr *instant.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError; got %T", err)
	}

	// Empty-id preflight.
	if _, err := client.DeploymentEvents(context.Background(), "", 0); err == nil || !strings.Contains(err.Error(), "id is required") {
		t.Errorf("empty id: got err = %v", err)
	}
}

// TestDeploymentEvents_SurfacesUnauthorized confirms a 401 (no/invalid API key)
// surfaces as *APIError with the right helper predicate — the events endpoint
// requires auth.
func TestDeploymentEvents_SurfacesUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":           false,
			"error":        "unauthorized",
			"error_code":   "missing_credentials",
			"message":      "A valid API key is required",
			"agent_action": "Have the user log in and pass the API key.",
		})
	}))
	defer srv.Close()

	client := instant.New(instant.WithBaseURL(srv.URL))
	_, err := client.DeploymentEvents(context.Background(), "6fffcc21", 0)
	if !instant.IsUnauthorized(err) {
		t.Fatalf("expected IsUnauthorized; got %v", err)
	}
	var apiErr *instant.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError; got %T", err)
	}
	if apiErr.CanonicalCode() != "missing_credentials" {
		t.Errorf("CanonicalCode = %q; want missing_credentials", apiErr.CanonicalCode())
	}
}
