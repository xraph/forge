package layouts

import "testing"

func TestExtractContributorName(t *testing.T) {
	t.Helper()

	cases := []struct {
		name     string
		path     string
		basePath string
		want     string
	}{
		{
			name:     "local ext prefix with subpath",
			path:     "/dashboard/ext/authsome/pages/users",
			basePath: "/dashboard",
			want:     "authsome",
		},
		{
			name:     "local ext prefix root pages",
			path:     "/dashboard/ext/authsome/pages",
			basePath: "/dashboard",
			want:     "authsome",
		},
		{
			name:     "remote prefix with subpath",
			path:     "/dashboard/remote/authsome/pages/users",
			basePath: "/dashboard",
			want:     "authsome",
		},
		{
			name:     "remote prefix root pages",
			path:     "/dashboard/remote/authsome/pages",
			basePath: "/dashboard",
			want:     "authsome",
		},
		{
			name:     "empty basePath, ext prefix",
			path:     "/ext/foo/pages/x",
			basePath: "",
			want:     "foo",
		},
		{
			name:     "empty basePath, remote prefix",
			path:     "/remote/foo/pages/x",
			basePath: "",
			want:     "foo",
		},
		{
			name:     "non-extension path returns empty",
			path:     "/dashboard/health",
			basePath: "/dashboard",
			want:     "",
		},
		{
			name:     "empty contributor segment",
			path:     "/dashboard/ext//pages",
			basePath: "/dashboard",
			want:     "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractContributorName(tc.path, tc.basePath)
			if got != tc.want {
				t.Fatalf("extractContributorName(%q, %q) = %q; want %q", tc.path, tc.basePath, got, tc.want)
			}
		})
	}
}
