package pathspec

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParse_StaticAndColonParams(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		segments []Segment
		params   []string
	}{
		{
			name: "root",
			raw:  "/",
		},
		{
			name:     "single static segment",
			raw:      "/users",
			segments: []Segment{{Kind: KindStatic, Literal: "users"}},
		},
		{
			name: "nested static segments",
			raw:  "/api/v1/users",
			segments: []Segment{
				{Kind: KindStatic, Literal: "api"},
				{Kind: KindStatic, Literal: "v1"},
				{Kind: KindStatic, Literal: "users"},
			},
		},
		{
			name: "colon parameter",
			raw:  "/users/:id",
			segments: []Segment{
				{Kind: KindStatic, Literal: "users"},
				{Kind: KindParam, Name: "id"},
			},
			params: []string{"id"},
		},
		{
			name: "two colon parameters",
			raw:  "/posts/:postId/comments/:commentId",
			segments: []Segment{
				{Kind: KindStatic, Literal: "posts"},
				{Kind: KindParam, Name: "postId"},
				{Kind: KindStatic, Literal: "comments"},
				{Kind: KindParam, Name: "commentId"},
			},
			params: []string{"postId", "commentId"},
		},
		{
			name:     "trailing slash is normalized away",
			raw:      "/users/",
			segments: []Segment{{Kind: KindStatic, Literal: "users"}},
		},
		{
			name: "double slash collapses to root",
			raw:  "//",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.raw)
			require.NoError(t, err)
			require.Equal(t, tt.segments, got.Segments)
			require.Equal(t, tt.params, got.Params)
			require.Equal(t, tt.raw, got.Raw, "Raw must preserve the path exactly as registered")
		})
	}
}

func TestParse_RejectsMalformedPaths(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"empty path", ""},
		{"missing leading slash", "users"},
		{"unnamed colon parameter", "/users/:"},
		{"parameter name starting with a digit", "/users/:1bad"},
		{"parameter name with a hyphen", "/users/:bad-name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.raw)
			require.Error(t, err)
			require.Contains(t, err.Error(), "pathspec: ")
		})
	}
}

func TestParse_BraceParamsAndConstraints(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		segments []Segment
		params   []string
	}{
		{
			name: "brace parameter",
			raw:  "/users/{id}",
			segments: []Segment{
				{Kind: KindStatic, Literal: "users"},
				{Kind: KindParam, Name: "id"},
			},
			params: []string{"id"},
		},
		{
			name: "mixed colon and brace styles",
			raw:  "/users/:userId/posts/{postId}",
			segments: []Segment{
				{Kind: KindStatic, Literal: "users"},
				{Kind: KindParam, Name: "userId"},
				{Kind: KindStatic, Literal: "posts"},
				{Kind: KindParam, Name: "postId"},
			},
			params: []string{"userId", "postId"},
		},
		{
			name: "int constraint",
			raw:  "/users/{id:int}",
			segments: []Segment{
				{Kind: KindStatic, Literal: "users"},
				{Kind: KindParam, Name: "id", Constraint: ConstraintInt},
			},
			params: []string{"id"},
		},
		{
			name: "uuid constraint",
			raw:  "/orders/{id:uuid}",
			segments: []Segment{
				{Kind: KindStatic, Literal: "orders"},
				{Kind: KindParam, Name: "id", Constraint: ConstraintUUID},
			},
			params: []string{"id"},
		},
		{
			name: "enum constraint",
			raw:  "/invoices/{status:enum(draft|sent|paid)}",
			segments: []Segment{
				{Kind: KindStatic, Literal: "invoices"},
				{
					Kind:       KindParam,
					Name:       "status",
					Constraint: ConstraintEnum,
					Enum:       []string{"draft", "sent", "paid"},
				},
			},
			params: []string{"status"},
		},
		{
			name: "single-value enum",
			raw:  "/x/{k:enum(only)}",
			segments: []Segment{
				{Kind: KindStatic, Literal: "x"},
				{Kind: KindParam, Name: "k", Constraint: ConstraintEnum, Enum: []string{"only"}},
			},
			params: []string{"k"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.raw)
			require.NoError(t, err)
			require.Equal(t, tt.segments, got.Segments)
			require.Equal(t, tt.params, got.Params)
		})
	}
}

func TestParse_RejectsBadConstraints(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantErrHas string
	}{
		{"unclosed brace", "/users/{id", "unclosed"},
		{"unknown constraint", "/users/{id:number}", "unknown constraint"},
		{"regex is not supported", "/users/{id:[0-9]+}", "unknown constraint"},
		{"unclosed enum", "/x/{k:enum(a|b}", "unclosed enum("},
		{"empty enum", "/x/{k:enum()}", "empty enum value"},
		{"empty enum member", "/x/{k:enum(a||b)}", "empty enum value"},
		{"unnamed brace parameter", "/users/{}", "unnamed parameter"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.raw)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErrHas)
		})
	}
}

func TestParse_Wildcards(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		segments []Segment
		params   []string
	}{
		{
			name:     "bare wildcard takes the default name",
			raw:      "/*",
			segments: []Segment{{Kind: KindWildcard, Name: DefaultWildcardName}},
			params:   []string{DefaultWildcardName},
		},
		{
			name: "named wildcard",
			raw:  "/static/*path",
			segments: []Segment{
				{Kind: KindStatic, Literal: "static"},
				{Kind: KindWildcard, Name: "path"},
			},
			params: []string{"path"},
		},
		{
			name: "wildcard after parameters",
			raw:  "/{org}/repos/{repo}/files/*",
			segments: []Segment{
				{Kind: KindParam, Name: "org"},
				{Kind: KindStatic, Literal: "repos"},
				{Kind: KindParam, Name: "repo"},
				{Kind: KindStatic, Literal: "files"},
				{Kind: KindWildcard, Name: DefaultWildcardName},
			},
			params: []string{"org", "repo", DefaultWildcardName},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.raw)
			require.NoError(t, err)
			require.Equal(t, tt.segments, got.Segments)
			require.Equal(t, tt.params, got.Params)
		})
	}
}

// A mid-path wildcard has never worked. convertPathToBunRouter rewrote
// "/api/*/assets" into "/api/*filepath/assets", which 404s for every path,
// and passing the raw form to bunrouter panics with "param must have a name".
// Rejecting it at parse time turns both outcomes into a legible error.
func TestParse_RejectsNonTerminalWildcard(t *testing.T) {
	for _, raw := range []string{"/api/*/assets", "/api/*filepath/assets", "/*/x"} {
		t.Run(raw, func(t *testing.T) {
			_, err := Parse(raw)
			require.Error(t, err)
			require.Contains(t, err.Error(), "wildcard")
		})
	}
}

func TestParse_RejectsDuplicateParamNames(t *testing.T) {
	for _, raw := range []string{"/a/{id}/b/{id}", "/a/:id/b/{id}", "/a/{filepath}/b/*"} {
		t.Run(raw, func(t *testing.T) {
			_, err := Parse(raw)
			require.Error(t, err)
			require.Contains(t, err.Error(), "twice")
		})
	}
}
