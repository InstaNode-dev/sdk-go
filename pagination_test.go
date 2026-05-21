package instant_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/InstaNode-dev/sdk-go/instant"
)

// TestListResourcesPage_Pagination pins B17-P1: ListResourcesPage forwards
// cursor + limit as query params and reads next_cursor from the response.
func TestListResourcesPage_Pagination(t *testing.T) {
	mux := http.NewServeMux()
	var sawCursor, sawLimit string
	mux.HandleFunc("/api/v1/resources", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		sawCursor = q.Get("cursor")
		sawLimit = q.Get("limit")
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true,
			"items": []map[string]any{
				{"id": "r1", "token": "t1", "resource_type": "redis", "tier": "hobby", "status": "active", "created_at": "2024-01-01T00:00:00Z"},
			},
			"total":       42,
			"next_cursor": "cursor-page-2",
		})
	})

	client := serve(t, mux)
	page, err := client.ListResourcesPage(context.Background(), instant.ListResourcesOpts{
		Cursor: "cursor-page-1",
		Limit:  25,
	})
	if err != nil {
		t.Fatalf("ListResourcesPage: %v", err)
	}

	if sawCursor != "cursor-page-1" {
		t.Errorf("server saw cursor=%q, want cursor-page-1", sawCursor)
	}
	if sawLimit != "25" {
		t.Errorf("server saw limit=%q, want 25", sawLimit)
	}
	if page.NextCursor != "cursor-page-2" {
		t.Errorf("NextCursor = %q, want cursor-page-2", page.NextCursor)
	}
	if page.Total != 42 {
		t.Errorf("Total = %d, want 42", page.Total)
	}
}

// TestListResources_NoOptsOmitsQuery pins that the zero-value path doesn't
// add stray query params — a legacy /api/v1/resources without a cursor / limit
// must still work.
func TestListResources_NoOptsOmitsQuery(t *testing.T) {
	mux := http.NewServeMux()
	var rawQuery string
	mux.HandleFunc("/api/v1/resources", func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":    true,
			"items": []any{},
			"total": 0,
		})
	})

	client := serve(t, mux)
	if _, err := client.ListResources(context.Background()); err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if rawQuery != "" {
		_, err := url.ParseQuery(rawQuery)
		t.Errorf("legacy ListResources should send no query string, got %q (parse err=%v)", rawQuery, err)
	}
}

// TestProvisionDatabase_IdempotencyKey pins B17-P1: ProvisionOpts.IdempotencyKey
// is forwarded as the Idempotency-Key HTTP header.
func TestProvisionDatabase_IdempotencyKey(t *testing.T) {
	mux := http.NewServeMux()
	var sawKey string
	mux.HandleFunc("/db/new", func(w http.ResponseWriter, r *http.Request) {
		sawKey = r.Header.Get("Idempotency-Key")
		writeJSON(w, http.StatusCreated, map[string]any{
			"ok":             true,
			"id":             "x",
			"token":          "tok",
			"connection_url": "postgres://x/y",
			"tier":           "hobby",
			"limits":         map[string]any{"storage_mb": 1024, "connections": 8},
		})
	})

	client := serve(t, mux)
	_, err := client.ProvisionDatabase(context.Background(), &instant.ProvisionOpts{
		Name:           "app-db",
		IdempotencyKey: "abc-123",
	})
	if err != nil {
		t.Fatalf("ProvisionDatabase: %v", err)
	}
	if sawKey != "abc-123" {
		t.Errorf("Idempotency-Key = %q, want abc-123", sawKey)
	}
}

// TestProvisionDatabase_NoIdempotencyKeyHeader pins that an empty
// IdempotencyKey does NOT emit a stray header.
func TestProvisionDatabase_NoIdempotencyKeyHeader(t *testing.T) {
	mux := http.NewServeMux()
	var saw bool
	mux.HandleFunc("/db/new", func(w http.ResponseWriter, r *http.Request) {
		_, saw = r.Header["Idempotency-Key"]
		writeJSON(w, http.StatusCreated, map[string]any{
			"ok":             true,
			"id":             "x",
			"token":          "tok",
			"connection_url": "postgres://x/y",
			"tier":           "hobby",
		})
	})
	client := serve(t, mux)
	_, err := client.ProvisionDatabase(context.Background(), &instant.ProvisionOpts{Name: "app-db"})
	if err != nil {
		t.Fatalf("ProvisionDatabase: %v", err)
	}
	if saw {
		t.Errorf("Idempotency-Key header should be absent when opts.IdempotencyKey is empty")
	}
}

// TestProvisionStorage_PresignURL_Absolute pins B17-P1: a relative presign_url
// returned by the server is rewritten to an absolute URL by the SDK.
func TestProvisionStorage_PresignURL_Absolute(t *testing.T) {
	tests := []struct {
		name       string
		serverPath string
		wantSuffix string
		wantAbs    bool // whether we expect the URL to begin with http(s)://
	}{
		{
			name:       "relative path",
			serverPath: "/storage/abc/presign",
			wantSuffix: "/storage/abc/presign",
			wantAbs:    true,
		},
		{
			name:       "relative without leading slash",
			serverPath: "storage/abc/presign",
			wantSuffix: "/storage/abc/presign",
			wantAbs:    true,
		},
		{
			name:       "already absolute https",
			serverPath: "https://example.com/storage/abc/presign",
			wantSuffix: "https://example.com/storage/abc/presign",
			wantAbs:    true,
		},
		{
			name:       "empty stays empty",
			serverPath: "",
			wantSuffix: "",
			wantAbs:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/storage/new", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusCreated, map[string]any{
					"ok":             true,
					"id":             "s",
					"token":          "tok",
					"connection_url": "https://x.example/instant-shared/abc/",
					"tier":           "anonymous",
					"presign_url":    tc.serverPath,
					"mode":           "broker",
				})
			})

			client := serve(t, mux)
			got, err := client.ProvisionStorage(context.Background(), &instant.ProvisionOpts{Name: "app"})
			if err != nil {
				t.Fatalf("ProvisionStorage: %v", err)
			}
			if tc.wantSuffix == "" {
				if got.PresignURL != "" {
					t.Errorf("PresignURL = %q, want empty", got.PresignURL)
				}
				return
			}
			if tc.wantAbs && !(stringHasPrefix(got.PresignURL, "http://") || stringHasPrefix(got.PresignURL, "https://")) {
				t.Errorf("PresignURL = %q is not absolute", got.PresignURL)
			}
			if !stringHasSuffix(got.PresignURL, tc.wantSuffix) {
				t.Errorf("PresignURL = %q, want suffix %q", got.PresignURL, tc.wantSuffix)
			}
		})
	}
}

func stringHasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }
func stringHasSuffix(s, p string) bool { return len(s) >= len(p) && s[len(s)-len(p):] == p }
