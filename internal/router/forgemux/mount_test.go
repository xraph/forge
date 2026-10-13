package forgemux

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mount("/") has to cover the root itself as well as everything under it.
// Trimming the trailing slash leaves an empty base, which used to register
// only "/*", and that wildcard does not match "/".
func TestMux_MountAtRootServesRoot(t *testing.T) {
	m := New()
	m.Mount("/", namedHandler("root"))

	for _, path := range []string{"/", "/docs", "/docs/", "/a/b/c"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			rec := httptest.NewRecorder()
			m.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, path, nil))

			require.Equal(t, http.StatusOK, rec.Code, "%s %s", method, path)
			assert.Equal(t, "root", rec.Body.String(), "%s %s", method, path)
		}
	}
}

func TestMux_MountAtPrefixServesBaseAndSubtree(t *testing.T) {
	for _, mount := range []string{"/api", "/api/"} {
		m := New()
		m.Mount(mount, namedHandler("api"))

		for _, path := range []string{"/api", "/api/", "/api/users/1"} {
			rec := httptest.NewRecorder()
			m.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))

			require.Equal(t, http.StatusOK, rec.Code, "mount %q, path %q", mount, path)
			assert.Equal(t, "api", rec.Body.String())
		}

		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/other", nil))
		assert.Equal(t, http.StatusNotFound, rec.Code, "mount %q must not catch /other", mount)
	}
}
