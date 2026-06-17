package instant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// LeadParams holds the fields for an enterprise contact / interest submission.
// Only Email is required; the other fields add context for the instanode.dev team.
type LeadParams struct {
	// Email is the contact address. Required. Must be RFC 5322-compliant; max 254 chars.
	Email string `json:"email"`

	// Name is the contact's full name. Optional — max 128 chars.
	Name string `json:"name,omitempty"`

	// Company is the organisation name. Optional — max 128 chars.
	Company string `json:"company,omitempty"`

	// UseCase describes the scale requirements driving the Enterprise inquiry.
	// Optional — max 1024 chars.
	UseCase string `json:"use_case,omitempty"`
}

// LeadResult is returned by CreateLead on a successful 201 response.
type LeadResult struct {
	// OK is always true on success.
	OK bool `json:"ok"`

	// ID is the UUID of the created enterprise_leads row.
	ID string `json:"id"`
}

// CreateLead submits an enterprise contact / interest form to instanode.dev
// (POST /api/v1/leads). Only params.Email is required.
//
// No authentication is required — anonymous callers are accepted. When called
// with a bearer token (INSTANT_TOKEN / WithToken option), the lead is
// automatically linked to the caller's team so the instanode.dev team can see
// the account's current usage without asking for it.
//
// Use CreateLead when the user needs capacity or features beyond the Pro tier:
// dedicated infrastructure, SAML/SSO, SOC 2 compliance, a custom SLA, or any
// other requirement not met by a self-serve paid plan.
//
// Example:
//
//	lead, err := client.CreateLead(ctx, &instant.LeadParams{
//	    Email:   "cto@acme.com",
//	    Name:    "Alice Smith",
//	    Company: "Acme Corp",
//	    UseCase: "Multi-region Postgres with SOC 2 Type II compliance.",
//	})
//	if err != nil { log.Fatal(err) }
//	fmt.Println("lead ID:", lead.ID)
func (c *Client) CreateLead(ctx context.Context, params *LeadParams) (*LeadResult, error) {
	if params == nil {
		return nil, fmt.Errorf("CreateLead: params must not be nil")
	}
	if params.Email == "" {
		return nil, fmt.Errorf("CreateLead: Email is required")
	}

	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("CreateLead: marshal params: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/leads", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("CreateLead: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Authorization is handled by the client's RoundTripper (authTransport),
	// which injects the Bearer token from c.apiKey when present. No manual
	// header injection needed — the http.Client already has it wired.

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("CreateLead: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		var apiErr APIError
		if decErr := json.NewDecoder(resp.Body).Decode(&apiErr); decErr == nil && apiErr.Code != "" {
			apiErr.StatusCode = resp.StatusCode
			return nil, &apiErr
		}
		return nil, fmt.Errorf("CreateLead: unexpected status %d", resp.StatusCode)
	}

	var result LeadResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("CreateLead: decode response: %w", err)
	}
	return &result, nil
}
