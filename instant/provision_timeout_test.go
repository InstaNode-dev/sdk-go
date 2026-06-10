package instant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestProvisioningTimeoutDefaults pins the read-vs-provisioning timeout split.
//
// Synchronous provisioning (POST /db/new etc.) can exceed the 30 s read-path
// budget under prod hot-pool contention; a client-side give-up at 30 s strands
// the resource behind a 60 s in-flight idempotency marker, so the retry hits
// 409 idempotency_key_in_progress. The fix gives provisioning a 120 s deadline.
// This test fails if either constant drifts or the wiring is dropped.
func TestProvisioningTimeoutDefaults(t *testing.T) {
	c := New()

	if c.httpClient.Timeout != defaultTimeout {
		t.Errorf("read-path Timeout = %v, want %v (reads must stay short)", c.httpClient.Timeout, defaultTimeout)
	}
	if c.provisionTimeout != defaultProvisioningTimeout {
		t.Errorf("provisionTimeout = %v, want %v (provisioning must outlive reads)", c.provisionTimeout, defaultProvisioningTimeout)
	}
	if c.provisionTimeout <= c.httpClient.Timeout {
		t.Errorf("provisionTimeout (%v) must be strictly longer than the read Timeout (%v)", c.provisionTimeout, c.httpClient.Timeout)
	}
	if defaultProvisioningTimeout != 120*time.Second {
		t.Errorf("defaultProvisioningTimeout = %v, want 120s (the value verified to fix the prod 409 conflict)", defaultProvisioningTimeout)
	}
	// The provisioning client must carry NO client-wide Timeout cap — the
	// deadline is enforced per-request via provisionContext. A non-zero cap
	// here would silently re-impose a ceiling on slow provisions.
	if c.provisionClient.Timeout != 0 {
		t.Errorf("provisionClient.Timeout = %v, want 0 (cap must come from the request context, not the client)", c.provisionClient.Timeout)
	}
}

// TestProvisioningOutlivesReadCap is the behavioral proof: a provisioning call
// succeeds against a server that stalls *past* the read-path timeout, because
// the provisioning path runs on provisionClient (no cap) with the longer
// provisioning deadline — while a read against the same slow server times out.
//
// Scaled down from the real 30 s / 120 s split (read = 40 ms, provision = 2 s,
// server stalls 250 ms) so it runs deterministically in milliseconds; the
// mechanism under test is identical.
func TestProvisioningOutlivesReadCap(t *testing.T) {
	const serverStall = 250 * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(serverStall)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "token": "tok-db", "connection_url": "postgres://h",
		})
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	// Reproduce the prod split at millisecond scale: reads would give up well
	// before the server responds; provisioning has ample headroom.
	c.httpClient.Timeout = 40 * time.Millisecond // read-path cap (stand-in for 30 s)
	c.provisionTimeout = 2 * time.Second         // provisioning deadline (stand-in for 120 s)

	// Read path: must trip the short read-path cap.
	if _, err := c.ListResources(context.Background()); err == nil {
		t.Fatal("read call should have timed out at the short read-path cap, but succeeded")
	}

	// Provisioning path: must outlive the read-path cap and succeed.
	r, err := c.ProvisionDatabase(context.Background(), &ProvisionOpts{Name: "db1"})
	if err != nil {
		t.Fatalf("ProvisionDatabase should have outlived the read-path cap, got error: %v", err)
	}
	if r.Token != "tok-db" {
		t.Errorf("Token = %q, want tok-db", r.Token)
	}
}

// TestProvisioningHonorsTighterCallerDeadline proves provisionContext never
// *extends* a deadline the caller already set: a caller passing a 30 ms context
// to a provisioning call against a 250 ms server must still time out, even
// though the provisioning default (120 s) is far longer.
func TestProvisioningHonorsTighterCallerDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(250 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "token": "t", "connection_url": "x"})
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	if _, err := c.ProvisionDatabase(ctx, &ProvisionOpts{Name: "db1"}); err == nil {
		t.Fatal("provisioning must honour a tighter caller deadline and time out, but it succeeded")
	}
}

// TestWithTimeoutGovernsProvisioning proves WithTimeout overrides BOTH the read
// and provisioning defaults: a 40 ms explicit budget makes a provisioning call
// against a 250 ms server time out, confirming the option remains the escape
// hatch for callers who want a single, smaller budget.
func TestWithTimeoutGovernsProvisioning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(250 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "token": "t", "connection_url": "x"})
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL), WithTimeout(40*time.Millisecond))
	if c.provisionTimeout != 40*time.Millisecond {
		t.Fatalf("WithTimeout did not govern provisionTimeout: got %v", c.provisionTimeout)
	}

	if _, err := c.ProvisionDatabase(context.Background(), &ProvisionOpts{Name: "db1"}); err == nil {
		t.Fatal("provisioning should respect the explicit WithTimeout budget and time out, but it succeeded")
	}
}

// TestWithHTTPClientTimeoutGovernsProvisioning proves a caller-supplied
// http.Client whose Timeout is explicitly set (via WithHTTPClient) governs the
// provisioning deadline too — the SDK must not silently widen the caller's
// deliberate budget to 120 s.
func TestWithHTTPClientTimeoutGovernsProvisioning(t *testing.T) {
	c := New(WithHTTPClient(&http.Client{Timeout: 7 * time.Second}))
	if c.provisionTimeout != 7*time.Second {
		t.Errorf("WithHTTPClient Timeout should govern provisionTimeout, got %v want 7s", c.provisionTimeout)
	}
}

// TestProvisionContextOpenEndedGetsBudget covers the open-ended-context branch
// of provisionContext directly: a context with no deadline receives the full
// provisioning budget.
func TestProvisionContextOpenEndedGetsBudget(t *testing.T) {
	c := New()
	pctx, cancel := c.provisionContext(context.Background())
	defer cancel()
	dl, ok := pctx.Deadline()
	if !ok {
		t.Fatal("provisionContext should add a deadline to an open-ended context")
	}
	remaining := time.Until(dl)
	// Allow a little slack for scheduling; it should be close to 120 s.
	if remaining < defaultProvisioningTimeout-time.Second || remaining > defaultProvisioningTimeout+time.Second {
		t.Errorf("provisioning deadline = %v out, want ~%v", remaining, defaultProvisioningTimeout)
	}
}

// TestProvisionContextZeroTimeoutFallsBack covers the defensive d<=0 guard:
// a Client whose provisionTimeout is somehow zero (e.g. a zero-value Client)
// still gets the default provisioning budget rather than an instantly-expired
// deadline.
func TestProvisionContextZeroTimeoutFallsBack(t *testing.T) {
	c := New()
	c.provisionTimeout = 0 // force the defensive branch

	pctx, cancel := c.provisionContext(context.Background())
	defer cancel()
	dl, ok := pctx.Deadline()
	if !ok {
		t.Fatal("provisionContext should still add a deadline when provisionTimeout is 0")
	}
	if remaining := time.Until(dl); remaining < defaultProvisioningTimeout-time.Second {
		t.Errorf("zero provisionTimeout should fall back to ~%v, got %v", defaultProvisioningTimeout, remaining)
	}
}

// TestProvisionJSONMarshalError covers the marshal-error branch of
// provisionJSONWithHeaders: an unmarshalable body surfaces the marshalling
// error before any network request, mirroring the read-path postJSON behaviour.
func TestProvisionJSONMarshalError(t *testing.T) {
	c := New(WithBaseURL("http://unused"))
	// channels are not JSON-marshalable
	err := c.provisionJSONWithHeaders(context.Background(), "/db/new", make(chan int), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "marshalling") {
		t.Errorf("expected marshalling error, got %v", err)
	}
}
