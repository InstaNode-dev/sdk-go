package instant_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/InstaNode-dev/sdk-go/instant"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func serve(t *testing.T, mux *http.ServeMux) *instant.Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return instant.New(instant.WithBaseURL(srv.URL))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// ─── ProvisionDatabase ────────────────────────────────────────────────────────

func TestProvisionDatabase_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/db/new", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{
			"ok":             true,
			"id":             "db-id",
			"token":          "db-tok",
			"connection_url": "postgres://usr:pass@host:5432/db",
			"tier":           "anonymous",
			"limits": map[string]any{
				"storage_mb":  10,
				"connections": 2,
				"expires_in":  "24h",
			},
			"note": "Works now.",
		})
	})

	client := serve(t, mux)
	result, err := client.ProvisionDatabase(context.Background(), &instant.ProvisionOpts{Name: "test-db"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Token != "db-tok" {
		t.Errorf("Token = %q, want db-tok", result.Token)
	}
	if result.ConnectionURL != "postgres://usr:pass@host:5432/db" {
		t.Errorf("ConnectionURL = %q", result.ConnectionURL)
	}
	if result.Limits.StorageMB != 10 {
		t.Errorf("StorageMB = %d, want 10", result.Limits.StorageMB)
	}
	if result.Limits.Connections != 2 {
		t.Errorf("Connections = %d, want 2", result.Limits.Connections)
	}
}

func TestProvisionDatabase_ServiceDisabled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/db/new", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "service_disabled",
			"message": "Postgres provisioning is coming in Phase 2.",
		})
	})

	client := serve(t, mux)
	_, err := client.ProvisionDatabase(context.Background(), &instant.ProvisionOpts{Name: "test-db"})
	if !instant.IsServiceUnavailable(err) {
		t.Errorf("expected IsServiceUnavailable, got: %v", err)
	}
}

// ─── ProvisionCache ───────────────────────────────────────────────────────────

func TestProvisionCache_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cache/new", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{
			"ok":             true,
			"id":             "cache-id",
			"token":          "cache-tok",
			"connection_url": "redis://:pass@host:6379",
			"key_prefix":     "ns_cache-tok:",
			"tier":           "anonymous",
			"limits": map[string]any{
				"memory_mb":  5,
				"expires_in": "24h",
			},
		})
	})

	client := serve(t, mux)
	result, err := client.ProvisionCache(context.Background(), &instant.ProvisionOpts{Name: "my-cache"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Token != "cache-tok" {
		t.Errorf("Token = %q, want cache-tok", result.Token)
	}
	if result.KeyPrefix != "ns_cache-tok:" {
		t.Errorf("KeyPrefix = %q, want ns_cache-tok:", result.KeyPrefix)
	}
	if result.Limits.MemoryMB != 5 {
		t.Errorf("MemoryMB = %d, want 5", result.Limits.MemoryMB)
	}
}

// ─── ProvisionMongoDB ─────────────────────────────────────────────────────────

func TestProvisionMongoDB_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/nosql/new", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{
			"ok":             true,
			"id":             "mongo-id",
			"token":          "mongo-tok",
			"connection_url": "mongodb://usr:pass@host:27017/db",
			"tier":           "anonymous",
			"limits": map[string]any{
				"storage_mb":  5,
				"connections": 2,
				"expires_in":  "24h",
			},
		})
	})

	client := serve(t, mux)
	result, err := client.ProvisionMongoDB(context.Background(), &instant.ProvisionOpts{Name: "test-mongo"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Token != "mongo-tok" {
		t.Errorf("Token = %q, want mongo-tok", result.Token)
	}
}

// ─── ProvisionQueue ───────────────────────────────────────────────────────────

func TestProvisionQueue_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/queue/new", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{
			"ok":             true,
			"id":             "q-id",
			"token":          "q-tok",
			"connection_url": "nats://usr:pass@host:4222",
			"tier":           "anonymous",
			"limits": map[string]any{
				"storage_mb": 1024,
				"expires_in": "24h",
			},
		})
	})

	client := serve(t, mux)
	result, err := client.ProvisionQueue(context.Background(), &instant.ProvisionOpts{Name: "test-queue"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Token != "q-tok" {
		t.Errorf("Token = %q, want q-tok", result.Token)
	}
}

// ─── Mandatory resource name ──────────────────────────────────────────────────

// TestProvision_RequiresName verifies that every provision method rejects a
// missing or invalid name client-side, before any network request is made.
// The server enforces the same contract with an HTTP 400; the SDK fails fast.
func TestProvision_RequiresName(t *testing.T) {
	// A server that fails the test if it is ever reached — the SDK must
	// reject the request before the network round-trip.
	mux := http.NewServeMux()
	hit := false
	for _, path := range []string{"/db/new", "/cache/new", "/nosql/new", "/queue/new"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			hit = true
			writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
		})
	}
	client := serve(t, mux)
	ctx := context.Background()

	provisioners := map[string]func(*instant.ProvisionOpts) error{
		"ProvisionDatabase": func(o *instant.ProvisionOpts) error {
			_, err := client.ProvisionDatabase(ctx, o)
			return err
		},
		"ProvisionCache": func(o *instant.ProvisionOpts) error {
			_, err := client.ProvisionCache(ctx, o)
			return err
		},
		"ProvisionMongoDB": func(o *instant.ProvisionOpts) error {
			_, err := client.ProvisionMongoDB(ctx, o)
			return err
		},
		"ProvisionQueue": func(o *instant.ProvisionOpts) error {
			_, err := client.ProvisionQueue(ctx, o)
			return err
		},
	}

	// nil opts, empty name, and names that violate the server regex
	// (1-64 chars, ^[A-Za-z0-9][A-Za-z0-9 _-]*$) must all be rejected.
	badOpts := []struct {
		name string
		opts *instant.ProvisionOpts
	}{
		{"nil opts", nil},
		{"empty name", &instant.ProvisionOpts{Name: ""}},
		{"leading hyphen", &instant.ProvisionOpts{Name: "-bad"}},
		{"leading space", &instant.ProvisionOpts{Name: " bad"}},
		{"illegal char", &instant.ProvisionOpts{Name: "bad/name"}},
		{"too long", &instant.ProvisionOpts{Name: string(make([]byte, 65))}},
	}

	for method, fn := range provisioners {
		for _, bad := range badOpts {
			t.Run(method+"/"+bad.name, func(t *testing.T) {
				hit = false
				err := fn(bad.opts)
				if err == nil {
					t.Fatalf("%s with %s: expected error, got nil", method, bad.name)
				}
				if hit {
					t.Errorf("%s with %s: SDK sent a network request; it must reject client-side", method, bad.name)
				}
			})
		}
	}
}

// TestProvision_AcceptsValidName verifies names that satisfy the server
// contract pass client-side validation and reach the wire.
func TestProvision_AcceptsValidName(t *testing.T) {
	var gotName string
	mux := http.NewServeMux()
	mux.HandleFunc("/db/new", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		gotName = body["name"]
		writeJSON(w, http.StatusCreated, map[string]any{
			"ok":             true,
			"token":          "tok",
			"connection_url": "postgres://u:p@h:5432/db",
			"tier":           "anonymous",
		})
	})
	client := serve(t, mux)

	for _, valid := range []string{"app-db", "My App 1", "db_2", "A"} {
		if _, err := client.ProvisionDatabase(context.Background(), &instant.ProvisionOpts{Name: valid}); err != nil {
			t.Fatalf("ProvisionDatabase(%q): unexpected error: %v", valid, err)
		}
		if gotName != valid {
			t.Errorf("server received name = %q, want %q", gotName, valid)
		}
	}
}

// ─── ListResources ────────────────────────────────────────────────────────────

func TestListResources_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/resources", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true,
			"items": []map[string]any{
				{
					"id":            "res-1",
					"token":         "tok-1",
					"resource_type": "redis",
					"tier":          "hobby",
					"status":        "active",
					"created_at":    "2024-01-01T00:00:00Z",
				},
				{
					"id":            "res-2",
					"token":         "tok-2",
					"resource_type": "postgres",
					"tier":          "hobby",
					"status":        "active",
					"created_at":    "2024-01-02T00:00:00Z",
				},
			},
			"total": 2,
		})
	})

	client := serve(t, mux)
	list, err := client.ListResources(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if list.Total != 2 {
		t.Errorf("Total = %d, want 2", list.Total)
	}
	if len(list.Items) != 2 {
		t.Errorf("len(Items) = %d, want 2", len(list.Items))
	}
	if list.Items[0].ResourceType != "redis" {
		t.Errorf("Items[0].ResourceType = %q, want redis", list.Items[0].ResourceType)
	}
}

func TestListResources_Unauthorized(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/resources", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error":   "unauthorized",
			"message": "Valid session token required",
		})
	})

	client := serve(t, mux)
	_, err := client.ListResources(context.Background())
	if !instant.IsUnauthorized(err) {
		t.Errorf("expected IsUnauthorized, got: %v", err)
	}
}

// ─── GetResource ─────────────────────────────────────────────────────────────

func TestGetResource_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true,
			"item": map[string]any{
				"id":            "res-1",
				"token":         "tok-1",
				"resource_type": "postgres",
				"tier":          "pro",
				"status":        "active",
				"storage_bytes": 1024,
				"created_at":    "2024-01-01T00:00:00Z",
			},
		})
	})

	client := serve(t, mux)
	r, err := client.GetResource(context.Background(), "tok-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Tier != "pro" {
		t.Errorf("Tier = %q, want pro", r.Tier)
	}
	if r.StorageBytes != 1024 {
		t.Errorf("StorageBytes = %d, want 1024", r.StorageBytes)
	}
}

func TestGetResource_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error":   "not_found",
			"message": "Resource not found",
		})
	})

	client := serve(t, mux)
	_, err := client.GetResource(context.Background(), "missing-tok")
	if !instant.IsNotFound(err) {
		t.Errorf("expected IsNotFound, got: %v", err)
	}
}

// ─── DeleteResource ───────────────────────────────────────────────────────────

func TestDeleteResource_Success(t *testing.T) {
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		called = true
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"message": "Resource deleted",
		})
	})

	client := serve(t, mux)
	if err := client.DeleteResource(context.Background(), "del-tok"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected DELETE endpoint to be called")
	}
}

func TestDeleteResource_Forbidden(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error":   "forbidden",
			"message": "You do not own this resource",
		})
	})

	client := serve(t, mux)
	err := client.DeleteResource(context.Background(), "other-tok")
	if !instant.IsForbidden(err) {
		t.Errorf("expected IsForbidden, got: %v", err)
	}
}

// ─── RotateCredentials ────────────────────────────────────────────────────────

func TestRotateCredentials_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":             true,
			"connection_url": "postgres://usr:newpass@host:5432/db",
		})
	})

	client := serve(t, mux)
	result, err := client.RotateCredentials(context.Background(), "rot-tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ConnectionURL != "postgres://usr:newpass@host:5432/db" {
		t.Errorf("ConnectionURL = %q", result.ConnectionURL)
	}
}

// ─── Claim ────────────────────────────────────────────────────────────────────

func TestClaim_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/claim", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		if body["jwt"] == "" || body["email"] == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "missing_fields",
				"message": "jwt and email are required",
			})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"ok":      true,
			"team_id": "team-uuid",
			"user_id": "user-uuid",
			"message": "Account created. Your resources have been transferred.",
		})
	})

	client := serve(t, mux)
	result, err := client.Claim(context.Background(), instant.ClaimOpts{
		JWT:      "test-jwt",
		Email:    "dev@example.com",
		TeamName: "Test Corp",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TeamID != "team-uuid" {
		t.Errorf("TeamID = %q, want team-uuid", result.TeamID)
	}
}

func TestClaim_AlreadyClaimed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/claim", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   "already_claimed",
			"message": "This upgrade token has already been used",
		})
	})

	client := serve(t, mux)
	_, err := client.Claim(context.Background(), instant.ClaimOpts{
		JWT:   "used-jwt",
		Email: "dev@example.com",
	})
	if !instant.IsConflict(err) {
		t.Errorf("expected IsConflict, got: %v", err)
	}
}

func TestClaim_MissingJWT(t *testing.T) {
	client := instant.New(instant.WithBaseURL("http://localhost:1"))
	_, err := client.Claim(context.Background(), instant.ClaimOpts{Email: "x@y.com"})
	if err == nil {
		t.Fatal("expected error for missing JWT")
	}
}

func TestClaim_MissingEmail(t *testing.T) {
	client := instant.New(instant.WithBaseURL("http://localhost:1"))
	_, err := client.Claim(context.Background(), instant.ClaimOpts{JWT: "tok"})
	if err == nil {
		t.Fatal("expected error for missing email")
	}
}

// ─── Error helpers ────────────────────────────────────────────────────────────

func TestAPIError_Error(t *testing.T) {
	tests := []struct {
		err  *instant.APIError
		want string
	}{
		{
			&instant.APIError{StatusCode: 404, Code: "not_found", Message: "Resource not found"},
			"instant.dev API error 404 (not_found): Resource not found",
		},
		{
			&instant.APIError{StatusCode: 503},
			"instant.dev API error 503",
		},
	}
	for _, tt := range tests {
		if got := tt.err.Error(); got != tt.want {
			t.Errorf("Error() = %q, want %q", got, tt.want)
		}
	}
}

func TestIsHelpers(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		fn     func(error) bool
		expect bool
	}{
		{"IsNotFound/true", &instant.APIError{StatusCode: 404}, instant.IsNotFound, true},
		{"IsNotFound/false", &instant.APIError{StatusCode: 403}, instant.IsNotFound, false},
		{"IsUnauthorized/true", &instant.APIError{StatusCode: 401}, instant.IsUnauthorized, true},
		{"IsForbidden/true", &instant.APIError{StatusCode: 403}, instant.IsForbidden, true},
		{"IsRateLimited/true", &instant.APIError{StatusCode: 429}, instant.IsRateLimited, true},
		{"IsConflict/true", &instant.APIError{StatusCode: 409}, instant.IsConflict, true},
		{"IsServiceUnavailable/true", &instant.APIError{StatusCode: 503}, instant.IsServiceUnavailable, true},
		{"IsNotFound/nil", nil, instant.IsNotFound, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn(tt.err); got != tt.expect {
				t.Errorf("%s(%v) = %v, want %v", tt.name, tt.err, got, tt.expect)
			}
		})
	}
}

// ─── Context cancellation ─────────────────────────────────────────────────────

func TestContextCancellation(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/db/new", func(w http.ResponseWriter, r *http.Request) {
		// Block indefinitely — cancellation should abort before this completes.
		<-r.Context().Done()
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	client := serve(t, mux)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := client.ProvisionDatabase(ctx, &instant.ProvisionOpts{Name: "test-db"})
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

// ─── WithAPIKey transport ─────────────────────────────────────────────────────

func TestWithAPIKey_SetsAuthHeader(t *testing.T) {
	var gotAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/db/new", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeJSON(w, http.StatusCreated, map[string]any{
			"ok":             true,
			"id":             "db-id",
			"token":          "tok-auth",
			"connection_url": "postgres://u:p@host:5432/db",
			"tier":           "hobby",
			"limits": map[string]any{
				"storage_mb": 500,
			},
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := instant.New(
		instant.WithBaseURL(srv.URL),
		instant.WithAPIKey("inst_live_mykey"),
	)
	_, err := client.ProvisionDatabase(context.Background(), &instant.ProvisionOpts{Name: "test-db"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer inst_live_mykey" {
		t.Errorf("Authorization = %q, want Bearer inst_live_mykey", gotAuth)
	}
}

// ─── ProvisionStorage ─────────────────────────────────────────────────────────

// TestProvisionStorage covers both response shapes the agent API returns from
// POST /storage/new:
//
//   - the fresh-provision path — HTTP 201, full S3 credentials
//     (endpoint, access_key_id, secret_access_key, prefix); and
//   - the fingerprint-dedup path — HTTP 200, the 6th-call response that returns
//     an already-provisioned resource. It echoes token + connection_url but
//     OMITS the S3 credential fields (they are not reconstructable from the
//     stored resource row).
//
// Regression: ProvisionStorage previously errored on the dedup path because it
// checked result.Endpoint != "" as a success invariant. The dedup body has no
// endpoint, so a perfectly valid 200 dedup response became a spurious error.
// The fix mirrors ProvisionDatabase/Cache/MongoDB/Queue: connection_url (which
// the dedup path always carries) is the secondary invariant, not endpoint.
func TestProvisionStorage(t *testing.T) {
	const (
		freshToken   = "stor-fresh-tok"
		dedupToken   = "stor-dedup-tok"
		bucketURL    = "https://nyc3.digitaloceanspaces.com/instant-shared/abc12345/"
		s3Endpoint   = "https://nyc3.digitaloceanspaces.com"
		dedupNote    = "Daily anonymous limit reached — returning your existing storage bucket."
		dedupUpgrade = "https://api.instanode.dev/start?t=jwt-abc"
	)

	tests := []struct {
		name         string
		status       int
		body         map[string]any
		wantErr      bool
		wantToken    string
		wantConnURL  string
		wantEndpoint string
	}{
		{
			name:   "fresh provision returns full S3 credentials",
			status: http.StatusCreated,
			body: map[string]any{
				"ok":                true,
				"id":                "stor-id",
				"token":             freshToken,
				"name":              "app-assets",
				"connection_url":    bucketURL,
				"endpoint":          s3Endpoint,
				"access_key_id":     "key_abc12345",
				"secret_access_key": "0123456789abcdef0123456789abcdef",
				"prefix":            "abc12345/",
				"tier":              "anonymous",
				"limits":            map[string]any{"storage_mb": 10, "expires_in": "24h"},
			},
			wantToken:    freshToken,
			wantConnURL:  bucketURL,
			wantEndpoint: s3Endpoint,
		},
		{
			// The dedup body intentionally omits endpoint / access_key_id /
			// secret_access_key / prefix — exactly what api/internal/handlers/
			// storage.go emits on the limitExceeded branch.
			name:   "dedup path returns existing resource without S3 credentials",
			status: http.StatusOK,
			body: map[string]any{
				"ok":             true,
				"id":             "stor-id-existing",
				"token":          dedupToken,
				"name":           "app-assets",
				"connection_url": bucketURL,
				"tier":           "anonymous",
				"env":            "development",
				"limits":         map[string]any{"storage_mb": 10, "expires_in": "24h"},
				"note":           dedupNote,
				"upgrade":        dedupUpgrade,
				"upgrade_jwt":    "jwt-abc",
			},
			wantToken:    dedupToken,
			wantConnURL:  bucketURL,
			wantEndpoint: "",
		},
		{
			name:   "empty token is still an error",
			status: http.StatusOK,
			body: map[string]any{
				"ok":             true,
				"connection_url": bucketURL,
			},
			wantErr: true,
		},
		{
			name:   "empty connection_url is still an error",
			status: http.StatusCreated,
			body: map[string]any{
				"ok":    true,
				"token": freshToken,
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/storage/new", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, tc.status, tc.body)
			})
			client := serve(t, mux)

			result, err := client.ProvisionStorage(context.Background(), &instant.ProvisionOpts{Name: "app-assets"})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (result=%+v)", result)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Token != tc.wantToken {
				t.Errorf("Token = %q, want %q", result.Token, tc.wantToken)
			}
			if result.ConnectionURL != tc.wantConnURL {
				t.Errorf("ConnectionURL = %q, want %q", result.ConnectionURL, tc.wantConnURL)
			}
			if result.Endpoint != tc.wantEndpoint {
				t.Errorf("Endpoint = %q, want %q", result.Endpoint, tc.wantEndpoint)
			}
		})
	}
}
