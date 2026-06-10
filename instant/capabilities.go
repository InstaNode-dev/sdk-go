package instant

import (
	"context"
	"encoding/json"
	"fmt"
)

// capabilitiesPath is the public, unauthenticated tier-matrix endpoint.
// GET /api/v1/capabilities lets an agent discover "what can I do at which
// tier" without provisioning-and-failing or scraping llms.txt.
const capabilitiesPath = "/api/v1/capabilities"

// Capabilities is the typed result of GET /api/v1/capabilities — the full
// tier matrix plus the docs + support pointers. The shape is contract-stable;
// see [Client.Capabilities].
type Capabilities struct {
	// OK is always true on success.
	OK bool `json:"ok"`

	// Tiers is the plan matrix in upgrade order (anonymous → team).
	Tiers []TierCapabilities `json:"tiers"`

	// Docs is the LLM-targeted docs URL returned in the envelope.
	Docs string `json:"docs"`

	// Contact is the support/enterprise contact link (a mailto:).
	Contact string `json:"contact"`

	// Raw holds the full decoded JSON so callers can read fields newer than
	// this SDK version without an upgrade. The typed fields above are the
	// stable surface; Raw is the escape hatch for forward compatibility.
	Raw map[string]any `json:"-"`
}

// TierCapabilities describes a single plan tier in the [Capabilities] matrix.
// Unknown/future fields the API adds are preserved on [Capabilities.Raw]; the
// typed fields here are the stable, documented surface.
type TierCapabilities struct {
	// Tier is the machine name (e.g. "anonymous", "hobby", "pro", "team").
	Tier string `json:"tier"`

	// DisplayName is the human-readable tier label.
	DisplayName string `json:"display_name"`

	// PriceUSDMonthly is the monthly price in whole US dollars (0 for free tiers).
	PriceUSDMonthly int `json:"price_usd_monthly"`

	// PaidFromDayOne is true for any tier that bills from signup (no trial).
	PaidFromDayOne bool `json:"paid_from_day_one"`

	// StorageLimitMB maps a service ("postgres", "redis", "mongodb", "queue",
	// "storage", "webhook", "vector") to its per-resource storage cap in MB.
	StorageLimitMB map[string]int `json:"storage_limit_mb"`

	// ConnectionsLimit maps a service to its per-resource connection cap.
	ConnectionsLimit map[string]int `json:"connections_limit"`

	// ResourceCountLimit maps a service to the max number of active resources
	// a team may hold. -1 means unlimited.
	ResourceCountLimit map[string]int `json:"resource_count_limit"`

	// Deployments is the per-tier deployments_apps cap.
	Deployments int `json:"deployments_apps"`

	// BackupRetentionDays is how long automated backups are retained (0 = none).
	BackupRetentionDays int `json:"backup_retention_days"`

	// BackupRestoreEnabled reports whether self-serve restore is offered.
	BackupRestoreEnabled bool `json:"backup_restore_enabled"`

	// ManualBackupsPerDay is the per-day manual-backup quota.
	ManualBackupsPerDay int `json:"manual_backups_per_day"`

	// RPOMinutes / RTOMinutes are the recovery objectives (0 = not promised).
	RPOMinutes int `json:"rpo_minutes"`
	RTOMinutes int `json:"rto_minutes"`

	// AnnualDiscountPercent is the discount of the yearly variant vs 12× monthly.
	AnnualDiscountPercent int `json:"annual_discount_percent"`

	// UpgradeURL is where to upgrade to a higher tier. nil on the terminal
	// (top) tier — there is nothing to upgrade to. Pairs with IsTerminalTier.
	UpgradeURL *string `json:"upgrade_url"`

	// IsTerminalTier is true for the top tier (UpgradeURL is nil when true).
	IsTerminalTier bool `json:"is_terminal_tier"`
}

// Capabilities fetches the full tier matrix from GET /api/v1/capabilities.
//
// The endpoint is public and unauthenticated, so this works in anonymous mode
// (no API key required). Use it to discover tier limits — storage, connection,
// resource-count, and deployment caps per plan — before provisioning, so an
// agent can pick the right tier or warn the user about a limit up front instead
// of provisioning-and-failing.
//
// Forward compatibility: the typed [Capabilities] / [TierCapabilities] fields
// are the stable surface, and the complete decoded JSON is also preserved on
// [Capabilities.Raw] so callers can read fields newer than this SDK release.
//
// Example:
//
//	caps, err := client.Capabilities(ctx)
//	if err != nil { log.Fatal(err) }
//	for _, t := range caps.Tiers {
//	    fmt.Printf("%s: postgres %dMB, %d deploys\n",
//	        t.Tier, t.StorageLimitMB["postgres"], t.Deployments)
//	}
func (c *Client) Capabilities(ctx context.Context) (*Capabilities, error) {
	// One HTTP round-trip: capture the raw JSON, decode it into the typed
	// struct, and keep the complete forward-compatible JSON on Raw so callers
	// can read fields newer than this SDK release. We decode the raw bytes
	// directly into both targets — re-encoding an already-decoded map can never
	// fail, so this avoids a dead, untestable error branch.
	var rawMsg json.RawMessage
	if err := c.get(ctx, capabilitiesPath, &rawMsg); err != nil {
		return nil, fmt.Errorf("Capabilities: %w", err)
	}
	var out Capabilities
	if err := json.Unmarshal(rawMsg, &out); err != nil {
		return nil, fmt.Errorf("Capabilities: decoding response: %w", err)
	}
	out.Raw = map[string]any{}
	_ = json.Unmarshal(rawMsg, &out.Raw) //nolint:errcheck // best-effort; the typed decode above already validated the body
	return &out, nil
}
