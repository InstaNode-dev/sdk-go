package instant_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"instant.dev/sdk/go/instant"
)

// markerTransport is an http.RoundTripper that stamps a marker header on every
// outbound request and records that it was invoked. Used to prove the SDK's
// New() preserves the caller's Transport instead of silently discarding it.
type markerTransport struct {
	marker string
	base   http.RoundTripper
	hits   int32
}

func (m *markerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	atomic.AddInt32(&m.hits, 1)
	req = req.Clone(req.Context())
	req.Header.Set("X-Caller-Marker", m.marker)
	base := m.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

// TestWithHTTPClient_PreservesTransport pins B17-P0: the caller's Transport
// MUST be chained underneath the SDK's auth transport so OTel injection,
// custom TLS, proxy injection, and other RoundTripper wrappers keep working.
//
// Before the fix, WithHTTPClient kept only the caller's Timeout and discarded
// the rest of the *http.Client — including Transport. The server below would
// have observed no X-Caller-Marker header and the test would fail.
func TestWithHTTPClient_PreservesTransport(t *testing.T) {
	var sawMarker, sawUserAgent, sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawMarker = r.Header.Get("X-Caller-Marker")
		sawUserAgent = r.Header.Get("User-Agent")
		sawAuth = r.Header.Get("Authorization")
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":    true,
			"items": []any{},
			"total": 0,
		})
	}))
	t.Cleanup(srv.Close)

	caller := &markerTransport{marker: "trace-id-abc"}
	hc := &http.Client{
		Timeout:   17 * time.Second,
		Transport: caller,
	}

	client := instant.New(
		instant.WithBaseURL(srv.URL),
		instant.WithAPIKey("sk-test"),
		instant.WithHTTPClient(hc),
	)

	if _, err := client.ListResources(context.Background()); err != nil {
		t.Fatalf("ListResources: %v", err)
	}

	if got := atomic.LoadInt32(&caller.hits); got != 1 {
		t.Fatalf("caller transport invocations = %d, want 1 (caller Transport was discarded)", got)
	}
	if sawMarker != "trace-id-abc" {
		t.Fatalf("server saw X-Caller-Marker = %q, want %q (caller Transport was discarded)", sawMarker, "trace-id-abc")
	}
	if !strings.HasPrefix(sawUserAgent, "instant-go-sdk/") {
		t.Errorf("User-Agent = %q, want prefix instant-go-sdk/ (auth transport not layered)", sawUserAgent)
	}
	if sawAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want Bearer sk-test (auth transport not layered)", sawAuth)
	}
}

// TestWithHTTPClient_PreservesTimeout pins that the caller's Timeout still
// survives the transport-chaining rewrite. Proven indirectly by the slow
// server hitting the caller's tiny timeout — the request must error with a
// timeout, NOT succeed.
func TestWithHTTPClient_PreservesTimeout(t *testing.T) {
	// Server stalls for longer than the caller's Timeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": []any{}, "total": 0})
	}))
	t.Cleanup(srv.Close)

	hc := &http.Client{Timeout: 30 * time.Millisecond}
	client := instant.New(instant.WithBaseURL(srv.URL), instant.WithHTTPClient(hc))

	_, err := client.ListResources(context.Background())
	if err == nil {
		t.Fatalf("ListResources should have timed out — caller Timeout was discarded")
	}
}

// TestWithHTTPClient_NilIsNoop pins that passing a nil client doesn't blow up
// later when the SDK tries to wrap it. The default 30s timeout should remain,
// proven indirectly by a normal request succeeding.
func TestWithHTTPClient_NilIsNoop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": []any{}, "total": 0})
	}))
	t.Cleanup(srv.Close)

	client := instant.New(instant.WithBaseURL(srv.URL), instant.WithHTTPClient(nil))
	if _, err := client.ListResources(context.Background()); err != nil {
		t.Errorf("nil hc should be a no-op, ListResources errored: %v", err)
	}
}

// TestWithHTTPClient_NestedTransports pins that nested Transport wrappers
// (an OTel-style RoundTripper that itself wraps another RoundTripper) chain
// correctly. Each layer should see the request before the wire.
func TestWithHTTPClient_NestedTransports(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":    true,
			"items": []any{},
			"total": 0,
		})
	}))
	t.Cleanup(srv.Close)

	inner := &markerTransport{marker: "inner"}
	outer := &markerTransport{marker: "outer", base: inner}
	hc := &http.Client{Transport: outer}

	client := instant.New(instant.WithBaseURL(srv.URL), instant.WithHTTPClient(hc))
	if _, err := client.ListResources(context.Background()); err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if got := atomic.LoadInt32(&outer.hits); got != 1 {
		t.Fatalf("outer transport hits = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&inner.hits); got != 1 {
		t.Fatalf("inner transport hits = %d, want 1 (chain broken)", got)
	}
}

// TestSDKVersionInUserAgent pins the User-Agent format and that the version
// comes from the SDKVersion constant — the source of truth.
func TestSDKVersionInUserAgent(t *testing.T) {
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":    true,
			"items": []any{},
			"total": 0,
		})
	}))
	t.Cleanup(srv.Close)

	client := instant.New(instant.WithBaseURL(srv.URL))
	if _, err := client.ListResources(context.Background()); err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	want := "instant-go-sdk/" + instant.SDKVersion
	if ua != want {
		t.Errorf("User-Agent = %q, want %q", ua, want)
	}
}
