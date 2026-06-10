package instant

// stack_env_test.go — exercises UpdateStackEnv (PATCH /stacks/:slug/env)
// against an httptest server mirroring the api stack handler envelope
// ({ok, env, message}).

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestUpdateStackEnv_HappyPath(t *testing.T) {
	srv, call := newOperateServer(t, http.StatusOK,
		`{"ok":true,"env":{"LOG_LEVEL":"debug"},"message":"Env vars persisted. Redeploy the stack to apply."}`)

	c := New(WithBaseURL(srv.URL), WithAPIKey("k"))
	res, err := c.UpdateStackEnv(context.Background(), "stk-abc123", map[string]string{
		"LOG_LEVEL": "debug",
		"OLD_FLAG":  "", // empty value = delete the key server-side
	})
	if err != nil {
		t.Fatalf("UpdateStackEnv: %v", err)
	}

	if call.Method != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", call.Method)
	}
	if call.Path != "/stacks/stk-abc123/env" {
		t.Errorf("path = %q", call.Path)
	}
	var body struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal([]byte(call.Body), &body); err != nil {
		t.Fatalf("body decode: %v (raw %q)", err, call.Body)
	}
	if body.Env["LOG_LEVEL"] != "debug" {
		t.Errorf("body env = %v", body.Env)
	}
	if v, present := body.Env["OLD_FLAG"]; !present || v != "" {
		t.Errorf("empty-string delete marker must survive serialization; body env = %v", body.Env)
	}
	if !res.OK || res.Env["LOG_LEVEL"] != "debug" {
		t.Errorf("result = %+v", res)
	}
	if !strings.Contains(res.Message, "Redeploy") {
		t.Errorf("Message = %q, want redeploy reminder", res.Message)
	}
}

func TestUpdateStackEnv_ValidationErrors(t *testing.T) {
	c := New(WithBaseURL("http://127.0.0.1:0"))

	if _, err := c.UpdateStackEnv(context.Background(), "", map[string]string{"K": "v"}); err == nil ||
		!strings.Contains(err.Error(), "slug is required") {
		t.Errorf("empty slug: err = %v", err)
	}
	if _, err := c.UpdateStackEnv(context.Background(), "stk-abc123", map[string]string{}); err == nil ||
		!strings.Contains(err.Error(), "non-empty map") {
		t.Errorf("empty env: err = %v", err)
	}
}

func TestUpdateStackEnv_APIError(t *testing.T) {
	srv, _ := newOperateServer(t, http.StatusConflict,
		`{"ok":false,"error":"stack_deleting","message":"Stack is mid-teardown and cannot be modified"}`)

	_, err := New(WithBaseURL(srv.URL)).UpdateStackEnv(
		context.Background(), "stk-abc123", map[string]string{"K": "v"})
	if err == nil || !strings.Contains(err.Error(), "UpdateStackEnv") {
		t.Fatalf("expected UpdateStackEnv-prefixed error, got %v", err)
	}
	if !IsConflict(err) {
		t.Errorf("expected 409 APIError, got %v", err)
	}
}
