package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/InstaNode-dev/sdk-go/instant"
)

type mockOpts struct {
	dbStatus           int
	cacheStatus        int
	queueStatus        int
	dbTier             string
	cacheWithKeyPrefix bool
}

func newServer(t *testing.T, opts mockOpts) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/db/new":
			if opts.dbStatus != 0 {
				w.WriteHeader(opts.dbStatus)
				_, _ = io.WriteString(w, `{"error":"forced","message":"db forced error"}`)
				return
			}
			tier := opts.dbTier
			if tier == "" {
				tier = "anonymous"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":             true,
				"token":          "tok-db",
				"connection_url": "postgres://u:p@h/db",
				"tier":           tier,
				"limits":         map[string]any{"storage_mb": 10, "connections": 2},
			})
		case "/cache/new":
			if opts.cacheStatus != 0 {
				w.WriteHeader(opts.cacheStatus)
				_, _ = io.WriteString(w, `{"error":"forced","message":"cache forced error"}`)
				return
			}
			payload := map[string]any{
				"ok":             true,
				"token":          "tok-cache",
				"connection_url": "redis://h:6379",
				"tier":           "anonymous",
				"limits":         map[string]any{"memory_mb": 5},
			}
			if opts.cacheWithKeyPrefix {
				payload["key_prefix"] = "tenant42:"
			}
			_ = json.NewEncoder(w).Encode(payload)
		case "/queue/new":
			if opts.queueStatus != 0 {
				w.WriteHeader(opts.queueStatus)
				_, _ = io.WriteString(w, `{"error":"forced","message":"queue forced error"}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":             true,
				"token":          "tok-queue",
				"connection_url": "nats://h:4222",
				"tier":           "anonymous",
				"limits":         map[string]any{"storage_mb": 1024},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRun_HappyPath_AnonymousUpsell(t *testing.T) {
	srv := newServer(t, mockOpts{cacheWithKeyPrefix: true})
	client := instant.New(instant.WithBaseURL(srv.URL))

	var out strings.Builder
	if err := Run(context.Background(), client, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	s := out.String()
	for _, want := range []string{
		"POSTGRES",
		"REDIS",
		"NATS QUEUE",
		"prefix:  tenant42:",
		"postgres://u:p@h/db",
		"redis://h:6379",
		"nats://h:4222",
		// Anonymous-tier upsell line because dbTier defaulted to "anonymous":
		"24h TTL",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in output\n--- got ---\n%s", want, s)
		}
	}
}

func TestRun_HappyPath_HobbyTierNoUpsell(t *testing.T) {
	srv := newServer(t, mockOpts{dbTier: "hobby"})
	client := instant.New(instant.WithBaseURL(srv.URL))

	var out strings.Builder
	if err := Run(context.Background(), client, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(out.String(), "24h TTL") {
		t.Errorf("hobby tier should not print 24h TTL upsell:\n%s", out.String())
	}
}

func TestRun_ErrorBranch_PrintsAllErrorsAndReturns(t *testing.T) {
	// Make all three fail; verify the function joins the errors and returns
	// a non-nil error and that every per-resource error line shows up in out.
	srv := newServer(t, mockOpts{
		dbStatus:    http.StatusInternalServerError,
		cacheStatus: http.StatusInternalServerError,
		queueStatus: http.StatusInternalServerError,
	})
	client := instant.New(instant.WithBaseURL(srv.URL))

	var out strings.Builder
	err := Run(context.Background(), client, &out)
	if err == nil {
		t.Fatal("expected error when all three fail")
	}
	if !strings.Contains(err.Error(), "3 provisioning error") {
		t.Errorf("expected '3 provisioning error', got: %v", err)
	}
	s := out.String()
	// Each named source should appear in the output.
	for _, name := range []string{"postgres:", "redis:", "queue:"} {
		if !strings.Contains(s, name) {
			t.Errorf("missing %q in error output\n%s", name, s)
		}
	}
}

// TestMain_CallsRunSuccessfully invokes main() against a mocked server via
// INSTANT_API_URL — exercising the main() entry point itself.
func TestMain_CallsRunSuccessfully(t *testing.T) {
	srv := newServer(t, mockOpts{})
	t.Setenv("INSTANT_API_KEY", "")
	t.Setenv("INSTANT_API_URL", srv.URL)
	// Redirect stdout so we don't pollute the test log.
	r, w, _ := os.Pipe()
	origStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, r)
		close(done)
	}()
	// Should not panic / not call log.Fatal because mocked server returns 2xx.
	main()
	w.Close()
	<-done
}

func TestRun_ErrorBranch_SingleFailureReturnsOne(t *testing.T) {
	// Only the queue fails — verify partial failure still returns an error
	// (the rest succeeded but the function reports the failure rather than
	// silently swallowing it).
	srv := newServer(t, mockOpts{queueStatus: http.StatusServiceUnavailable})
	client := instant.New(instant.WithBaseURL(srv.URL))

	var out strings.Builder
	err := Run(context.Background(), client, &out)
	if err == nil {
		t.Fatal("expected error when queue fails")
	}
	if !strings.Contains(err.Error(), "1 provisioning error") {
		t.Errorf("expected '1 provisioning error', got: %v", err)
	}
	if !strings.Contains(out.String(), "queue:") {
		t.Errorf("expected queue: prefix in output\n%s", out.String())
	}
}
