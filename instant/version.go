package instant

// SDKVersion is the released SDK version. It is the single source of truth
// for the User-Agent header and any version-reporting surface.
//
// Bump policy:
//   - Patch bumps for bug-fix-only releases (back-compatible).
//   - Minor bumps when adding API surface (new option fields, new helpers).
//   - Major bumps for breaking changes.
//
// CI verifies that — when the head commit carries an annotated git tag of the
// form vX.Y.Z — this constant equals X.Y.Z. The check lives in
// .github/workflows/ci.yml ("verify-version-tag" job).
var SDKVersion = "0.3.0"

// userAgentString returns the User-Agent header value sent on every request.
// Kept as a single helper so the format is consistent everywhere the value
// is needed (client construction + tests).
func userAgentString() string {
	return "instant-go-sdk/" + SDKVersion
}
