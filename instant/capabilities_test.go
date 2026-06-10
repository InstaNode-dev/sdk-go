package instant

// capabilities_test.go — exercises Client.Capabilities against a httptest
// server serving a realistic two-tier matrix matching the api
// /api/v1/capabilities envelope shape (api/internal/handlers/capabilities.go).

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// capabilitiesBody is a trimmed but shape-accurate sample of the live
// /api/v1/capabilities response: a free terminal-free tier and a paid tier,
// plus the docs/contact pointers and a forward-compat field
// ("future_unknown_field") that must survive on Raw without breaking decode.
const capabilitiesBody = `{
  "ok": true,
  "docs": "https://instanode.dev/llms-full.txt",
  "contact": "mailto:enterprise@instanode.dev",
  "future_unknown_field": "tolerated",
  "tiers": [
    {
      "tier": "anonymous",
      "display_name": "Anonymous",
      "price_usd_monthly": 0,
      "paid_from_day_one": false,
      "storage_limit_mb": {"postgres": 10, "redis": 5},
      "connections_limit": {"postgres": 2},
      "resource_count_limit": {"postgres": 2},
      "deployments_apps": 0,
      "backup_retention_days": 0,
      "backup_restore_enabled": false,
      "manual_backups_per_day": 0,
      "rpo_minutes": 0,
      "rto_minutes": 0,
      "annual_discount_percent": 0,
      "upgrade_url": "https://instanode.dev/pricing/",
      "is_terminal_tier": false
    },
    {
      "tier": "team",
      "display_name": "Team",
      "price_usd_monthly": 199,
      "paid_from_day_one": true,
      "storage_limit_mb": {"postgres": -1},
      "connections_limit": {"postgres": -1},
      "resource_count_limit": {"postgres": -1},
      "deployments_apps": -1,
      "backup_retention_days": 30,
      "backup_restore_enabled": true,
      "manual_backups_per_day": 10,
      "rpo_minutes": 5,
      "rto_minutes": 15,
      "annual_discount_percent": 17,
      "upgrade_url": null,
      "is_terminal_tier": true
    }
  ]
}`

func TestCapabilities_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Valid JSON, but `tiers` is a string where the typed struct expects an
		// array — the typed decode fails, exercising the decode-error branch.
		_, _ = io.WriteString(w, `{"ok":true,"tiers":"not-an-array"}`)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	if _, err := c.Capabilities(context.Background()); err == nil {
		t.Fatal("Capabilities: expected a decode error for malformed tiers, got nil")
	}
}

func TestCapabilities_TypedDecode(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, capabilitiesBody)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	caps, err := c.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}

	if gotPath != capabilitiesPath {
		t.Errorf("hit path %q, want %q", gotPath, capabilitiesPath)
	}
	if !caps.OK {
		t.Error("OK = false, want true")
	}
	if caps.Docs != "https://instanode.dev/llms-full.txt" {
		t.Errorf("Docs = %q", caps.Docs)
	}
	if caps.Contact != "mailto:enterprise@instanode.dev" {
		t.Errorf("Contact = %q", caps.Contact)
	}
	if len(caps.Tiers) != 2 {
		t.Fatalf("len(Tiers) = %d, want 2", len(caps.Tiers))
	}

	anon := caps.Tiers[0]
	if anon.Tier != "anonymous" || anon.DisplayName != "Anonymous" {
		t.Errorf("anon tier mismatch: %+v", anon)
	}
	if anon.StorageLimitMB["postgres"] != 10 {
		t.Errorf("anon postgres storage = %d, want 10", anon.StorageLimitMB["postgres"])
	}
	if anon.ConnectionsLimit["postgres"] != 2 {
		t.Errorf("anon postgres conns = %d, want 2", anon.ConnectionsLimit["postgres"])
	}
	if anon.PaidFromDayOne {
		t.Error("anon PaidFromDayOne = true, want false")
	}
	if anon.IsTerminalTier {
		t.Error("anon IsTerminalTier = true, want false")
	}
	if anon.UpgradeURL == nil || *anon.UpgradeURL != "https://instanode.dev/pricing/" {
		t.Errorf("anon UpgradeURL = %v, want pricing page", anon.UpgradeURL)
	}

	team := caps.Tiers[1]
	if team.Tier != "team" {
		t.Errorf("team.Tier = %q", team.Tier)
	}
	if team.PriceUSDMonthly != 199 || !team.PaidFromDayOne {
		t.Errorf("team price/paid mismatch: %d %v", team.PriceUSDMonthly, team.PaidFromDayOne)
	}
	if team.Deployments != -1 {
		t.Errorf("team Deployments = %d, want -1 (unlimited)", team.Deployments)
	}
	if !team.IsTerminalTier {
		t.Error("team IsTerminalTier = false, want true")
	}
	if team.UpgradeURL != nil {
		t.Errorf("terminal tier UpgradeURL = %v, want nil", team.UpgradeURL)
	}
	if team.BackupRestoreEnabled != true || team.RPOMinutes != 5 || team.RTOMinutes != 15 {
		t.Errorf("team durability fields mismatch: %+v", team)
	}
	if team.AnnualDiscountPercent != 17 {
		t.Errorf("team AnnualDiscountPercent = %d, want 17", team.AnnualDiscountPercent)
	}

	// Forward-compat escape hatch: the unknown field is preserved on Raw.
	if caps.Raw == nil {
		t.Fatal("Raw map is nil, want the full decoded body")
	}
	if caps.Raw["future_unknown_field"] != "tolerated" {
		t.Errorf("Raw[future_unknown_field] = %v, want the forward-compat value preserved",
			caps.Raw["future_unknown_field"])
	}
}

func TestCapabilities_AnonymousMode(t *testing.T) {
	// The endpoint is public — Capabilities must work with no API key set.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("anonymous client should send no Authorization header, got %q", got)
		}
		_, _ = io.WriteString(w, capabilitiesBody)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL)) // no WithAPIKey
	if _, err := c.Capabilities(context.Background()); err != nil {
		t.Fatalf("Capabilities (anon): %v", err)
	}
}

func TestCapabilities_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"ok":false,"error":"plans_unavailable","message":"Tier matrix not loaded"}`)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.Capabilities(context.Background())
	if err == nil {
		t.Fatal("expected error on 503")
	}
	if !IsServiceUnavailable(err) {
		t.Errorf("IsServiceUnavailable should match: %v", err)
	}
}
