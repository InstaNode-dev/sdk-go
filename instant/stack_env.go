package instant

// stack_env.go — env-var mutation on an existing stack
// (PATCH /stacks/:slug/env, api/internal/handlers/stack.go UpdateEnv,
// migration 062: stacks.env_vars JSONB).

import (
	"context"
	"fmt"
	"net/url"
)

const (
	// stackPathPrefix is the stack operate-verb endpoint family. The stack
	// slug is appended as a path-escaped segment.
	stackPathPrefix = "/stacks/"

	// stackEnvSuffix is the PATCH env-merge sub-resource.
	stackEnvSuffix = "/env"
)

// StackEnvUpdate is returned by [Client.UpdateStackEnv].
type StackEnvUpdate struct {
	// OK is always true on success.
	OK bool `json:"ok"`

	// Env is the FULL env set on the stack AFTER the merge (values redacted)
	// — the caller does not need to re-GET the stack.
	Env map[string]string `json:"env"`

	// Message reminds the caller that a redeploy is required to apply the
	// change.
	Message string `json:"message,omitempty"`
}

// UpdateStackEnv merges env vars into an existing stack via
// PATCH /stacks/:slug/env.
//
// slug is the stack identifier from [Stack.Slug]. PATCH semantics: the
// incoming map is merged into the stack's existing env_vars under a
// server-side row lock (concurrent PATCHes don't lose updates). Setting a key
// to the EMPTY STRING deletes it. Keys must match the POSIX shape
// [A-Z_][A-Z0-9_]* — the same rule /stacks/new enforces — and the merged
// payload is capped at 64 KiB (413 *APIError beyond that).
//
// Requires a valid API key: anonymous stacks cannot be mutated after
// creation. The change is persisted to the stack row and applied on the next
// stack redeploy.
//
// Example:
//
//	res, err := client.UpdateStackEnv(ctx, stack.Slug, map[string]string{
//	    "LOG_LEVEL": "debug",
//	    "OLD_FLAG":  "", // empty string deletes the key
//	})
//	if err != nil { log.Fatal(err) }
//	fmt.Println(res.Message)
func (c *Client) UpdateStackEnv(ctx context.Context, slug string, env map[string]string) (*StackEnvUpdate, error) {
	if slug == "" {
		return nil, fmt.Errorf("UpdateStackEnv: slug is required")
	}
	if len(env) == 0 {
		return nil, fmt.Errorf("UpdateStackEnv: env must be a non-empty map")
	}
	var out StackEnvUpdate
	path := stackPathPrefix + url.PathEscape(slug) + stackEnvSuffix
	if err := c.patchJSON(ctx, path, envUpdateBody{Env: env}, &out); err != nil {
		return nil, fmt.Errorf("UpdateStackEnv: %w", err)
	}
	return &out, nil
}
