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
