package instant

import (
	"fmt"
	"regexp"
)

// resourceNamePattern is the server-side validation rule for resource names.
// A name must be 1–64 characters, start with an alphanumeric character, and
// otherwise contain only letters, digits, spaces, underscores, and hyphens.
//
// It mirrors the server contract: omitting or sending an invalid name on any
// provisioning endpoint results in an HTTP 400 response.
var resourceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _-]*$`)

// validateResourceName checks that name satisfies the server-side resource-name
// contract (1–64 chars, ^[A-Za-z0-9][A-Za-z0-9 _-]*$). It returns a descriptive
// error when name is empty or malformed so the caller fails fast, before the
// network round-trip, instead of receiving an opaque HTTP 400.
func validateResourceName(name string) error {
	if name == "" {
		return fmt.Errorf("name is required (1-64 chars, must match %s)", resourceNamePattern.String())
	}
	if len(name) > 64 {
		return fmt.Errorf("name must be 1-64 characters, got %d", len(name))
	}
	if !resourceNamePattern.MatchString(name) {
		return fmt.Errorf("name %q is invalid: must match %s", name, resourceNamePattern.String())
	}
	return nil
}

// provisionBody validates opts and returns the JSON request body shared by
// every Provision* method. opts must be non-nil and carry a valid Name; the
// resource name is REQUIRED on all provisioning endpoints.
func provisionBody(opts *ProvisionOpts) (map[string]string, error) {
	if opts == nil {
		return nil, fmt.Errorf("opts is required: a non-nil *ProvisionOpts with a valid Name must be supplied")
	}
	if err := validateResourceName(opts.Name); err != nil {
		return nil, err
	}
	return map[string]string{"name": opts.Name}, nil
}

// provisionHeaders returns the extra HTTP headers — currently just the
// optional Idempotency-Key — that the Provision* helpers attach to each
// request. Returns nil when opts.IdempotencyKey is empty so callers don't
// pay for an empty header send.
func provisionHeaders(opts *ProvisionOpts) map[string]string {
	if opts == nil || opts.IdempotencyKey == "" {
		return nil
	}
	return map[string]string{"Idempotency-Key": opts.IdempotencyKey}
}
