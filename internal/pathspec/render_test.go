package pathspec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These rows are the compatibility contract previously pinned on
// convertPathToBunRouter at internal/router/bunrouter_test.go:120. bunrouter
// and httprouter both speak this dialect, and both need a NAMED terminal
// wildcard.
func TestRender_ColonMatchesTheBunRouterContract(t *testing.T) {
	tests := []struct {
		in, want, desc string
	}{
		{"/users/{id}", "/users/:id", "brace parameter converts to colon"},
		{"/posts/{postId}/comments/{commentId}", "/posts/:postId/comments/:commentId", "multiple brace parameters"},
		{"/{category}/{id}", "/:category/:id", "consecutive brace parameters"},
		{"/callback/{provider}", "/callback/:provider", "single brace parameter"},
		{"/users/:userId/posts/{postId}", "/users/:userId/posts/:postId", "mixed styles"},
		{"/static", "/static", "no parameters"},
		{"/api/users", "/api/users", "no parameters, multiple segments"},
		{"/api/auth/dashboard/static/*", "/api/auth/dashboard/static/*filepath", "unnamed wildcard at end"},
		{"/files/*", "/files/*filepath", "simple unnamed wildcard"},
		{"/*", "/*filepath", "root wildcard"},
		{"/static/*path", "/static/*path", "already named wildcard"},
		{"/api/*filepath", "/api/*filepath", "already named with filepath"},
		{"/api/{id}/sub/*", "/api/:id/sub/*filepath", "parameter and wildcard"},
		{"/{org}/repos/{repo}/files/*", "/:org/repos/:repo/files/*filepath", "multiple parameters and wildcard"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			p, err := Parse(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, p.Render(SyntaxColon))
		})
	}
}

func TestRender_BraceAndOpenAPI(t *testing.T) {
	tests := []struct {
		in          string
		wantBrace   string
		wantOpenAPI string
	}{
		{"/", "/", "/"},
		{"/users", "/users", "/users"},
		{"/users/:id", "/users/{id}", "/users/{id}"},
		{"/users/{id:int}", "/users/{id}", "/users/{id}"},
		{"/invoices/{status:enum(draft|sent)}", "/invoices/{status}", "/invoices/{status}"},
		{"/files/*", "/files/*", "/files/{filepath}"},
		{"/files/*path", "/files/*", "/files/{path}"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			p, err := Parse(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.wantBrace, p.Render(SyntaxBrace), "brace")
			assert.Equal(t, tt.wantOpenAPI, p.Render(SyntaxOpenAPI), "openapi")
		})
	}
}

// Constraints are dropped by every dialect. No backend other than the future
// forgemux matcher can express them.
func TestRender_DropsConstraints(t *testing.T) {
	p, err := Parse("/users/{id:uuid}")
	require.NoError(t, err)

	assert.Equal(t, "/users/:id", p.Render(SyntaxColon))
	assert.Equal(t, "/users/{id}", p.Render(SyntaxBrace))
}
