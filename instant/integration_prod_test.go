//go:build integration

package instant_test

// Live integration tests against the PROD instanode.dev API.
//
// Unlike integration_test.go (which targets a local k8s cluster via
// INSTANT_API_URL and provisions real resources), this file pins the public
// production host and exercises only READ-ONLY, side-effect-free surfaces:
//
//   - an unauthenticated GET /api/v1/resources, which the API rejects with the
//     canonical agent-native error envelope — proving the APIError fix
//     (agent_action + error_code + upgrade_url survive the round trip), and
//   - GET /api/v1/capabilities, the public tier matrix — proving Capabilities()
//     decodes the live response and returns tiers.
//
// Run:
//
//	go test ./instant/... -tags integration -v -run TestProdIntegration
//
// INSTANODE_PROD_API_URL overrides the host (defaults to the public prod host).
// The suite skips cleanly if the host is unreachable so CI on a network-isolated
// runner is not a hard failure.

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/InstaNode-dev/sdk-go/instant"
)

// defaultProdAPIURL is the canonical public production backend host. The fix
// being verified is the agent-native error envelope, which is a prod contract —
// so this test pins prod rather than a local cluster.
const defaultProdAPIURL = "https://api.instanode.dev"

// prodClient returns a Client pointed at the prod host, or skips the test if the
// host is unreachable. No API key is set: the resources call is meant to 401, and
// capabilities is public.
func prodClient(t *testing.T) (*instant.Client, string) {
	t.Helper()

	apiURL := os.Getenv("INSTANODE_PROD_API_URL")
	if apiURL == "" {
		apiURL = defaultProdAPIURL
	}

	// Probe /healthz to give a clean skip when prod is unreachable from this
	// runner (e.g. an air-gapped CI box) rather than a hard failure.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+"/healthz", nil)
	if err != nil {
		t.Skipf("cannot build healthz request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skipf("prod instanode.dev unreachable at %s: %v", apiURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("prod instanode.dev returned %d from /healthz", resp.StatusCode)
	}

	return instant.New(instant.WithBaseURL(apiURL)), apiURL
}

// TestProdIntegration_APIErrorEnvelope hits an erroring endpoint on PROD with no
// credentials and asserts the SDK's APIError carries the full agent-native
// envelope: a machine-readable error_code (via CanonicalCode) AND a non-empty
// agent_action AND an upgrade_url. This proves the envelope fix against the real
// production response, not a mock.
func TestProdIntegration_APIErrorEnvelope(t *testing.T) {
	client, apiURL := prodClient(t)
	ctx := context.Background()

	// Unauthenticated list — RequireAuth rejects with the canonical envelope.
	_, err := client.ListResources(ctx)
	if err == nil {
		t.Fatalf("ListResources against %s with no credentials: want an error, got nil", apiURL)
	}

	var apiErr *instant.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not *instant.APIError: %T: %v", err, err)
	}

	t.Logf("prod APIError: status=%d code=%q error_code=%q canonical=%q agent_action=%q upgrade_url=%q request_id=%q",
		apiErr.StatusCode, apiErr.Code, apiErr.ErrorCode, apiErr.CanonicalCode(),
		apiErr.AgentAction, apiErr.UpgradeURL, apiErr.RequestID)

	// The auth-required surface must be a 401 (or 403 if the host re-shapes it).
	if apiErr.StatusCode != http.StatusUnauthorized && apiErr.StatusCode != http.StatusForbidden {
		t.Errorf("StatusCode = %d, want 401 or 403 for an unauthenticated list", apiErr.StatusCode)
	}

	// CanonicalCode must resolve to a machine-readable code (error_code preferred,
	// error category fallback). Empty means the envelope was dropped.
	if apiErr.CanonicalCode() == "" {
		t.Error("CanonicalCode() is empty — error envelope (error/error_code) was not captured")
	}

	// agent_action is the LLM-ready next step — the core of the agent-native
	// contract. The fix exists to stop this being dropped.
	if apiErr.AgentAction == "" {
		t.Error("AgentAction is empty — agent_action was dropped from the prod envelope")
	}

	// upgrade_url points the user at claim/login to clear the auth error.
	if apiErr.UpgradeURL == "" {
		t.Error("UpgradeURL is empty — upgrade_url was dropped from the prod envelope")
	}
}

// TestProdIntegration_Capabilities calls Capabilities() against PROD and asserts
// the public tier matrix decodes and returns tiers — proving the new
// Capabilities() surface works against the live, unauthenticated endpoint.
func TestProdIntegration_Capabilities(t *testing.T) {
	client, apiURL := prodClient(t)
	ctx := context.Background()

	caps, err := client.Capabilities(ctx)
	if err != nil {
		t.Fatalf("Capabilities against %s: %v", apiURL, err)
	}

	if !caps.OK {
		t.Error("Capabilities.OK = false, want true")
	}
	if len(caps.Tiers) == 0 {
		t.Fatal("Capabilities returned zero tiers — tier matrix did not decode")
	}

	// Sanity: the anonymous tier should be present at the head of the upgrade
	// order, and at least one tier should name a postgres storage limit.
	sawAnonymous := false
	sawPostgresLimit := false
	for _, tier := range caps.Tiers {
		if tier.Tier == "anonymous" {
			sawAnonymous = true
		}
		if _, ok := tier.StorageLimitMB["postgres"]; ok {
			sawPostgresLimit = true
		}
		t.Logf("tier=%s display=%q price=$%d/mo postgres=%dMB deploys=%d",
			tier.Tier, tier.DisplayName, tier.PriceUSDMonthly,
			tier.StorageLimitMB["postgres"], tier.Deployments)
	}

	if !sawAnonymous {
		t.Error("no 'anonymous' tier in the prod capabilities matrix")
	}
	if !sawPostgresLimit {
		t.Error("no tier reported a postgres storage limit — matrix shape unexpected")
	}
}
