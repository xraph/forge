package router

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A blocked result whose Error is nil used to reach the handler: the executor
// returned the nil error, and nil reads as success. Every way of producing
// such a result has to answer 403 and leave the handler alone.
func TestInterceptor_BlockWithNilErrorFailsClosed(t *testing.T) {
	var nilErr error

	allow := InterceptorFromFunc(func(ctx Context, route RouteInfo) InterceptorResult { return Allow() })
	deny := InterceptorFromFunc(func(ctx Context, route RouteInfo) InterceptorResult { return Block(nilErr) })
	rawDeny := InterceptorFromFunc(func(ctx Context, route RouteInfo) InterceptorResult {
		return InterceptorResult{Blocked: true}
	})

	cases := map[string]Interceptor{
		"Block(nil)": deny,
		"BlockWithValues(nil)": InterceptorFromFunc(func(ctx Context, route RouteInfo) InterceptorResult {
			return BlockWithValues(nil, map[string]any{"k": 1})
		}),
		"hand-built result":            rawDeny,
		"Not(allow, nil)":              Not(allow, nil),
		"Or(raw, raw)":                 Or(rawDeny, rawDeny),
		"And(allow, raw)":              And(allow, rawDeny),
		"Parallel(allow, raw)":         Parallel(allow, rawDeny),
		"ParallelAny(raw, raw)":        ParallelAny(rawDeny, rawDeny),
		"When(true, raw)":              When(func(Context, RouteInfo) bool { return true }, rawDeny),
		"Parallel(nested Not nil)":     Parallel(Not(allow, nil)),
		"Or(Not(allow,nil), raw deny)": Or(Not(allow, nil), rawDeny),
	}

	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			var ran atomic.Int32

			r := NewRouter()
			require.NoError(t, r.GET("/admin", func(ctx Context) error {
				ran.Add(1)

				return ctx.String(http.StatusOK, "secret")
			}, WithInterceptor(in)))

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/admin", nil))

			assert.Equal(t, http.StatusForbidden, rec.Code)
			assert.NotContains(t, rec.Body.String(), "secret")
			assert.Zero(t, ran.Load(), "handler must not run")
		})
	}
}

func TestInterceptorChain_BlockWithNilErrorReturnsError(t *testing.T) {
	chain := NewInterceptorChain([]Interceptor{
		InterceptorFromFunc(func(ctx Context, route RouteInfo) InterceptorResult {
			return InterceptorResult{Blocked: true}
		}),
	}, nil)

	err := chain.Execute(nil, RouteInfo{})
	require.Error(t, err)

	var httpErr interface{ StatusCode() int }
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusForbidden, httpErr.StatusCode())
}
