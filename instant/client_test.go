package instant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// roundTripFunc adapts a plain function into an http.RoundTripper so tests can
// inject deterministic responses without spinning up an httptest server.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// TestNew_DefaultsAndOptions checks that New() applies built-in defaults and
// option overrides (URL, API key, timeout, custom logger).
func TestNew_DefaultsAndOptions(t *testing.T) {
	t.Setenv("INSTANT_API_KEY", "")
	t.Setenv("INSTANT_API_URL", "")
	c := New()
	if c.baseURL != DefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, DefaultBaseURL)
	}
	if c.apiKey != "" {
		t.Errorf("apiKey should default to empty, got %q", c.apiKey)
	}
	if c.httpClient.Timeout != defaultTimeout {
		t.Errorf("Timeout = %v, want %v", c.httpClient.Timeout, defaultTimeout)
	}

	custom := New(
		WithBaseURL("http://example/"),
		WithAPIKey("sk_test_123"),
		WithTimeout(7*time.Second),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)
	if custom.baseURL != "http://example" {
		t.Errorf("baseURL should trim trailing slash, got %q", custom.baseURL)
	}
	if custom.apiKey != "sk_test_123" {
		t.Errorf("apiKey = %q", custom.apiKey)
	}
	if custom.httpClient.Timeout != 7*time.Second {
		t.Errorf("WithTimeout not applied, got %v", custom.httpClient.Timeout)
	}
}

// TestNew_EnvOverrides verifies INSTANT_API_KEY / INSTANT_API_URL feed New().
func TestNew_EnvOverrides(t *testing.T) {
	t.Setenv("INSTANT_API_KEY", "sk_env_abc")
	t.Setenv("INSTANT_API_URL", "http://envhost:9999/")
	c := New()
	if c.apiKey != "sk_env_abc" {
		t.Errorf("env api key not picked up: %q", c.apiKey)
	}
	if c.baseURL != "http://envhost:9999" {
		t.Errorf("env api url not picked up or not trimmed: %q", c.baseURL)
	}
}

// TestWithHTTPClient_PreservesTransport ensures a caller-supplied Transport is
// chained beneath the SDK's authTransport rather than discarded.
func TestWithHTTPClient_PreservesTransport(t *testing.T) {
	called := false
	custom := &http.Client{
		Timeout: 12 * time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			called = true
			if req.Header.Get("Authorization") != "Bearer sk_test_999" {
				t.Errorf("Authorization not set; got %q", req.Header.Get("Authorization"))
			}
			if req.Header.Get("User-Agent") == "" {
				t.Error("User-Agent should be set by authTransport")
			}
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	c := New(WithAPIKey("sk_test_999"), WithHTTPClient(custom))
	if c.httpClient.Timeout != 12*time.Second {
		t.Errorf("Timeout from custom client not preserved: %v", c.httpClient.Timeout)
	}

	var out map[string]any
	if err := c.get(context.Background(), "/somewhere", &out); err != nil {
		t.Fatalf("get: %v", err)
	}
	if !called {
		t.Error("custom round tripper not invoked")
	}
}

// TestWithHTTPClient_NilNoop confirms passing nil is a no-op (no panic).
func TestWithHTTPClient_NilNoop(t *testing.T) {
	c := New(WithHTTPClient(nil))
	if c.httpClient == nil {
		t.Fatal("httpClient should still be non-nil after WithHTTPClient(nil)")
	}
}

// TestRetryOn5xx verifies that the client retries once on a 5xx response.
func TestRetryOn5xx(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	var out map[string]any
	if err := c.get(context.Background(), "/x", &out); err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if hits != 2 {
		t.Errorf("expected exactly 2 hits (1 fail, 1 retry), got %d", hits)
	}
}

// TestAPIError_DecodedFromBody decodes the structured error envelope on 4xx.
func TestAPIError_DecodedFromBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":"forbidden","message":"nope"}`)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	var out map[string]any
	err := c.get(context.Background(), "/x", &out)
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.StatusCode != 403 {
		t.Errorf("StatusCode = %d", apiErr.StatusCode)
	}
	if apiErr.Code != "forbidden" {
		t.Errorf("Code = %q", apiErr.Code)
	}
	if !IsForbidden(err) {
		t.Error("IsForbidden should return true")
	}
}

// TestAPIError_RawFallback covers the path where the server returns a 4xx with
// a non-JSON body (e.g. a plain-text reverse-proxy 404). Error() must use raw.
func TestAPIError_RawFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `garbled not-json`)
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	var out map[string]any
	err := c.get(context.Background(), "/x", &out)
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsNotFound(err) {
		t.Error("IsNotFound should match the 404")
	}
	if !strings.Contains(err.Error(), "garbled") {
		t.Errorf("expected raw body in error string, got %q", err.Error())
	}
}

// TestErrorPredicates covers every Is*() helper in types.go.
func TestErrorPredicates(t *testing.T) {
	cases := []struct {
		status int
		check  func(error) bool
		name   string
	}{
		{401, IsUnauthorized, "401"},
		{403, IsForbidden, "403"},
		{404, IsNotFound, "404"},
		{409, IsConflict, "409"},
		{429, IsRateLimited, "429"},
		{503, IsServiceUnavailable, "503"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := &APIError{StatusCode: tc.status, Code: "x"}
			if !tc.check(err) {
				t.Errorf("predicate did not match status %d", tc.status)
			}
			// non-APIError must yield false.
			if tc.check(errors.New("plain")) {
				t.Error("predicate matched a non-APIError")
			}
		})
	}
	// Error() formatting paths.
	e1 := &APIError{StatusCode: 500, Code: "boom", Message: "oh no"}
	if !strings.Contains(e1.Error(), "boom") || !strings.Contains(e1.Error(), "500") {
		t.Errorf("Error() = %q", e1.Error())
	}
	e2 := &APIError{StatusCode: 500}
	if !strings.Contains(e2.Error(), "500") {
		t.Errorf("Error() = %q", e2.Error())
	}
}

// TestLogHeaders ensures advisory headers reach the logger without panicking.
func TestLogHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Instant-Notice", "be advised")
		w.Header().Set("X-Instant-Upgrade", "https://instanode.dev/pricing")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL), WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	var out map[string]any
	if err := c.get(context.Background(), "/x", &out); err != nil {
		t.Fatalf("get: %v", err)
	}
}

// TestPostAndDelete exercises post()/delete() helper paths.
func TestPostAndDelete(t *testing.T) {
	var (
		methods []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	var out map[string]any
	if err := c.post(context.Background(), "/p", &out); err != nil {
		t.Fatalf("post: %v", err)
	}
	if err := c.delete(context.Background(), "/d", &out); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(methods) != 2 || methods[0] != "POST" || methods[1] != "DELETE" {
		t.Errorf("methods = %v", methods)
	}
}

// TestPostJSONMarshalError forces json.Marshal to fail by sending an unsupported type.
func TestPostJSONMarshalError(t *testing.T) {
	c := New(WithBaseURL("http://unused"))
	// channels are not JSON-marshalable
	err := c.postJSON(context.Background(), "/x", make(chan int), nil)
	if err == nil || !strings.Contains(err.Error(), "marshalling") {
		t.Errorf("expected marshalling error, got %v", err)
	}
}

// TestDoCtxCancelled returns ctx.Err() on cancellation.
func TestDoCtxCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out map[string]any
	err := c.get(ctx, "/x", &out)
	if err == nil {
		t.Fatal("expected error on cancelled ctx")
	}
}

// TestPostJSONWithHeaders sends an Idempotency-Key and asserts it lands.
func TestPostJSONWithHeaders(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Idempotency-Key")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	var out map[string]any
	if err := c.postJSONWithHeaders(context.Background(), "/x",
		map[string]string{"a": "b"},
		map[string]string{"Idempotency-Key": "k-1"}, &out); err != nil {
		t.Fatalf("post: %v", err)
	}
	if got != "k-1" {
		t.Errorf("idempotency key not propagated; got %q", got)
	}
}

// TestUserAgentEnv pins that UA contains at least the package marker.
func TestUserAgentEnv(t *testing.T) {
	t.Setenv("INSTANT_API_KEY", "")
	c := New()
	tr, ok := c.httpClient.Transport.(*authTransport)
	if !ok {
		t.Fatal("Transport is not *authTransport")
	}
	if tr.userAgent == "" {
		t.Error("userAgent must be set")
	}
}

// helper — collapse json body to map for assertions.
func decodeJSONBody(t *testing.T, body io.Reader) map[string]any {
	t.Helper()
	b, _ := io.ReadAll(body)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	return out
}

// fakeProvisionServer returns an httptest server that records the inbound body and
// replies with a canned JSON envelope.
func fakeProvisionServer(t *testing.T, wantPath string, resp map[string]any) (*httptest.Server, *map[string]any) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wantPath {
			t.Errorf("path = %q; want %q", r.URL.Path, wantPath)
		}
		captured = decodeJSONBody(t, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	return srv, &captured
}

func TestProvisionDatabase(t *testing.T) {
	srv, body := fakeProvisionServer(t, "/db/new", map[string]any{
		"ok": true, "token": "tok-db", "connection_url": "postgres://h",
		"tier": "anonymous", "note": "upgrade me",
	})
	defer srv.Close()
	c := New(WithBaseURL(srv.URL), WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	r, err := c.ProvisionDatabase(context.Background(), &ProvisionOpts{Name: "db1"})
	if err != nil {
		t.Fatalf("ProvisionDatabase: %v", err)
	}
	if r.Token != "tok-db" || r.ConnectionURL != "postgres://h" {
		t.Errorf("got %+v", r)
	}
	if (*body)["name"] != "db1" {
		t.Errorf("name = %v", (*body)["name"])
	}

	// nil opts
	if _, err := c.ProvisionDatabase(context.Background(), nil); err == nil {
		t.Error("nil opts should error")
	}
	// invalid name
	if _, err := c.ProvisionDatabase(context.Background(), &ProvisionOpts{Name: " bad"}); err == nil {
		t.Error("invalid name should error")
	}
}

func TestProvisionDatabase_EmptyTokenErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "token": "", "connection_url": "x"})
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	if _, err := c.ProvisionDatabase(context.Background(), &ProvisionOpts{Name: "ok"}); err == nil {
		t.Error("empty token should error")
	}

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "token": "t", "connection_url": ""})
	}))
	defer srv2.Close()
	c2 := New(WithBaseURL(srv2.URL))
	if _, err := c2.ProvisionDatabase(context.Background(), &ProvisionOpts{Name: "ok"}); err == nil {
		t.Error("empty connection_url should error")
	}
}

func TestProvisionCache(t *testing.T) {
	srv, _ := fakeProvisionServer(t, "/cache/new", map[string]any{
		"ok": true, "token": "tok-r", "connection_url": "redis://h", "tier": "hobby",
	})
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	r, err := c.ProvisionCache(context.Background(), &ProvisionOpts{Name: "c1"})
	if err != nil {
		t.Fatalf("ProvisionCache: %v", err)
	}
	if r.Token == "" {
		t.Error("empty token")
	}
	if _, err := c.ProvisionCache(context.Background(), nil); err == nil {
		t.Error("nil opts should error")
	}
}

func TestProvisionMongoDB(t *testing.T) {
	srv, _ := fakeProvisionServer(t, "/nosql/new", map[string]any{
		"ok": true, "token": "tok-m", "connection_url": "mongodb://h", "tier": "pro",
	})
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	r, err := c.ProvisionMongoDB(context.Background(), &ProvisionOpts{Name: "m1"})
	if err != nil {
		t.Fatalf("ProvisionMongoDB: %v", err)
	}
	if r.Token != "tok-m" {
		t.Errorf("token = %q", r.Token)
	}
	if _, err := c.ProvisionMongoDB(context.Background(), &ProvisionOpts{Name: ""}); err == nil {
		t.Error("empty name should error")
	}
}

func TestProvisionQueue(t *testing.T) {
	srv, _ := fakeProvisionServer(t, "/queue/new", map[string]any{
		"ok": true, "token": "tok-q", "connection_url": "nats://h",
	})
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	r, err := c.ProvisionQueue(context.Background(), &ProvisionOpts{Name: "q1"})
	if err != nil {
		t.Fatalf("ProvisionQueue: %v", err)
	}
	if r.Token != "tok-q" {
		t.Errorf("token = %q", r.Token)
	}
	if _, err := c.ProvisionQueue(context.Background(), nil); err == nil {
		t.Error("nil opts should error")
	}
}

func TestProvisionWebhook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/webhook/new" {
			http.Error(w, "bad path", 404)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "token": "tok-w", "receive_url": "https://x/hook", "note": "n",
		})
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	r, err := c.ProvisionWebhook(context.Background(), &ProvisionOpts{Name: "wh"})
	if err != nil {
		t.Fatalf("ProvisionWebhook: %v", err)
	}
	if r.ReceiveURL != "https://x/hook" {
		t.Errorf("ReceiveURL = %q", r.ReceiveURL)
	}
	if _, err := c.ProvisionWebhook(context.Background(), nil); err == nil {
		t.Error("nil opts should error")
	}

	// Empty token -> error
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "receive_url": "x"})
	}))
	defer srv2.Close()
	if _, err := New(WithBaseURL(srv2.URL)).ProvisionWebhook(context.Background(), &ProvisionOpts{Name: "x"}); err == nil {
		t.Error("expected empty-token error")
	}
}

func TestProvisionStorage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != storagePath {
			http.Error(w, "bad", 404)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "token": "tok-s", "connection_url": "https://x/b/p/",
			"endpoint":    "nyc3.do",
			"presign_url": "/storage/tok-s/presign",
			"mode":        "broker",
		})
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	r, err := c.ProvisionStorage(context.Background(), &ProvisionOpts{Name: "s1"})
	if err != nil {
		t.Fatalf("ProvisionStorage: %v", err)
	}
	if !strings.HasPrefix(r.PresignURL, srv.URL) {
		t.Errorf("PresignURL not absolutized: %q", r.PresignURL)
	}
	if r.Mode != "broker" {
		t.Errorf("Mode = %q", r.Mode)
	}
	if _, err := c.ProvisionStorage(context.Background(), nil); err == nil {
		t.Error("nil opts should error")
	}

	// empty connection_url -> error
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "token": "t", "connection_url": ""})
	}))
	defer srv2.Close()
	if _, err := New(WithBaseURL(srv2.URL)).ProvisionStorage(context.Background(), &ProvisionOpts{Name: "x"}); err == nil {
		t.Error("expected empty connection_url error")
	}
}

func TestAbsoluteURL(t *testing.T) {
	cases := []struct{ base, in, want string }{
		{"http://x", "", ""},
		{"http://x", "https://abs/y", "https://abs/y"},
		{"http://x", "http://abs/y", "http://abs/y"},
		{"http://x", "/rel/z", "http://x/rel/z"},
		{"http://x/", "rel", "http://x/rel"},
	}
	for _, c := range cases {
		got := absoluteURL(c.base, c.in)
		if got != c.want {
			t.Errorf("absoluteURL(%q,%q) = %q; want %q", c.base, c.in, got, c.want)
		}
	}
}

func TestClaim(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := decodeJSONBody(t, r.Body)
		// Canonical wire field is `token` (api ClaimRequest, 2026-05-20).
		// The SDK must never send the deprecated `jwt` alias even when the
		// caller supplied the deprecated [ClaimOpts.JWT] field.
		if _, hasJWT := body["jwt"]; hasJWT {
			t.Errorf("body must not contain deprecated `jwt` field; got %+v", body)
		}
		if body["token"] != "ey.j.j" || body["email"] != "a@b.c" || body["team_name"] != "Acme" {
			t.Errorf("body = %+v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":            true,
			"team_id":       "T",
			"user_id":       "U",
			"session_token": "sess.jwt.tok",
			"message":       "ok",
		})
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	r, err := c.Claim(context.Background(), ClaimOpts{Token: "ey.j.j", Email: "a@b.c", TeamName: "Acme"})
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if r.TeamID != "T" {
		t.Errorf("TeamID = %q", r.TeamID)
	}
	if r.SessionToken != "sess.jwt.tok" {
		t.Errorf("SessionToken = %q, want sess.jwt.tok", r.SessionToken)
	}
	if _, err := c.Claim(context.Background(), ClaimOpts{Email: "x"}); err == nil {
		t.Error("missing Token should error")
	}
	if _, err := c.Claim(context.Background(), ClaimOpts{Token: "x"}); err == nil {
		t.Error("missing Email should error")
	}
}

// TestClaim_JWTFieldBackwardCompat — the deprecated [ClaimOpts.JWT] field is
// still accepted as a fallback so existing call sites compile unchanged, but
// the wire body must still send the canonical `token` field.
func TestClaim_JWTFieldBackwardCompat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := decodeJSONBody(t, r.Body)
		if _, hasJWT := body["jwt"]; hasJWT {
			t.Errorf("body must not contain deprecated `jwt` field; got %+v", body)
		}
		if body["token"] != "legacy.jwt.val" {
			t.Errorf("token field = %v, want legacy.jwt.val", body["token"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "team_id": "T", "user_id": "U", "message": "ok",
		})
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	// Caller supplies legacy JWT field; SDK must translate to canonical wire.
	_, err := c.Claim(context.Background(), ClaimOpts{JWT: "legacy.jwt.val", Email: "a@b.c"})
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
}

// TestClaim_TokenWinsOverJWT — when both fields are set, the canonical
// Token field takes precedence (mirrors api ClaimRequest.claimToken).
func TestClaim_TokenWinsOverJWT(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := decodeJSONBody(t, r.Body)
		if body["token"] != "canonical.tok" {
			t.Errorf("token = %v, want canonical.tok (Token must win over JWT)", body["token"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "team_id": "T", "user_id": "U", "message": "ok",
		})
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	_, err := c.Claim(context.Background(), ClaimOpts{
		Token: "canonical.tok",
		JWT:   "deprecated.tok",
		Email: "a@b.c",
	})
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
}

func TestClaimTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-1" {
			t.Errorf("Auth header = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "team_id": "T", "user_id": "U", "message": "claimed",
		})
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	r, err := c.ClaimTokens(context.Background(), "sk-1", []string{"a", "b"})
	if err != nil {
		t.Fatalf("ClaimTokens: %v", err)
	}
	if r.Message != "claimed" {
		t.Errorf("Message = %q", r.Message)
	}
	if _, err := c.ClaimTokens(context.Background(), "", []string{"a"}); err == nil {
		t.Error("empty apiKey should error")
	}
	if _, err := c.ClaimTokens(context.Background(), "k", nil); err == nil {
		t.Error("empty tokens should error")
	}
}

func TestListResources(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "" && r.URL.Query().Get("limit") != "5" {
			t.Errorf("limit = %q", r.URL.Query().Get("limit"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "items": []map[string]any{{"id": "i1", "token": "t1"}},
			"total": 1, "next_cursor": "c1",
		})
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL), WithAPIKey("k"))
	list, err := c.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(list.Items) != 1 || list.NextCursor != "c1" {
		t.Errorf("unexpected list: %+v", list)
	}
	if _, err := c.ListResourcesPage(context.Background(), ListResourcesOpts{Cursor: "c", Limit: 5}); err != nil {
		t.Errorf("ListResourcesPage: %v", err)
	}
}

func TestGetResource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/tok-x") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "item": map[string]any{"id": "X", "token": "tok-x", "status": "active"},
		})
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	r, err := c.GetResource(context.Background(), "tok-x")
	if err != nil {
		t.Fatalf("GetResource: %v", err)
	}
	if r.Token != "tok-x" {
		t.Errorf("Token = %q", r.Token)
	}
}

func TestDeleteResource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("method = %s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "message": "gone"})
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	if err := c.DeleteResource(context.Background(), "tok"); err != nil {
		t.Fatalf("DeleteResource: %v", err)
	}
}

func TestRotateCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "connection_url": "postgres://new"})
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	r, err := c.RotateCredentials(context.Background(), "tok")
	if err != nil {
		t.Fatalf("RotateCredentials: %v", err)
	}
	if r.ConnectionURL != "postgres://new" {
		t.Errorf("ConnectionURL = %q", r.ConnectionURL)
	}

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv2.Close()
	if _, err := New(WithBaseURL(srv2.URL)).RotateCredentials(context.Background(), "tok"); err == nil {
		t.Error("empty connection_url should error")
	}
}

// TestValidateResourceName covers happy + error paths in validate.go.
func TestValidateResourceName(t *testing.T) {
	if err := validateResourceName(""); err == nil {
		t.Error("empty should error")
	}
	if err := validateResourceName(strings.Repeat("a", 65)); err == nil {
		t.Error(">64 should error")
	}
	if err := validateResourceName(" leading"); err == nil {
		t.Error("leading space should error")
	}
	if err := validateResourceName("ok_name-1"); err != nil {
		t.Errorf("valid name rejected: %v", err)
	}
}

func TestProvisionBody_HeaderAndIdempotency(t *testing.T) {
	if h := provisionHeaders(nil); h != nil {
		t.Errorf("nil opts should yield nil headers, got %v", h)
	}
	if h := provisionHeaders(&ProvisionOpts{}); h != nil {
		t.Errorf("empty idem key should yield nil headers, got %v", h)
	}
	h := provisionHeaders(&ProvisionOpts{IdempotencyKey: "K"})
	if h["Idempotency-Key"] != "K" {
		t.Errorf("idem header not set: %v", h)
	}
	if _, err := provisionBody(nil); err == nil {
		t.Error("nil opts should error")
	}
	if _, err := provisionBody(&ProvisionOpts{Name: ""}); err == nil {
		t.Error("empty name should error")
	}
	body, err := provisionBody(&ProvisionOpts{Name: "ok"})
	if err != nil || body["name"] != "ok" {
		t.Errorf("body = %v err=%v", body, err)
	}
}

// TestDoBodyReadError forces an io.Reader that returns a non-EOF error.
type errReader struct{}

func (errReader) Read(p []byte) (int, error) { return 0, errors.New("boom") }

func TestDoBodyReadError(t *testing.T) {
	c := New(WithBaseURL("http://example"))
	err := c.doWithHeaders(context.Background(), "POST", "/x", errReader{}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "reading request body") {
		t.Errorf("expected reading body error, got %v", err)
	}
}

// Smoke-test version.go references compile.
func TestUserAgentString(t *testing.T) {
	if userAgentString() == "" {
		t.Error("userAgentString() empty")
	}
	// Force test environment shape.
	if os.Getenv("CI") == "always-something-that-is-not-set" {
		t.Skip("unreachable")
	}
	// Bytes round-trip sanity for body buffering.
	var buf bytes.Buffer
	buf.WriteString("x")
}

// TestAPIError_RawOnly hits the path where Code is empty but raw is present.
func TestAPIError_RawOnly(t *testing.T) {
	e := &APIError{StatusCode: 500, raw: "some body"}
	if !strings.Contains(e.Error(), "some body") {
		t.Errorf("expected raw in error, got %q", e.Error())
	}
}
