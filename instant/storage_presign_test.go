package instant

// storage_presign_test.go — exercises PresignStorage
// (POST /storage/:token/presign) against an httptest server mirroring the api
// presign envelope ({ok, url, method, key, object_key, expires_at}).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestPresignStorage_HappyPath(t *testing.T) {
	srv, call := newOperateServer(t, http.StatusOK,
		`{"ok":true,"url":"https://s3.instanode.dev/instant-shared/t-1/uploads/a.png?X-Amz-Signature=x","method":"PUT","key":"uploads/a.png","object_key":"t-1/uploads/a.png","expires_at":"2026-06-11T12:00:00Z"}`)

	// No API key: presign is broker-mode — the token in the URL IS the credential.
	c := New(WithBaseURL(srv.URL))
	res, err := c.PresignStorage(context.Background(), "tok-123", PresignOpts{
		Operation: "PUT",
		Key:       "uploads/a.png",
		ExpiresIn: 900,
	})
	if err != nil {
		t.Fatalf("PresignStorage: %v", err)
	}

	if call.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", call.Method)
	}
	if call.Path != "/storage/tok-123/presign" {
		t.Errorf("path = %q", call.Path)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(call.Body), &body); err != nil {
		t.Fatalf("body decode: %v (raw %q)", err, call.Body)
	}
	if body["operation"] != "PUT" || body["key"] != "uploads/a.png" || body["expires_in"] != float64(900) {
		t.Errorf("body = %v", body)
	}
	if !res.OK || res.Method != "PUT" || res.ObjectKey != "t-1/uploads/a.png" ||
		res.ExpiresAt != "2026-06-11T12:00:00Z" || !strings.Contains(res.URL, "X-Amz-Signature") {
		t.Errorf("result = %+v", res)
	}
}

func TestPresignStorage_OmitsZeroExpiresIn(t *testing.T) {
	srv, call := newOperateServer(t, http.StatusOK,
		`{"ok":true,"url":"https://x","method":"GET","key":"k","object_key":"p/k","expires_at":"2026-06-11T12:00:00Z"}`)

	c := New(WithBaseURL(srv.URL))
	if _, err := c.PresignStorage(context.Background(), "tok-123", PresignOpts{
		Operation: "GET",
		Key:       "k",
	}); err != nil {
		t.Fatalf("PresignStorage: %v", err)
	}
	// ExpiresIn=0 must be omitted so the server default (600s) applies.
	if strings.Contains(call.Body, "expires_in") {
		t.Errorf("body = %q, want expires_in omitted", call.Body)
	}
}

func TestPresignStorage_ValidationErrors(t *testing.T) {
	c := New(WithBaseURL("http://127.0.0.1:0"))
	cases := []struct {
		name       string
		token      string
		opts       PresignOpts
		wantSubstr string
	}{
		{"empty token", "", PresignOpts{Operation: "GET", Key: "k"}, "token is required"},
		{"empty operation", "tok", PresignOpts{Key: "k"}, "Operation is required"},
		{"empty key", "tok", PresignOpts{Operation: "GET"}, "Key is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.PresignStorage(context.Background(), tc.token, tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("err = %v, want substring %q", err, tc.wantSubstr)
			}
		})
	}
}

func TestPresignStorage_APIError(t *testing.T) {
	srv, _ := newOperateServer(t, http.StatusGone,
		`{"ok":false,"error":"resource_inactive","message":"paused, expired, or deleted"}`)

	_, err := New(WithBaseURL(srv.URL)).PresignStorage(
		context.Background(), "tok-123", PresignOpts{Operation: "GET", Key: "k"})
	if err == nil || !strings.Contains(err.Error(), "PresignStorage") {
		t.Fatalf("expected PresignStorage-prefixed error, got %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusGone {
		t.Errorf("expected 410 APIError, got %v", err)
	}
}
