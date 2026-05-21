package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/InstaNode-dev/sdk-go/instant"
)

// newTestServer returns a mock api server that serves the three provisioning
// endpoints the bootstrap example calls. Each endpoint returns a deterministic
// payload so the test asserts both the wire shape and the writeback content.
func newTestServer(t *testing.T, opts mockServerOpts) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/db/new":
			if opts.dbStatus != 0 {
				w.WriteHeader(opts.dbStatus)
				_, _ = io.WriteString(w, `{"error":"forced","message":"forced db error"}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":             true,
				"token":          "tok-db",
				"connection_url": "postgres://u:p@h/db",
				"tier":           "anonymous",
				"note":           "upgrade for permanence",
				"limits":         map[string]any{"storage_mb": 10, "connections": 2},
			})
		case "/cache/new":
			if opts.cacheStatus != 0 {
				w.WriteHeader(opts.cacheStatus)
				_, _ = io.WriteString(w, `{"error":"forced","message":"forced cache error"}`)
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
				_, _ = io.WriteString(w, `{"error":"forced","message":"forced queue error"}`)
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

type mockServerOpts struct {
	dbStatus           int
	cacheStatus        int
	queueStatus        int
	cacheWithKeyPrefix bool
}

func TestRun_HappyPath_WritesAllThreeKeys(t *testing.T) {
	srv := newTestServer(t, mockServerOpts{cacheWithKeyPrefix: true})
	client := instant.New(instant.WithBaseURL(srv.URL))

	tmp := t.TempDir()
	envPath := filepath.Join(tmp, ".env")

	var out strings.Builder
	if err := Run(context.Background(), client, envPath, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	s := string(got)
	for _, want := range []string{
		"DATABASE_URL=postgres://u:p@h/db",
		"REDIS_URL=redis://h:6379  # key prefix: tenant42:",
		"NATS_URL=nats://h:4222",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in .env:\n%s", want, s)
		}
	}

	// Should print the upgrade note from the db response
	if !strings.Contains(out.String(), "upgrade for permanence") {
		t.Errorf("Note not surfaced in output:\n%s", out.String())
	}
}

func TestRun_SkipsAlreadyProvisioned(t *testing.T) {
	srv := newTestServer(t, mockServerOpts{})
	client := instant.New(instant.WithBaseURL(srv.URL))

	tmp := t.TempDir()
	envPath := filepath.Join(tmp, ".env")
	// Pre-seed .env so DATABASE_URL and REDIS_URL skip — only NATS_URL is new
	if err := os.WriteFile(envPath, []byte("DATABASE_URL=postgres://existing\nREDIS_URL=redis://existing\n"), 0600); err != nil {
		t.Fatalf("seed .env: %v", err)
	}

	var out strings.Builder
	if err := Run(context.Background(), client, envPath, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(out.String(), "Postgres: already provisioned") {
		t.Errorf("expected skip-message for Postgres in output: %s", out.String())
	}
	if !strings.Contains(out.String(), "Redis:    already provisioned") {
		t.Errorf("expected skip-message for Redis in output: %s", out.String())
	}

	got, _ := os.ReadFile(envPath)
	s := string(got)
	if !strings.Contains(s, "DATABASE_URL=postgres://existing") {
		t.Errorf("seeded DATABASE_URL clobbered:\n%s", s)
	}
	if !strings.Contains(s, "NATS_URL=nats://h:4222") {
		t.Errorf("NATS_URL not added:\n%s", s)
	}
}

func TestRun_AllSkippedNoWrite(t *testing.T) {
	srv := newTestServer(t, mockServerOpts{})
	client := instant.New(instant.WithBaseURL(srv.URL))

	tmp := t.TempDir()
	envPath := filepath.Join(tmp, ".env")
	if err := os.WriteFile(envPath, []byte("DATABASE_URL=a\nREDIS_URL=b\nNATS_URL=c\n"), 0600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	origContent, _ := os.ReadFile(envPath)

	var out strings.Builder
	if err := Run(context.Background(), client, envPath, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	newContent, _ := os.ReadFile(envPath)
	if string(newContent) != string(origContent) {
		t.Errorf("file rewritten when nothing changed:\noriginal=%q\nnew=%q", origContent, newContent)
	}
	if strings.Contains(out.String(), "Wrote") {
		t.Errorf("should not say 'Wrote N values' when nothing changed:\n%s", out.String())
	}
}

func TestRun_PostgresErrorReturned(t *testing.T) {
	srv := newTestServer(t, mockServerOpts{dbStatus: http.StatusPaymentRequired})
	client := instant.New(instant.WithBaseURL(srv.URL))
	tmp := t.TempDir()
	err := Run(context.Background(), client, filepath.Join(tmp, ".env"), io.Discard)
	if err == nil {
		t.Fatal("expected postgres error")
	}
	if !strings.Contains(err.Error(), "postgres:") {
		t.Errorf("expected postgres prefix, got: %v", err)
	}
}

func TestRun_RedisErrorReturned(t *testing.T) {
	srv := newTestServer(t, mockServerOpts{cacheStatus: http.StatusInternalServerError})
	client := instant.New(instant.WithBaseURL(srv.URL))
	tmp := t.TempDir()
	envPath := filepath.Join(tmp, ".env")
	// Pre-seed DATABASE_URL so we get to Redis
	_ = os.WriteFile(envPath, []byte("DATABASE_URL=a\n"), 0600)
	err := Run(context.Background(), client, envPath, io.Discard)
	if err == nil {
		t.Fatal("expected redis error")
	}
	if !strings.Contains(err.Error(), "redis:") {
		t.Errorf("expected redis prefix, got: %v", err)
	}
}

func TestRun_QueueErrorReturned(t *testing.T) {
	srv := newTestServer(t, mockServerOpts{queueStatus: http.StatusBadRequest})
	client := instant.New(instant.WithBaseURL(srv.URL))
	tmp := t.TempDir()
	envPath := filepath.Join(tmp, ".env")
	_ = os.WriteFile(envPath, []byte("DATABASE_URL=a\nREDIS_URL=b\n"), 0600)
	err := Run(context.Background(), client, envPath, io.Discard)
	if err == nil {
		t.Fatal("expected queue error")
	}
	if !strings.Contains(err.Error(), "nats:") {
		t.Errorf("expected nats prefix, got: %v", err)
	}
}

// TestRun_WriteFails covers the writeDotEnv error branch — pointing path at
// a directory makes os.WriteFile fail.
func TestRun_WriteFails(t *testing.T) {
	srv := newTestServer(t, mockServerOpts{})
	client := instant.New(instant.WithBaseURL(srv.URL))
	tmp := t.TempDir()
	// Use the temp dir itself as the path — WriteFile will return EISDIR.
	err := Run(context.Background(), client, tmp, io.Discard)
	if err == nil {
		t.Fatal("expected write error")
	}
	if !strings.Contains(err.Error(), "write .env") {
		t.Errorf("expected write .env prefix, got: %v", err)
	}
}

// TestLoadDotEnv_SkipsCommentsAndBlanks covers the comment + blank-line
// branches in loadDotEnv.
func TestLoadDotEnv_SkipsCommentsAndBlanks(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, ".env")
	body := "# header comment\n\n  \nA=1\nB = 2\n# trailing comment\nC=\nMALFORMED_NO_EQ\n"
	_ = os.WriteFile(p, []byte(body), 0600)
	m := loadDotEnv(p)
	if m["A"] != "1" {
		t.Errorf("A = %q", m["A"])
	}
	if m["B"] != "2" {
		t.Errorf("B = %q", m["B"])
	}
	if _, ok := m["MALFORMED_NO_EQ"]; ok {
		t.Errorf("malformed line should not be parsed")
	}
	// Missing file returns empty map, not error.
	missing := loadDotEnv(filepath.Join(tmp, "does-not-exist"))
	if len(missing) != 0 {
		t.Errorf("missing file should yield empty map, got %v", missing)
	}
}

// TestWriteDotEnv_AppendsExtraKeys exercises the "key not in order" branch.
func TestWriteDotEnv_AppendsExtraKeys(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, ".env")
	if err := writeDotEnv(p, map[string]string{"EXTRA": "x"}, map[string]string{"DATABASE_URL": "y"}); err != nil {
		t.Fatalf("writeDotEnv: %v", err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	if !strings.Contains(s, "EXTRA=x") {
		t.Errorf("extra key missing: %s", s)
	}
	if !strings.Contains(s, "DATABASE_URL=y") {
		t.Errorf("known key missing: %s", s)
	}
}

// TestMain_SmokeCompile exercises main() indirectly by ensuring envVars
// is wired correctly (so the linter doesn't complain about the var).
func TestMain_SmokeCompile(t *testing.T) {
	if len(envVars) != 4 {
		t.Errorf("envVars should have 4 keys, got %d", len(envVars))
	}
}

// TestMain_CallsRunSuccessfully drives main() itself against a mocked
// server via INSTANT_API_URL. main() runs Run() in the package's CWD which
// means it writes ".env" relative to that — we cd to a temp dir so the
// write lands in a throwaway location.
func TestMain_CallsRunSuccessfully(t *testing.T) {
	srv := newTestServer(t, mockServerOpts{})
	t.Setenv("INSTANT_API_KEY", "")
	t.Setenv("INSTANT_API_URL", srv.URL)
	tmp := t.TempDir()
	orig, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(orig)
	// Redirect stdout to avoid noise.
	r, w, _ := os.Pipe()
	origStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, r)
		close(done)
	}()
	main()
	w.Close()
	<-done
	// .env should now exist in tmp.
	if _, err := os.Stat(filepath.Join(tmp, ".env")); err != nil {
		t.Errorf("expected .env in %s: %v", tmp, err)
	}
}
