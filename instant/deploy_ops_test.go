package instant

// deploy_ops_test.go — exercises UpdateDeployEnv (PATCH /deploy/:id/env) and
// WakeDeployment (POST /deploy/:id/wake) against httptest servers mirroring
// the api deploy handler envelopes.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestUpdateDeployEnv_HappyPath(t *testing.T) {
	srv, call := newOperateServer(t, http.StatusOK,
		`{"ok":true,"env":{"FEATURE_X":"on","SECRET":"se****et"},"note":"Run POST /deploy/abc12345/redeploy to apply changes."}`)

	c := New(WithBaseURL(srv.URL), WithAPIKey("k"))
	res, err := c.UpdateDeployEnv(context.Background(), "abc12345", map[string]string{"FEATURE_X": "on"})
	if err != nil {
		t.Fatalf("UpdateDeployEnv: %v", err)
	}

	if call.Method != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", call.Method)
	}
	if call.Path != "/deploy/abc12345/env" {
		t.Errorf("path = %q", call.Path)
	}
	var body struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal([]byte(call.Body), &body); err != nil {
		t.Fatalf("body decode: %v (raw %q)", err, call.Body)
	}
	if body.Env["FEATURE_X"] != "on" {
		t.Errorf("body env = %v", body.Env)
	}
	if !res.OK || res.Env["FEATURE_X"] != "on" || res.Env["SECRET"] != "se****et" {
		t.Errorf("result = %+v", res)
	}
	if !strings.Contains(res.Note, "redeploy") {
		t.Errorf("Note = %q, want redeploy reminder", res.Note)
	}
}

func TestUpdateDeployEnv_ValidationErrors(t *testing.T) {
	c := New(WithBaseURL("http://127.0.0.1:0"))

	if _, err := c.UpdateDeployEnv(context.Background(), "", map[string]string{"K": "v"}); err == nil ||
		!strings.Contains(err.Error(), "id is required") {
		t.Errorf("empty id: err = %v", err)
	}
	if _, err := c.UpdateDeployEnv(context.Background(), "abc12345", nil); err == nil ||
		!strings.Contains(err.Error(), "non-empty map") {
		t.Errorf("empty env: err = %v", err)
	}
}

func TestUpdateDeployEnv_APIError(t *testing.T) {
	srv, _ := newOperateServer(t, http.StatusNotFound,
		`{"ok":false,"error":"not_found","message":"Deployment not found"}`)

	_, err := New(WithBaseURL(srv.URL)).UpdateDeployEnv(
		context.Background(), "abc12345", map[string]string{"K": "v"})
	if err == nil || !strings.Contains(err.Error(), "UpdateDeployEnv") {
		t.Fatalf("expected UpdateDeployEnv-prefixed error, got %v", err)
	}
	if !IsNotFound(err) {
		t.Errorf("expected 404 APIError, got %v", err)
	}
}

func TestWakeDeployment_HappyPath(t *testing.T) {
	srv, call := newOperateServer(t, http.StatusOK,
		`{"ok":true,"message":"Deployment woken — the app will be reachable once its pod is Ready (cold start).","deployment":{"id":"3f4a","app_id":"abc12345","status":"deploying","url":"https://abc12345.deployment.instanode.dev"}}`)

	c := New(WithBaseURL(srv.URL), WithAPIKey("k"))
	res, err := c.WakeDeployment(context.Background(), "abc12345")
	if err != nil {
		t.Fatalf("WakeDeployment: %v", err)
	}

	if call.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", call.Method)
	}
	if call.Path != "/deploy/abc12345/wake" {
		t.Errorf("path = %q", call.Path)
	}
	if call.Body != "" {
		t.Errorf("body = %q, want empty", call.Body)
	}
	if !res.OK || !strings.Contains(res.Message, "woken") {
		t.Errorf("result = %+v", res)
	}
	if res.Deployment == nil || res.Deployment.AppID != "abc12345" || res.Deployment.Status != "deploying" {
		t.Errorf("Deployment = %+v", res.Deployment)
	}
}

func TestWakeDeployment_ValidationError(t *testing.T) {
	c := New(WithBaseURL("http://127.0.0.1:0"))
	if _, err := c.WakeDeployment(context.Background(), ""); err == nil ||
		!strings.Contains(err.Error(), "id is required") {
		t.Errorf("empty id: err = %v", err)
	}
}

func TestWakeDeployment_FlagGated501(t *testing.T) {
	srv, _ := newOperateServer(t, http.StatusNotImplemented,
		`{"ok":false,"error":"scale_to_zero_disabled","message":"Scale-to-zero is not enabled on this platform."}`)

	_, err := New(WithBaseURL(srv.URL)).WakeDeployment(context.Background(), "abc12345")
	if err == nil || !strings.Contains(err.Error(), "WakeDeployment") {
		t.Fatalf("expected WakeDeployment-prefixed error, got %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotImplemented ||
		apiErr.CanonicalCode() != "scale_to_zero_disabled" {
		t.Errorf("expected 501 scale_to_zero_disabled APIError, got %v", err)
	}
}
