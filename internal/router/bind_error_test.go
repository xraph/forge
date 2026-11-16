package router

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type bindTransferReq struct {
	TenantID string `path:"tenantId"`
	UserID   string `header:"X-User-Id"`
	Amount   int    `json:"amount"`
	Name     string `json:"name"        validate:"required"`
}

type bindTransferResp struct {
	OK bool `json:"ok"`
}

func serveBind(t *testing.T, opts []RouteOption, body string) *httptest.ResponseRecorder {
	t.Helper()

	r := NewRouter()
	require.NoError(t, r.POST("/t/:tenantId", func(ctx Context, req *bindTransferReq) (*bindTransferResp, error) {
		return &bindTransferResp{OK: true}, nil
	}, opts...))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/t/a", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", "u")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	return rec
}

// Bind failures used to be sent back as "invalid request: <raw error>",
// which named Go types and struct fields and turned 413 into 400.
func TestBindError_DoesNotLeakGoTypes(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		opts     []RouteOption
		status   int
		contains string
	}{
		{
			name:     "wrong type names the JSON field only",
			body:     `{"amount":"x","name":"n"}`,
			status:   http.StatusBadRequest,
			contains: `field \"amount\" has the wrong type (got JSON string)`,
		},
		{
			name:     "syntax error is generic",
			body:     `{"amount":`,
			status:   http.StatusBadRequest,
			contains: "malformed JSON",
		},
		{
			name:     "bad token is generic",
			body:     `{"amount": nope}`,
			status:   http.StatusBadRequest,
			contains: "malformed JSON",
		},
		{
			name:     "body over the cap is 413",
			body:     `{"name":"` + strings.Repeat("a", 4096) + `"}`,
			opts:     []RouteOption{WithMaxBodySize(1024)},
			status:   http.StatusRequestEntityTooLarge,
			contains: "request body too large",
		},
		{
			name:     "validation errors stay descriptive",
			body:     `{"amount":1}`,
			status:   http.StatusBadRequest,
			contains: "name",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveBind(t, tc.opts, tc.body)
			body := rec.Body.String()

			assert.Equal(t, tc.status, rec.Code, body)
			assert.Contains(t, body, tc.contains)

			for _, leak := range []string{"Go struct", "Go value", "bindTransferReq", "int", "router.", "failed to"} {
				assert.NotContains(t, body, leak)
			}
		})
	}
}

func TestBindError_UnknownErrorsAreGeneric(t *testing.T) {
	r := NewRouter()
	require.NoError(t, r.POST("/t/:tenantId", func(ctx Context, req *bindTransferReq) (*bindTransferResp, error) {
		return &bindTransferResp{OK: true}, nil
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/t/a", strings.NewReader(`<x/>`))
	req.Header.Set("Content-Type", "text/plain")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"invalid request"`)
	assert.NotContains(t, rec.Body.String(), "content type")
}
