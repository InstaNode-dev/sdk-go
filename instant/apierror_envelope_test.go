package instant

// apierror_envelope_test.go — registry contract for the canonical
// instanode.dev error envelope (rule 18 style: iterate the registry, not a
// hand-typed list). The API replies to every 4xx/5xx with
//
//	{ok, error, error_code, message, agent_action, upgrade_url,
//	 retry_after_seconds, request_id}
//
// and APIError must give every one of those keys a home — the original
// struct captured only error+message, silently dropping agent_action,
// error_code, upgrade_url, retry_after_seconds, and request_id, which
// defeated the agent-native contract. These tests fail if a future envelope
// key is added to APIErrorEnvelopeKeys without a tagged field, OR if a sample
// full body fails to round-trip every field onto the struct.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// jsonTagsOfAPIError reflects the JSON tag set declared on APIError, skipping
// untagged fields (StatusCode), the unexported raw field, and json:"-".
func jsonTagsOfAPIError(t *testing.T) map[string]string {
	t.Helper()
	tags := map[string]string{}
	rt := reflect.TypeOf(APIError{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" {
			continue
		}
		tags[name] = f.Name
	}
	return tags
}

// TestAPIError_EnvelopeKeysAllHaveAHome asserts every envelope key the SDK
// claims to map (APIErrorEnvelopeKeys) is backed by a tagged field on
// APIError. A future API field added to the registry without a struct field
// reds this test instead of silently dropping at runtime.
func TestAPIError_EnvelopeKeysAllHaveAHome(t *testing.T) {
	tags := jsonTagsOfAPIError(t)
	for _, key := range APIErrorEnvelopeKeys {
		if _, ok := tags[key]; !ok {
			t.Errorf("envelope key %q in APIErrorEnvelopeKeys has no tagged field on APIError "+
				"(add the field with `json:%q` so it can't drop)", key, key)
		}
	}
}

// TestAPIError_EnvelopeKeysRegistryIsComplete guards the other direction:
// every documented envelope key the API can emit (the canonical contract
// list) must appear in APIErrorEnvelopeKeys. This is the registry-iterating
// guard from rule 18 — if the API adds a key here-but-not-in-the-SDK the test
// names exactly which one is missing.
func TestAPIError_EnvelopeKeysRegistryIsComplete(t *testing.T) {
	// The canonical key set the api emits on its ErrorResponse envelope
	// (api/internal/handlers/helpers.go: ErrorResponse). claim_url is the
	// recycle-gate-only alias the SDK folds into upgrade_url; ok is the
	// success/failure flag the SDK reads via the typed predicates, not via
	// APIError — both are intentionally excluded and listed here so the
	// exclusion is explicit rather than an oversight.
	canonical := []string{
		"error",
		"error_code",
		"message",
		"agent_action",
		"upgrade_url",
		"retry_after_seconds",
		"request_id",
	}
	have := map[string]bool{}
	for _, k := range APIErrorEnvelopeKeys {
		have[k] = true
	}
	for _, k := range canonical {
		if !have[k] {
			t.Errorf("canonical envelope key %q is NOT in APIErrorEnvelopeKeys — "+
				"the SDK will drop it; add it to the registry and APIError", k)
		}
	}
}

// TestAPIError_FullEnvelopeRoundTrips decodes a sample of the complete error
// envelope and asserts EVERY field lands on the struct — the regression the
// fix closes (pre-fix, only Code+Message survived). It also asserts the tag
// mapping: Code holds the category "error", ErrorCode holds the canonical
// "error_code", and CanonicalCode prefers error_code.
func TestAPIError_FullEnvelopeRoundTrips(t *testing.T) {
	const retry = 30
	body := `{
		"ok": false,
		"error": "unauthorized",
		"error_code": "missing_credentials",
		"message": "No INSTANODE_TOKEN was provided.",
		"agent_action": "Have the user log in at https://instanode.dev/login.",
		"upgrade_url": "https://instanode.dev/login",
		"retry_after_seconds": 30,
		"request_id": "req_abc123"
	}`

	var e APIError
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if e.Code != "unauthorized" {
		t.Errorf("Code (json:\"error\", the category) = %q, want %q", e.Code, "unauthorized")
	}
	if e.ErrorCode != "missing_credentials" {
		t.Errorf("ErrorCode (json:\"error_code\") = %q, want %q", e.ErrorCode, "missing_credentials")
	}
	if e.Message != "No INSTANODE_TOKEN was provided." {
		t.Errorf("Message = %q", e.Message)
	}
	if !strings.Contains(e.AgentAction, "log in") {
		t.Errorf("AgentAction not captured: %q", e.AgentAction)
	}
	if e.UpgradeURL != "https://instanode.dev/login" {
		t.Errorf("UpgradeURL = %q", e.UpgradeURL)
	}
	if e.RetryAfterSeconds == nil {
		t.Fatal("RetryAfterSeconds = nil, want non-nil 30")
	}
	if *e.RetryAfterSeconds != retry {
		t.Errorf("RetryAfterSeconds = %d, want %d", *e.RetryAfterSeconds, retry)
	}
	if e.RequestID != "req_abc123" {
		t.Errorf("RequestID = %q", e.RequestID)
	}

	// CanonicalCode prefers the finer-grained error_code over the category.
	if got := e.CanonicalCode(); got != "missing_credentials" {
		t.Errorf("CanonicalCode() = %q, want the canonical machine code %q", got, "missing_credentials")
	}
}

// TestAPIError_CanonicalCodeFallback — when the server omits error_code the
// canonical code falls back to the category (Code / json:"error").
func TestAPIError_CanonicalCodeFallback(t *testing.T) {
	e := &APIError{Code: "not_found"}
	if got := e.CanonicalCode(); got != "not_found" {
		t.Errorf("CanonicalCode() with empty ErrorCode = %q, want fallback to Code %q", got, "not_found")
	}
	e2 := &APIError{Code: "unauthorized", ErrorCode: "missing_credentials"}
	if got := e2.CanonicalCode(); got != "missing_credentials" {
		t.Errorf("CanonicalCode() = %q, want %q", got, "missing_credentials")
	}
}

// TestAPIError_ErrorStringFoldsAgentActionAndUpgrade — the Error() string must
// surface agent_action + upgrade_url so logs are actionable, while staying
// backward-compatible (no trailing " | ..." when neither is present).
func TestAPIError_ErrorStringFoldsAgentActionAndUpgrade(t *testing.T) {
	full := &APIError{
		StatusCode:  402,
		Code:        "quota_exceeded",
		ErrorCode:   "storage_limit_reached",
		Message:     "storage limit reached",
		AgentAction: "Upgrade to Pro at https://instanode.dev/pricing.",
		UpgradeURL:  "https://instanode.dev/pricing",
	}
	s := full.Error()
	// canonical code wins over category in the prefix
	if !strings.Contains(s, "(storage_limit_reached)") {
		t.Errorf("Error() should use canonical code: %q", s)
	}
	if !strings.Contains(s, "agent_action: Upgrade to Pro") {
		t.Errorf("Error() must fold in agent_action: %q", s)
	}
	if !strings.Contains(s, "upgrade_url: https://instanode.dev/pricing") {
		t.Errorf("Error() must fold in upgrade_url: %q", s)
	}

	// Backward compat: with neither agent_action nor upgrade_url, the string
	// is exactly the legacy shape (no trailing pipes).
	plain := &APIError{StatusCode: 404, Code: "not_found", Message: "Resource not found"}
	if got, want := plain.Error(), "instant.dev API error 404 (not_found): Resource not found"; got != want {
		t.Errorf("Error() backward-compat = %q, want %q", got, want)
	}
}

// TestAPIError_FullEnvelopeOverHTTP wires the full envelope through the real
// client error path (do → json.Unmarshal(raw, apiErr)) to prove the wire
// decode — not just a direct unmarshal — populates every field.
func TestAPIError_FullEnvelopeOverHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"ok":false,"error":"rate_limited","error_code":"too_many_requests",`+
			`"message":"slow down","agent_action":"Wait 60 seconds and retry.",`+
			`"upgrade_url":"https://instanode.dev/pricing","retry_after_seconds":60,"request_id":"req_z9"}`)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	var out map[string]any
	err := c.get(context.Background(), "/x", &out)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("StatusCode = %d", apiErr.StatusCode)
	}
	if apiErr.ErrorCode != "too_many_requests" {
		t.Errorf("ErrorCode = %q (dropped on the wire path?)", apiErr.ErrorCode)
	}
	if apiErr.AgentAction == "" || apiErr.UpgradeURL == "" || apiErr.RequestID == "" {
		t.Errorf("agent-native fields dropped on the wire path: action=%q upgrade=%q rid=%q",
			apiErr.AgentAction, apiErr.UpgradeURL, apiErr.RequestID)
	}
	if apiErr.RetryAfterSeconds == nil || *apiErr.RetryAfterSeconds != 60 {
		t.Errorf("RetryAfterSeconds not captured: %v", apiErr.RetryAfterSeconds)
	}
	if !IsRateLimited(err) {
		t.Error("IsRateLimited should match the 429")
	}
}
