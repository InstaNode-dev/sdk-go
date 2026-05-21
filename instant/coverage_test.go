package instant

// coverage_test.go pins the error-path branches that the original baseline
// tests didn't reach. Each test is a focused httptest server that surfaces
// the exact wire condition (4xx/5xx, empty fields, network error,
// over-sized response bodies) the SDK must propagate.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// errServer returns an httptest server that always serves the given status +
// body. Each test gets a fresh one so retries and parallel runs don't bleed.
func errServer(status int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
}

// Each provisioning helper has 4 error branches:
//   1. post returns non-nil (HTTP 4xx/5xx surfaced as APIError)
//   2. result.Token == ""        (server gave us an unusable response)
//   3. result.ConnectionURL == ""
//   4. result.Note != ""         (info-log branch)

// ─── ProvisionDatabase ────────────────────────────────────────────────────────

func TestProvisionDatabase_APIError(t *testing.T) {
	srv := errServer(http.StatusForbidden, `{"error":"forbidden"}`)
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).ProvisionDatabase(
		context.Background(), &ProvisionOpts{Name: "ok"})
	if err == nil || !strings.Contains(err.Error(), "ProvisionDatabase") {
		t.Errorf("expected ProvisionDatabase-prefixed error, got %v", err)
	}
}

// ─── ProvisionCache ───────────────────────────────────────────────────────────

func TestProvisionCache_APIError(t *testing.T) {
	srv := errServer(http.StatusUnauthorized, `{"error":"unauthorized"}`)
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).ProvisionCache(
		context.Background(), &ProvisionOpts{Name: "ok"})
	if err == nil || !strings.Contains(err.Error(), "ProvisionCache") {
		t.Errorf("expected ProvisionCache-prefixed error, got %v", err)
	}
}

func TestProvisionCache_EmptyTokenErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "token": "", "connection_url": "x"})
	}))
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).ProvisionCache(
		context.Background(), &ProvisionOpts{Name: "ok"})
	if err == nil || !strings.Contains(err.Error(), "empty token") {
		t.Errorf("expected empty-token error, got %v", err)
	}
}

func TestProvisionCache_EmptyConnectionURLErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "token": "t", "connection_url": ""})
	}))
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).ProvisionCache(
		context.Background(), &ProvisionOpts{Name: "ok"})
	if err == nil || !strings.Contains(err.Error(), "empty connection_url") {
		t.Errorf("expected empty connection_url error, got %v", err)
	}
}

func TestProvisionCache_NoteLogsInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "token": "tok", "connection_url": "redis://x",
			"tier": "anonymous", "note": "upgrade-cta",
		})
	}))
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).ProvisionCache(
		context.Background(), &ProvisionOpts{Name: "ok"})
	if err != nil {
		t.Fatalf("ProvisionCache: %v", err)
	}
}

// ─── ProvisionMongoDB ─────────────────────────────────────────────────────────

func TestProvisionMongoDB_APIError(t *testing.T) {
	srv := errServer(http.StatusForbidden, `{}`)
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).ProvisionMongoDB(
		context.Background(), &ProvisionOpts{Name: "ok"})
	if err == nil || !strings.Contains(err.Error(), "ProvisionMongoDB") {
		t.Errorf("expected ProvisionMongoDB error, got %v", err)
	}
}

func TestProvisionMongoDB_EmptyToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "token": "", "connection_url": "x"})
	}))
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).ProvisionMongoDB(
		context.Background(), &ProvisionOpts{Name: "ok"})
	if err == nil || !strings.Contains(err.Error(), "empty token") {
		t.Errorf("expected empty-token error, got %v", err)
	}
}

func TestProvisionMongoDB_EmptyConnectionURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "token": "t", "connection_url": ""})
	}))
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).ProvisionMongoDB(
		context.Background(), &ProvisionOpts{Name: "ok"})
	if err == nil || !strings.Contains(err.Error(), "empty connection_url") {
		t.Errorf("expected empty connection_url error, got %v", err)
	}
}

func TestProvisionMongoDB_WithNote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "token": "tok", "connection_url": "mongodb://x",
			"tier": "anonymous", "note": "n",
		})
	}))
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).ProvisionMongoDB(
		context.Background(), &ProvisionOpts{Name: "ok"})
	if err != nil {
		t.Fatalf("ProvisionMongoDB: %v", err)
	}
}

// ─── ProvisionQueue ───────────────────────────────────────────────────────────

func TestProvisionQueue_APIError(t *testing.T) {
	srv := errServer(http.StatusForbidden, `{}`)
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).ProvisionQueue(
		context.Background(), &ProvisionOpts{Name: "ok"})
	if err == nil || !strings.Contains(err.Error(), "ProvisionQueue") {
		t.Errorf("expected ProvisionQueue error, got %v", err)
	}
}

func TestProvisionQueue_EmptyToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "token": "", "connection_url": "x"})
	}))
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).ProvisionQueue(
		context.Background(), &ProvisionOpts{Name: "ok"})
	if err == nil || !strings.Contains(err.Error(), "empty token") {
		t.Errorf("expected empty-token error, got %v", err)
	}
}

func TestProvisionQueue_EmptyConnectionURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "token": "t", "connection_url": ""})
	}))
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).ProvisionQueue(
		context.Background(), &ProvisionOpts{Name: "ok"})
	if err == nil || !strings.Contains(err.Error(), "empty connection_url") {
		t.Errorf("expected empty connection_url error, got %v", err)
	}
}

func TestProvisionQueue_WithNote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "token": "tok", "connection_url": "nats://x",
			"tier": "anonymous", "note": "queue note",
		})
	}))
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).ProvisionQueue(
		context.Background(), &ProvisionOpts{Name: "ok"})
	if err != nil {
		t.Fatalf("ProvisionQueue: %v", err)
	}
}

// ─── ProvisionWebhook ─────────────────────────────────────────────────────────

func TestProvisionWebhook_APIError(t *testing.T) {
	srv := errServer(http.StatusForbidden, `{}`)
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).ProvisionWebhook(
		context.Background(), &ProvisionOpts{Name: "ok"})
	if err == nil || !strings.Contains(err.Error(), "ProvisionWebhook") {
		t.Errorf("expected ProvisionWebhook error, got %v", err)
	}
}

func TestProvisionWebhook_EmptyReceiveURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "token": "t", "receive_url": ""})
	}))
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).ProvisionWebhook(
		context.Background(), &ProvisionOpts{Name: "ok"})
	if err == nil || !strings.Contains(err.Error(), "empty receive_url") {
		t.Errorf("expected empty receive_url error, got %v", err)
	}
}

// ─── ProvisionStorage ─────────────────────────────────────────────────────────

func TestProvisionStorage_APIError(t *testing.T) {
	srv := errServer(http.StatusForbidden, `{}`)
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).ProvisionStorage(
		context.Background(), &ProvisionOpts{Name: "ok"})
	if err == nil || !strings.Contains(err.Error(), "ProvisionStorage") {
		t.Errorf("expected ProvisionStorage error, got %v", err)
	}
}

func TestProvisionStorage_EmptyToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "token": "", "connection_url": "x"})
	}))
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).ProvisionStorage(
		context.Background(), &ProvisionOpts{Name: "ok"})
	if err == nil || !strings.Contains(err.Error(), "empty token") {
		t.Errorf("expected empty-token error, got %v", err)
	}
}

func TestProvisionStorage_WithNote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "token": "tok", "connection_url": "https://x/b/", "tier": "anonymous",
			"note": "upgrade",
		})
	}))
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).ProvisionStorage(
		context.Background(), &ProvisionOpts{Name: "ok"})
	if err != nil {
		t.Fatalf("ProvisionStorage: %v", err)
	}
}

// ─── ProvisionDatabase: note branch covered already in client_test.go but
// keep an additional regression for the rare connection_url-empty branch ───

func TestProvisionDatabase_WithNote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "token": "tok", "connection_url": "postgres://h",
			"tier": "anonymous", "note": "upgrade now",
		})
	}))
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).ProvisionDatabase(
		context.Background(), &ProvisionOpts{Name: "ok"})
	if err != nil {
		t.Fatalf("ProvisionDatabase: %v", err)
	}
}

// ─── Claim: APIError branch ───────────────────────────────────────────────────

func TestClaim_APIErrorWrapping(t *testing.T) {
	srv := errServer(http.StatusConflict, `{"error":"already_claimed"}`)
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).Claim(
		context.Background(), ClaimOpts{JWT: "ey", Email: "a@b"})
	if err == nil || !strings.Contains(err.Error(), "Claim") {
		t.Errorf("expected Claim-prefixed error, got %v", err)
	}
	if !IsConflict(err) {
		t.Error("IsConflict should match a 409")
	}
}

func TestClaimTokens_APIErrorWrapping(t *testing.T) {
	srv := errServer(http.StatusConflict, `{"error":"already_claimed"}`)
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).ClaimTokens(
		context.Background(), "sk-1", []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "ClaimTokens") {
		t.Errorf("expected ClaimTokens-prefixed error, got %v", err)
	}
}

// ─── resources.go: error-wrap branches ────────────────────────────────────────

func TestListResources_APIError(t *testing.T) {
	srv := errServer(http.StatusInternalServerError, `{"error":"boom"}`)
	defer srv.Close()
	// /!\ retry-on-5xx means we'd see 2 hits; the test only cares that the
	// final error wraps ListResources.
	_, err := New(WithBaseURL(srv.URL)).ListResources(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ListResources") {
		t.Errorf("expected ListResources-prefixed error, got %v", err)
	}
}

func TestGetResource_APIError(t *testing.T) {
	srv := errServer(http.StatusNotFound, `{"error":"not_found"}`)
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).GetResource(context.Background(), "missing")
	if err == nil || !strings.Contains(err.Error(), "GetResource") {
		t.Errorf("expected GetResource-prefixed error, got %v", err)
	}
}

func TestDeleteResource_APIError(t *testing.T) {
	srv := errServer(http.StatusForbidden, `{"error":"forbidden"}`)
	defer srv.Close()
	err := New(WithBaseURL(srv.URL)).DeleteResource(context.Background(), "tok")
	if err == nil || !strings.Contains(err.Error(), "DeleteResource") {
		t.Errorf("expected DeleteResource-prefixed error, got %v", err)
	}
}

func TestRotateCredentials_APIError(t *testing.T) {
	srv := errServer(http.StatusForbidden, `{"error":"forbidden"}`)
	defer srv.Close()
	_, err := New(WithBaseURL(srv.URL)).RotateCredentials(context.Background(), "tok")
	if err == nil || !strings.Contains(err.Error(), "RotateCredentials") {
		t.Errorf("expected RotateCredentials-prefixed error, got %v", err)
	}
}

// ─── deploy.go: every multipart writer + http path ───────────────────────────

// erroringReader returns the same error on every Read. Used to drive the
// io.Copy(tarball) error branch inside Deploy.
type erroringReader struct{}

func (erroringReader) Read(p []byte) (int, error) { return 0, errors.New("synthetic read failure") }

func TestDeploy_TarballReadError(t *testing.T) {
	client := New(WithBaseURL("http://unused"))
	_, err := client.Deploy(context.Background(), DeployOpts{Tarball: erroringReader{}})
	if err == nil || !strings.Contains(err.Error(), "reading tarball") {
		t.Errorf("expected 'reading tarball' error, got %v", err)
	}
}

func TestDeploy_TransportError(t *testing.T) {
	// closed socket → http.Client returns a "connection refused" error.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("could not listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // immediately unblock the port — connect attempts now refuse.
	client := New(WithBaseURL("http://" + addr))
	_, err = client.Deploy(context.Background(), DeployOpts{
		Tarball: strings.NewReader("tar"),
	})
	if err == nil || !strings.Contains(err.Error(), "deploy request failed") {
		t.Errorf("expected deploy request failed, got %v", err)
	}
}

func TestDeploy_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		_, _ = io.WriteString(w, `not-json-{{`)
	}))
	defer srv.Close()
	client := New(WithBaseURL(srv.URL))
	_, err := client.Deploy(context.Background(), DeployOpts{
		Tarball: strings.NewReader("tar"),
	})
	if err == nil || !strings.Contains(err.Error(), "decoding deploy response") {
		t.Errorf("expected decode error, got %v", err)
	}
}

func TestDeploy_BuildRequestError(t *testing.T) {
	// An invalid URL surfaces via http.NewRequestWithContext.
	client := New(WithBaseURL("://invalid"))
	_, err := client.Deploy(context.Background(), DeployOpts{
		Tarball: strings.NewReader("tar"),
	})
	if err == nil || !strings.Contains(err.Error(), "building deploy request") {
		t.Errorf("expected build-request error, got %v", err)
	}
}

// ─── StreamDeploymentLogs: error branches ─────────────────────────────────────

func TestStreamDeploymentLogs_RejectsNilWriter(t *testing.T) {
	client := New(WithBaseURL("http://unused"))
	err := client.StreamDeploymentLogs(context.Background(), "id", nil)
	if err == nil || !strings.Contains(err.Error(), "non-nil writer") {
		t.Errorf("expected non-nil writer error, got %v", err)
	}
}

func TestStreamDeploymentLogs_BuildRequestError(t *testing.T) {
	client := New(WithBaseURL("://invalid"))
	err := client.StreamDeploymentLogs(context.Background(), "id", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "building stream request") {
		t.Errorf("expected build error, got %v", err)
	}
}

func TestStreamDeploymentLogs_TransportError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("could not listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	client := New(WithBaseURL("http://" + addr))
	err = client.StreamDeploymentLogs(context.Background(), "id", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "stream request failed") {
		t.Errorf("expected stream-request-failed, got %v", err)
	}
}

// errWriter returns an error on every Write so we can cover the
// "writing log line" branch in StreamDeploymentLogs.
type errWriter struct{}

func (errWriter) Write(p []byte) (int, error) { return 0, errors.New("synthetic write failure") }

func TestStreamDeploymentLogs_WriterError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "data: line1\n\n")
	}))
	defer srv.Close()
	client := New(WithBaseURL(srv.URL))
	err := client.StreamDeploymentLogs(context.Background(), "appid", errWriter{})
	if err == nil || !strings.Contains(err.Error(), "writing log line") {
		t.Errorf("expected 'writing log line' error, got %v", err)
	}
}

// ─── client.go: leftover branches ─────────────────────────────────────────────

// TestRetryThenTransportError forces TWO transport errors back-to-back so the
// second-attempt error path is exercised (line 260: return lastErr).
func TestDoTransportErrorThenError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	c := New(WithBaseURL("http://" + addr))
	var out map[string]any
	err = c.get(context.Background(), "/x", &out)
	if err == nil {
		t.Fatal("expected error on both attempts")
	}
}

// TestRetryThenStillFails — first attempt returns 500, retry also returns 500;
// this exercises the "5xx + attempt==1" path (since attempt index goes 0,1
// and the retry happens only for attempt==0). After the retry the second 5xx
// must fall through to the apiErr return path.
func TestDoRetryStillFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":"bg"}`)
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	var out map[string]any
	err := c.get(context.Background(), "/x", &out)
	if err == nil {
		t.Fatal("expected error after retry")
	}
}

// TestDoCtxCancelDuring5xxRetry covers the "5xx + ctx cancelled during
// backoff sleep" branch in client.go.
func TestDoCtxCancelDuring5xxRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after 50ms — the SDK's between-retry sleep is 500ms.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	var out map[string]any
	err := c.get(ctx, "/x", &out)
	if err == nil {
		t.Fatal("expected ctx.Err()")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestDoCtxCancelDuringTransportErrorRetry covers the "transport error + ctx
// cancelled during 300ms backoff" branch.
func TestDoCtxCancelDuringTransportErrorRetry(t *testing.T) {
	// listener that accepts then closes — first request will fail mid-flight,
	// triggering the retry path. The listener stays open so the cancel races
	// the 300ms backoff sleep.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("listen: %v", err)
	}
	addr := ln.Addr().String()
	// Accept once and immediately close the conn to surface a transport error.
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			conn.Close()
		}
	}()
	defer ln.Close()
	c := New(WithBaseURL("http://" + addr))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	var out map[string]any
	err = c.get(ctx, "/x", &out)
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestDoDecodeError covers the JSON decode failure after a 2xx.
func TestDoDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `not-json-{{`)
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	var out map[string]any
	err := c.get(context.Background(), "/x", &out)
	if err == nil || !strings.Contains(err.Error(), "decoding response") {
		t.Errorf("expected decoding response error, got %v", err)
	}
}

// TestDoBuildRequestError forces http.NewRequestWithContext to fail by passing
// an invalid method character.
func TestDoBuildRequestError(t *testing.T) {
	c := New(WithBaseURL("http://example"))
	// Invalid method (contains a space) is rejected by http.NewRequest.
	err := c.doWithHeaders(context.Background(), "BAD METHOD", "/x", nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "building request") {
		t.Errorf("expected building-request error, got %v", err)
	}
}

