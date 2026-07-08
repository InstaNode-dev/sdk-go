package instant

// audit_test.go — exercises ListAuditEvents / ListAuditEventsPage against
// httptest servers mirroring the api audit handler envelope
// (api/internal/handlers/audit.go: {ok, items, total_returned, next_cursor,
// lookback_days, tier}).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// auditServer builds an httptest server that records the incoming request
// and responds with the given status + body. Uses the operateCall recorder
// defined in vault_test.go (same package, same test binary).
func auditServer(t *testing.T, status int, body string) (*httptest.Server, *operateCall) {
	t.Helper()
	return newOperateServer(t, status, body)
}

const auditListJSON = `{
  "ok": true,
  "items": [
    {
      "id": "aabbccdd-0000-0000-0000-000000000001",
      "kind": "resource.created",
      "created_at": "2026-07-07T10:00:00Z",
      "actor": "user",
      "actor_user_id": "usr-0001",
      "actor_email_masked": "m***@example.com",
      "resource_id": "res-0001",
      "resource_type": "postgres",
      "summary": "Provisioned <strong>postgres</strong>",
      "metadata": {"env": "production"}
    }
  ],
  "total_returned": 1,
  "next_cursor": null,
  "lookback_days": 30,
  "tier": "hobby"
}`

func TestListAuditEvents_HappyPath(t *testing.T) {
	srv, call := auditServer(t, http.StatusOK, auditListJSON)

	c := New(WithBaseURL(srv.URL), WithAPIKey("tok"))
	list, err := c.ListAuditEvents(context.Background())
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}

	if call.Method != http.MethodGet {
		t.Errorf("method = %q, want GET", call.Method)
	}
	if call.Path != "/api/v1/audit" {
		t.Errorf("path = %q, want /api/v1/audit", call.Path)
	}
	if call.Query != "" {
		t.Errorf("query = %q, want empty (no-opts call)", call.Query)
	}

	if !list.OK {
		t.Error("ok = false, want true")
	}
	if len(list.Items) != 1 {
		t.Fatalf("items len = %d, want 1", len(list.Items))
	}
	ev := list.Items[0]
	if ev.Kind != "resource.created" {
		t.Errorf("kind = %q", ev.Kind)
	}
	if ev.Actor != "user" {
		t.Errorf("actor = %q", ev.Actor)
	}
	if ev.ActorUserID == nil || *ev.ActorUserID != "usr-0001" {
		t.Errorf("actor_user_id = %v", ev.ActorUserID)
	}
	if ev.ActorEmailMasked == nil || *ev.ActorEmailMasked != "m***@example.com" {
		t.Errorf("actor_email_masked = %v", ev.ActorEmailMasked)
	}
	if ev.ResourceType != "postgres" {
		t.Errorf("resource_type = %q", ev.ResourceType)
	}
	if list.LookbackDays != 30 {
		t.Errorf("lookback_days = %d, want 30", list.LookbackDays)
	}
	if list.Tier != "hobby" {
		t.Errorf("tier = %q, want hobby", list.Tier)
	}

	// Metadata should be preserved as raw JSON.
	var meta map[string]string
	if err := json.Unmarshal(ev.Metadata, &meta); err != nil {
		t.Fatalf("metadata decode: %v", err)
	}
	if meta["env"] != "production" {
		t.Errorf("metadata env = %q", meta["env"])
	}
}

func TestListAuditEvents_SystemActorNilFields(t *testing.T) {
	body := `{"ok":true,"items":[{"id":"x","kind":"billing.upgraded","created_at":"2026-07-07T09:00:00Z","actor":"system","actor_user_id":null,"actor_email_masked":null,"resource_id":null,"resource_type":"","summary":"Upgraded to Pro","metadata":null}],"total_returned":1,"lookback_days":-1,"tier":"team"}`
	srv, _ := auditServer(t, http.StatusOK, body)

	c := New(WithBaseURL(srv.URL), WithAPIKey("tok"))
	list, err := c.ListAuditEvents(context.Background())
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	ev := list.Items[0]
	if ev.ActorUserID != nil {
		t.Errorf("actor_user_id should be nil for system actor, got %v", *ev.ActorUserID)
	}
	if ev.ActorEmailMasked != nil {
		t.Errorf("actor_email_masked should be nil, got %v", *ev.ActorEmailMasked)
	}
	if ev.ResourceID != nil {
		t.Errorf("resource_id should be nil, got %v", *ev.ResourceID)
	}
	if list.LookbackDays != -1 {
		t.Errorf("lookback_days = %d, want -1 (unlimited)", list.LookbackDays)
	}
}

func TestListAuditEventsPage_QueryParams(t *testing.T) {
	srv, call := auditServer(t, http.StatusOK, `{"ok":true,"items":[],"total_returned":0,"lookback_days":90,"tier":"pro"}`)

	before := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	since := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	c := New(WithBaseURL(srv.URL), WithAPIKey("tok"))
	_, err := c.ListAuditEventsPage(context.Background(), ListAuditEventsOpts{
		Limit:  100,
		Kind:   "resource.created",
		Before: before,
		Since:  since,
	})
	if err != nil {
		t.Fatalf("ListAuditEventsPage: %v", err)
	}

	q := call.Query
	if !strings.Contains(q, "limit=100") {
		t.Errorf("query %q missing limit=100", q)
	}
	if !strings.Contains(q, "kind=resource.created") {
		t.Errorf("query %q missing kind=resource.created", q)
	}
	if !strings.Contains(q, "before=2026-07-01T12%3A00%3A00Z") && !strings.Contains(q, "before=2026-07-01T12:00:00Z") {
		t.Errorf("query %q missing before timestamp", q)
	}
	if !strings.Contains(q, "since=2026-06-01T00%3A00%3A00Z") && !strings.Contains(q, "since=2026-06-01T00:00:00Z") {
		t.Errorf("query %q missing since timestamp", q)
	}
}

func TestListAuditEventsPage_NoQueryParamsWhenZeroOpts(t *testing.T) {
	srv, call := auditServer(t, http.StatusOK, `{"ok":true,"items":[],"total_returned":0,"lookback_days":30,"tier":"hobby"}`)

	c := New(WithBaseURL(srv.URL), WithAPIKey("tok"))
	_, err := c.ListAuditEventsPage(context.Background(), ListAuditEventsOpts{})
	if err != nil {
		t.Fatalf("ListAuditEventsPage: %v", err)
	}
	if call.Query != "" {
		t.Errorf("expected empty query string for zero opts, got %q", call.Query)
	}
}

func TestListAuditEvents_UntilParam(t *testing.T) {
	srv, call := auditServer(t, http.StatusOK, `{"ok":true,"items":[],"total_returned":0,"lookback_days":30,"tier":"hobby"}`)

	until := time.Date(2026, 6, 30, 23, 59, 59, 0, time.UTC)
	c := New(WithBaseURL(srv.URL), WithAPIKey("tok"))
	_, err := c.ListAuditEventsPage(context.Background(), ListAuditEventsOpts{Until: until})
	if err != nil {
		t.Fatalf("ListAuditEventsPage: %v", err)
	}
	if !strings.Contains(call.Query, "until=") {
		t.Errorf("query %q missing until param", call.Query)
	}
}

func TestListAuditEvents_UpgradeRequired(t *testing.T) {
	body := `{"ok":false,"error":"upgrade_required","message":"Audit log requires Hobby or higher"}`
	srv, _ := auditServer(t, http.StatusPaymentRequired, body)

	c := New(WithBaseURL(srv.URL), WithAPIKey("tok"))
	_, err := c.ListAuditEvents(context.Background())
	if err == nil {
		t.Fatal("expected error for 402, got nil")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusPaymentRequired {
		t.Errorf("status = %d, want 402", apiErr.StatusCode)
	}
}

func TestListAuditEvents_Unauthorized(t *testing.T) {
	srv, _ := auditServer(t, http.StatusUnauthorized,
		`{"ok":false,"error":"unauthorized","message":"Authentication required"}`)

	c := New(WithBaseURL(srv.URL))
	_, err := c.ListAuditEvents(context.Background())
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", apiErr.StatusCode)
	}
}

func TestListAuditEvents_NextCursor(t *testing.T) {
	body := `{"ok":true,"items":[{"id":"x","kind":"deploy.healthy","created_at":"2026-06-30T08:00:00Z","actor":"system","summary":"Deploy healthy"}],"total_returned":1,"next_cursor":"2026-06-30T08:00:00.000000000Z","lookback_days":30,"tier":"hobby"}`
	srv, _ := auditServer(t, http.StatusOK, body)

	c := New(WithBaseURL(srv.URL), WithAPIKey("tok"))
	list, err := c.ListAuditEvents(context.Background())
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if list.NextCursor == "" {
		t.Error("NextCursor should be non-empty when a full page is returned")
	}
	// The cursor is an RFC3339Nano timestamp — verify it parses.
	if _, err := time.Parse(time.RFC3339Nano, list.NextCursor); err != nil {
		t.Errorf("NextCursor %q is not RFC3339Nano: %v", list.NextCursor, err)
	}
}
