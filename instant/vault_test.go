package instant

// vault_test.go — exercises SetVaultKey / RotateVaultKey against httptest
// servers mirroring the api vault handler envelope
// (api/internal/handlers/vault.go: {ok, key, env, version}).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// operateCall records the single request an operate-verb test server saw.
type operateCall struct {
	Method string
	Path   string
	// EscapedPath preserves percent-encoding (e.g. %2F) that Path decodes.
	EscapedPath string
	Query       string
	Body        string
}

// newOperateServer returns an httptest server that records the request
// method/path/body into the returned *operateCall and serves the given
// status + JSON body. Shared by every operate-verb test file.
func newOperateServer(t *testing.T, status int, respBody string) (*httptest.Server, *operateCall) {
	t.Helper()
	call := &operateCall{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call.Method = r.Method
		call.Path = r.URL.Path
		call.EscapedPath = r.URL.EscapedPath()
		call.Query = r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		call.Body = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	return srv, call
}

func TestSetVaultKey_HappyPath(t *testing.T) {
	srv, call := newOperateServer(t, http.StatusCreated,
		`{"ok":true,"key":"STRIPE_KEY","env":"production","version":2}`)

	c := New(WithBaseURL(srv.URL), WithAPIKey("k"))
	res, err := c.SetVaultKey(context.Background(), "production", "STRIPE_KEY", "sk_live_x")
	if err != nil {
		t.Fatalf("SetVaultKey: %v", err)
	}

	if call.Method != http.MethodPut {
		t.Errorf("method = %q, want PUT", call.Method)
	}
	if call.Path != "/api/v1/vault/production/STRIPE_KEY" {
		t.Errorf("path = %q", call.Path)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(call.Body), &body); err != nil {
		t.Fatalf("body decode: %v (raw %q)", err, call.Body)
	}
	if body["value"] != "sk_live_x" {
		t.Errorf("body value = %q, want sk_live_x", body["value"])
	}
	if !res.OK || res.Key != "STRIPE_KEY" || res.Env != "production" || res.Version != 2 {
		t.Errorf("result = %+v", res)
	}
}

func TestSetVaultKey_PathEscapesSegments(t *testing.T) {
	srv, call := newOperateServer(t, http.StatusCreated,
		`{"ok":true,"key":"a/b","env":"prod","version":1}`)

	c := New(WithBaseURL(srv.URL))
	if _, err := c.SetVaultKey(context.Background(), "prod", "a/b", "v"); err != nil {
		t.Fatalf("SetVaultKey: %v", err)
	}
	// A key containing a slash must be escaped into ONE path segment, not two.
	if call.EscapedPath != "/api/v1/vault/prod/a%2Fb" {
		t.Errorf("escaped path = %q, want /api/v1/vault/prod/a%%2Fb", call.EscapedPath)
	}
}

func TestSetVaultKey_ValidationErrors(t *testing.T) {
	c := New(WithBaseURL("http://127.0.0.1:0"))
	cases := []struct {
		name            string
		env, key, value string
		wantSubstr      string
	}{
		{"empty env", "", "K", "v", "env is required"},
		{"empty key", "production", "", "v", "key is required"},
		{"empty value", "production", "K", "", "value is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.SetVaultKey(context.Background(), tc.env, tc.key, tc.value)
			if err == nil || !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("err = %v, want substring %q", err, tc.wantSubstr)
			}
		})
	}
}

func TestSetVaultKey_APIError(t *testing.T) {
	srv, _ := newOperateServer(t, http.StatusForbidden,
		`{"ok":false,"error":"vault_not_available"}`)

	_, err := New(WithBaseURL(srv.URL)).SetVaultKey(context.Background(), "production", "K", "v")
	if err == nil || !strings.Contains(err.Error(), "SetVaultKey") {
		t.Fatalf("expected SetVaultKey-prefixed error, got %v", err)
	}
	if !IsForbidden(err) {
		t.Errorf("expected 403 APIError, got %v", err)
	}
}

func TestRotateVaultKey_HappyPath(t *testing.T) {
	srv, call := newOperateServer(t, http.StatusOK,
		`{"ok":true,"key":"STRIPE_KEY","env":"production","version":3}`)

	c := New(WithBaseURL(srv.URL), WithAPIKey("k"))
	res, err := c.RotateVaultKey(context.Background(), "production", "STRIPE_KEY", "sk_live_new")
	if err != nil {
		t.Fatalf("RotateVaultKey: %v", err)
	}

	if call.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", call.Method)
	}
	if call.Path != "/api/v1/vault/production/STRIPE_KEY/rotate" {
		t.Errorf("path = %q", call.Path)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(call.Body), &body); err != nil {
		t.Fatalf("body decode: %v", err)
	}
	if body["value"] != "sk_live_new" {
		t.Errorf("body value = %q", body["value"])
	}
	if res.Version != 3 {
		t.Errorf("Version = %d, want 3", res.Version)
	}
}

func TestRotateVaultKey_ValidationError(t *testing.T) {
	c := New(WithBaseURL("http://127.0.0.1:0"))
	_, err := c.RotateVaultKey(context.Background(), "", "K", "v")
	if err == nil || !strings.Contains(err.Error(), "RotateVaultKey") {
		t.Errorf("err = %v, want RotateVaultKey-prefixed validation error", err)
	}
}

func TestRotateVaultKey_APIError(t *testing.T) {
	srv, _ := newOperateServer(t, http.StatusUnauthorized,
		`{"ok":false,"error":"unauthorized"}`)

	_, err := New(WithBaseURL(srv.URL)).RotateVaultKey(context.Background(), "production", "K", "v")
	if err == nil || !strings.Contains(err.Error(), "RotateVaultKey") {
		t.Fatalf("expected RotateVaultKey-prefixed error, got %v", err)
	}
	if !IsUnauthorized(err) {
		t.Errorf("expected 401 APIError, got %v", err)
	}
}
