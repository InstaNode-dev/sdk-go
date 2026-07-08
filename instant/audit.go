package instant

// audit.go — customer-facing audit log export (GET /api/v1/audit).
//
// Every team action — resource provisioned, deployment started, vault key
// written, billing event received — is recorded as an immutable audit row.
// This client method makes that log queryable from Go: stream it into a SIEM,
// build a compliance report, or inspect recent activity in CI.
//
// Tier gate (enforced server-side; client surfaces the *APIError):
//
//	anonymous / free  → 402 upgrade_required
//	hobby             → 30-day lookback
//	hobby_plus        → 60-day lookback
//	pro               → 90-day lookback
//	growth / team     → unlimited history
//
// Pagination: the response includes a NextCursor timestamp. Pass it as
// Before on the next call to page backward in time. An empty NextCursor
// means no more rows match the current filters.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// AuditEvent is one row from the team's audit log.
type AuditEvent struct {
	// ID is the unique UUID for this audit event.
	ID string `json:"id"`

	// Kind is the event category, e.g. "resource.created", "deploy.healthy",
	// "billing.upgraded". See the OpenAPI spec for the full kind catalogue.
	Kind string `json:"kind"`

	// CreatedAt is when the event was recorded (UTC).
	CreatedAt time.Time `json:"created_at"`

	// Actor is the high-level action source: "user", "system", or "agent".
	Actor string `json:"actor"`

	// ActorUserID is the UUID of the user who triggered the event.
	// Nil when the event was emitted by the system or an API key with
	// no associated user session.
	ActorUserID *string `json:"actor_user_id"`

	// ActorEmailMasked is the actor's email address with the local-part
	// partially redacted (e.g. "a***@example.com"). Nil when ActorUserID is nil.
	ActorEmailMasked *string `json:"actor_email_masked"`

	// ResourceID is the UUID of the resource this event relates to.
	// Nil for team-scoped events (billing, settings) that have no single resource.
	ResourceID *string `json:"resource_id"`

	// ResourceType is the kind of resource ("postgres", "redis", "deployment", …).
	// Empty when ResourceID is nil.
	ResourceType string `json:"resource_type"`

	// Summary is a human-readable one-line description of what happened,
	// safe to display in a dashboard or log.
	Summary string `json:"summary"`

	// Metadata holds event-specific structured data. The shape varies by Kind.
	// Use RawMetadata to access the raw JSON if the typed fields are insufficient.
	Metadata json.RawMessage `json:"metadata"`
}

// AuditEventList is the paginated response from [Client.ListAuditEvents] and
// [Client.ListAuditEventsPage].
type AuditEventList struct {
	// OK is always true on success.
	OK bool `json:"ok"`

	// Items is the page of audit events, newest first.
	Items []AuditEvent `json:"items"`

	// TotalReturned is the number of items in this response.
	TotalReturned int `json:"total_returned"`

	// NextCursor is the opaque cursor for the next page. Pass it as
	// [ListAuditEventsOpts.Before] to retrieve older events. Empty when
	// there are no more pages.
	NextCursor string `json:"next_cursor,omitempty"`

	// LookbackDays is the maximum age of events the team's plan permits.
	// -1 means unlimited (growth / team plans). 0 means the tier cannot
	// access audit logs (this value only appears in successful responses,
	// so 0 should not occur in practice).
	LookbackDays int `json:"lookback_days"`

	// Tier is the team's plan tier that determined the lookback window.
	Tier string `json:"tier"`
}

// ListAuditEventsOpts controls filtering and pagination for audit log queries.
type ListAuditEventsOpts struct {
	// Limit is the maximum number of events to return (1–200). Default 50.
	Limit int

	// Kind filters results to a single event kind, e.g. "resource.created".
	// Empty string returns all kinds.
	Kind string

	// Before returns only events older than this timestamp (exclusive).
	// Use the NextCursor from a previous response for keyset pagination.
	Before time.Time

	// Since returns only events at or after this timestamp.
	Since time.Time

	// Until returns only events at or before this timestamp.
	Until time.Time
}

const auditPath = "/api/v1/audit"

// ListAuditEvents returns the most recent page of audit events for the
// authenticated team. Requires a valid API key (Bearer token).
//
// Equivalent to calling [Client.ListAuditEventsPage] with zero-value opts.
//
// Example:
//
//	list, err := client.ListAuditEvents(ctx)
//	if err != nil { log.Fatal(err) }
//	for _, ev := range list.Items {
//	    fmt.Printf("%s  %-32s  %s\n", ev.CreatedAt.Format(time.RFC3339), ev.Kind, ev.Summary)
//	}
func (c *Client) ListAuditEvents(ctx context.Context) (*AuditEventList, error) {
	return c.ListAuditEventsPage(ctx, ListAuditEventsOpts{})
}

// ListAuditEventsPage returns one page of audit events honouring the supplied
// filter options. Requires a valid API key (Bearer token).
//
// Results are ordered newest-first. Use NextCursor as opts.Before to paginate:
//
//	opts := instant.ListAuditEventsOpts{Limit: 100}
//	for {
//	    page, err := client.ListAuditEventsPage(ctx, opts)
//	    if err != nil { return err }
//	    for _, ev := range page.Items { process(ev) }
//	    if page.NextCursor == "" { break }
//	    // NextCursor is an RFC3339 timestamp — assign directly to Before.
//	    t, _ := time.Parse(time.RFC3339Nano, page.NextCursor)
//	    opts.Before = t
//	}
func (c *Client) ListAuditEventsPage(ctx context.Context, opts ListAuditEventsOpts) (*AuditEventList, error) {
	q := url.Values{}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Kind != "" {
		q.Set("kind", opts.Kind)
	}
	if !opts.Before.IsZero() {
		q.Set("before", opts.Before.UTC().Format(time.RFC3339))
	}
	if !opts.Since.IsZero() {
		q.Set("since", opts.Since.UTC().Format(time.RFC3339))
	}
	if !opts.Until.IsZero() {
		q.Set("until", opts.Until.UTC().Format(time.RFC3339))
	}

	path := auditPath
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}

	var raw AuditEventList
	if err := c.get(ctx, path, &raw); err != nil {
		return nil, fmt.Errorf("ListAuditEvents: %w", err)
	}
	return &raw, nil
}
