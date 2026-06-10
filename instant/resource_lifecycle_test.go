package instant

// resource_lifecycle_test.go — exercises PauseResource / ResumeResource
// (POST /api/v1/resources/:id/{pause,resume}) against httptest servers
// mirroring the api resource handler envelope
// ({ok, id, token, status, message, resource}).

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestPauseResource_HappyPath(t *testing.T) {
	srv, call := newOperateServer(t, http.StatusOK,
		`{"ok":true,"id":"3f4a7b2c","token":"tok-123","status":"paused","message":"Resource paused.","resource":{"id":"3f4a7b2c","token":"tok-123","resource_type":"postgres","tier":"pro","status":"paused"}}`)

	c := New(WithBaseURL(srv.URL), WithAPIKey("k"))
	res, err := c.PauseResource(context.Background(), "tok-123")
	if err != nil {
		t.Fatalf("PauseResource: %v", err)
	}

	if call.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", call.Method)
	}
	if call.Path != "/api/v1/resources/tok-123/pause" {
		t.Errorf("path = %q", call.Path)
	}
	if !res.OK || res.Status != "paused" || res.Token != "tok-123" {
		t.Errorf("result = %+v", res)
	}
	if res.Resource == nil || res.Resource.ResourceType != "postgres" || res.Resource.Status != "paused" {
		t.Errorf("Resource = %+v", res.Resource)
	}
}

func TestPauseResource_ValidationError(t *testing.T) {
	c := New(WithBaseURL("http://127.0.0.1:0"))
	if _, err := c.PauseResource(context.Background(), ""); err == nil ||
		!strings.Contains(err.Error(), "token is required") {
		t.Errorf("empty token: err = %v", err)
	}
}

func TestPauseResource_TierGate402(t *testing.T) {
	srv, _ := newOperateServer(t, http.StatusPaymentRequired,
		`{"ok":false,"error":"upgrade_required","upgrade_url":"https://instanode.dev/pricing","agent_action":"Pause/resume requires Pro+."}`)

	_, err := New(WithBaseURL(srv.URL)).PauseResource(context.Background(), "tok-123")
	if err == nil || !strings.Contains(err.Error(), "PauseResource") {
		t.Fatalf("expected PauseResource-prefixed error, got %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusPaymentRequired ||
		apiErr.UpgradeURL == "" {
		t.Errorf("expected 402 APIError with upgrade_url, got %v", err)
	}
}

func TestResumeResource_HappyPath(t *testing.T) {
	srv, call := newOperateServer(t, http.StatusOK,
		`{"ok":true,"id":"3f4a7b2c","token":"tok-123","status":"active","message":"Resource resumed."}`)

	c := New(WithBaseURL(srv.URL), WithAPIKey("k"))
	res, err := c.ResumeResource(context.Background(), "tok-123")
	if err != nil {
		t.Fatalf("ResumeResource: %v", err)
	}

	if call.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", call.Method)
	}
	if call.Path != "/api/v1/resources/tok-123/resume" {
		t.Errorf("path = %q", call.Path)
	}
	if !res.OK || res.Status != "active" {
		t.Errorf("result = %+v", res)
	}
	if res.Resource != nil {
		t.Errorf("Resource = %+v, want nil when server omits it", res.Resource)
	}
}

func TestResumeResource_ValidationError(t *testing.T) {
	c := New(WithBaseURL("http://127.0.0.1:0"))
	if _, err := c.ResumeResource(context.Background(), ""); err == nil ||
		!strings.Contains(err.Error(), "token is required") {
		t.Errorf("empty token: err = %v", err)
	}
}

func TestResumeResource_NotPaused409(t *testing.T) {
	srv, _ := newOperateServer(t, http.StatusConflict,
		`{"ok":false,"error":"not_paused","message":"Resource is not paused (current status: active)."}`)

	_, err := New(WithBaseURL(srv.URL)).ResumeResource(context.Background(), "tok-123")
	if err == nil || !strings.Contains(err.Error(), "ResumeResource") {
		t.Fatalf("expected ResumeResource-prefixed error, got %v", err)
	}
	if !IsConflict(err) {
		t.Errorf("expected 409 APIError, got %v", err)
	}
}
