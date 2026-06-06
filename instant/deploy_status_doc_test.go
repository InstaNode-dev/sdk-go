package instant_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestDeploymentStatusDocMatchesAPIContract is a registry-honesty guard
// (CLAUDE.md rule 18): rather than re-typing a slice that itself could drift,
// it parses the actual doc comment on Deployment.Status from deploy.go's AST
// and asserts it documents EXACTLY the deployment-status values the API
// contract (OpenAPI DeploymentItem.status enum) emits — no more, no less.
//
// History: the doc comment previously claimed the API emits "queued" and
// "succeeded". Neither is in the OpenAPI enum
// (["building","deploying","healthy","failed","stopped","expired"]) nor in
// any api code path — they were fictional. A developer writing
// `if d.Status == "succeeded"` against that doc would have a branch that
// never fires. This test fails if either ghost status reappears, or if a real
// status documented by the contract is dropped from the comment.
func TestDeploymentStatusDocMatchesAPIContract(t *testing.T) {
	// canonicalStatuses mirrors the OpenAPI DeploymentItem.status enum in the
	// api repo (internal/handlers/openapi.go). This is the contract source of
	// truth; if the api adds a status it must be added here AND to the comment.
	canonicalStatuses := []string{
		"building", "deploying", "healthy", "failed", "stopped", "expired",
	}
	// ghostStatuses are values that MUST NOT appear in the doc — the api never
	// emits them. Keeping them as an explicit deny-list documents the bug we
	// are guarding against.
	ghostStatuses := []string{"queued", "succeeded"}

	doc := deploymentStatusFieldDoc(t)

	for _, s := range canonicalStatuses {
		if !strings.Contains(doc, `"`+s+`"`) {
			t.Errorf("Deployment.Status doc is missing canonical status %q; the api emits it (OpenAPI enum) but the SDK doc no longer documents it", s)
		}
	}
	for _, g := range ghostStatuses {
		if strings.Contains(doc, `"`+g+`"`) {
			// The deny-list explanation sentence ("There is no \"queued\" or
			// \"succeeded\" status") legitimately names the ghosts, so only
			// flag the bullet-list form `"queued"  —`. The em-dash bullet is
			// the documentation-as-real-status shape we forbid.
			for _, line := range strings.Split(doc, "\n") {
				trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "//"))
				if strings.HasPrefix(trimmed, `"`+g+`"`) {
					t.Errorf("Deployment.Status doc reintroduces ghost status %q as a real status (bullet line: %q); the api never emits it", g, trimmed)
				}
			}
		}
	}
}

// deploymentStatusFieldDoc returns the doc comment attached to the Status
// field of the Deployment struct in deploy.go, parsed from source so the test
// reflects the real comment rather than a copy.
func deploymentStatusFieldDoc(t *testing.T) string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "deploy.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse deploy.go: %v", err)
	}

	var doc string
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != "Deployment" {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return true
		}
		for _, field := range st.Fields.List {
			for _, name := range field.Names {
				if name.Name == "Status" && field.Doc != nil {
					doc = field.Doc.Text()
				}
			}
		}
		return false
	})

	if doc == "" {
		t.Fatal("could not locate Deployment.Status field doc comment in deploy.go")
	}
	return doc
}
