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

// testLookup returns a lookupEnv func that maps the given key/value pairs and
// falls back to "" for anything else.
func testLookup(kvs ...string) func(string) string {
	m := make(map[string]string, len(kvs)/2)
	for i := 0; i+1 < len(kvs); i += 2 {
		m[kvs[i]] = kvs[i+1]
	}
	return func(key string) string { return m[key] }
}

func TestRun_HappyPath(t *testing.T) {
	const wantID = "550e8400-e29b-41d4-a716-446655440099"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email   string `json:"email"`
			Company string `json:"company"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Email != "cto@acme.com" {
			http.Error(w, "bad email", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"ok":true,"id":"`+wantID+`"}`)
	}))
	t.Cleanup(srv.Close)

	c := instant.New(instant.WithBaseURL(srv.URL))
	var out strings.Builder
	err := run(context.Background(), c, &instant.LeadParams{
		Email:   "cto@acme.com",
		Company: "Acme Corp",
		UseCase: "Need SOC 2 + 10 TB Postgres.",
	}, &out)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, wantID) {
		t.Errorf("output missing lead ID %q:\n%s", wantID, s)
	}
	if !strings.Contains(s, "cto@acme.com") {
		t.Errorf("output missing email:\n%s", s)
	}
}

func TestRun_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"internal","message":"db down"}`)
	}))
	t.Cleanup(srv.Close)

	c := instant.New(instant.WithBaseURL(srv.URL))
	var out strings.Builder
	err := run(context.Background(), c, &instant.LeadParams{Email: "a@b.com"}, &out)
	if err == nil {
		t.Fatal("expected error from server failure")
	}
}

func TestRealMain_HappyPath(t *testing.T) {
	const wantID = "550e8400-e29b-41d4-a716-446655440077"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"ok":true,"id":"`+wantID+`"}`)
	}))
	t.Cleanup(srv.Close)

	var stdout, stderr strings.Builder
	code := realMain(
		[]string{"-email", "cto@acme.com", "-company", "Acme Corp"},
		&stdout,
		&stderr,
		testLookup("INSTANODE_API_URL", srv.URL),
	)
	if code != 0 {
		t.Errorf("want exit 0, got %d; stderr: %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), wantID) {
		t.Errorf("stdout missing lead ID; got: %q", stdout.String())
	}
}

func TestRealMain_MissingEmail(t *testing.T) {
	var stdout, stderr strings.Builder
	code := realMain([]string{}, &stdout, &stderr, testLookup())
	if code != 1 {
		t.Errorf("want exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "error: -email is required") {
		t.Errorf("stderr missing error message: %q", stderr.String())
	}
}

func TestRealMain_BadFlag(t *testing.T) {
	var stdout, stderr strings.Builder
	code := realMain([]string{"-unknown-flag-xyz"}, &stdout, &stderr, testLookup())
	if code != 1 {
		t.Errorf("unknown flag should exit 1, got %d", code)
	}
}

func TestRealMain_WithToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"ok":true,"id":"abc-token-test"}`)
	}))
	t.Cleanup(srv.Close)

	var stdout, stderr strings.Builder
	code := realMain(
		[]string{"-email", "cto@acme.com"},
		&stdout,
		&stderr,
		testLookup("INSTANT_TOKEN", "test-token-xyz", "INSTANODE_API_URL", srv.URL),
	)
	if code != 0 {
		t.Errorf("want exit 0, got %d; stderr: %q", code, stderr.String())
	}
	if !strings.Contains(gotAuth, "test-token-xyz") {
		t.Errorf("Authorization header missing token; got: %q", gotAuth)
	}
}

func TestMain_ExitIsCalled(t *testing.T) {
	// main() calls osExit(realMain(os.Args[1:], ...)) — we replace osExit to
	// capture the code without terminating the test binary.
	var gotCode int
	osExit = func(code int) { gotCode = code }
	defer func() { osExit = os.Exit }()

	// os.Args[1:] in the test binary contains -test.* flags that FlagSet
	// doesn't recognise → realMain returns 1 → main() passes 1 to osExit.
	main()

	if gotCode != 1 {
		t.Errorf("main() via test args: want exit code 1, got %d", gotCode)
	}
}

func TestRealMain_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"internal_error","message":"db down"}`)
	}))
	t.Cleanup(srv.Close)

	var stdout, stderr strings.Builder
	code := realMain(
		[]string{"-email", "fail@acme.com"},
		&stdout,
		&stderr,
		testLookup("INSTANODE_API_URL", srv.URL),
	)
	if code != 1 {
		t.Errorf("server error should exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "CreateLead") {
		t.Errorf("stderr missing error prefix; got: %q", stderr.String())
	}
}
