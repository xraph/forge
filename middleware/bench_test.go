package middleware

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	forge "github.com/xraph/forge"
)

// benchSink is a ResponseWriter that discards everything, so the benchmarks
// below measure the middleware rather than httptest.ResponseRecorder.
type benchSink struct{ h http.Header }

func (w *benchSink) Header() http.Header {
	if w.h == nil {
		w.h = http.Header{}
	}

	return w.h
}

func (w *benchSink) Write(p []byte) (int, error) { return len(p), nil }
func (w *benchSink) WriteHeader(int)             {}

func benchServe(b *testing.B, h http.Handler, hdr map[string]string) {
	b.Helper()

	req := httptest.NewRequestWithContext(b.Context(), http.MethodGet, "/x", nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}

	w := &benchSink{}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		clear(w.h)
		h.ServeHTTP(w, req)
	}
}

func BenchmarkTimeout(b *testing.B) {
	payload := bytes.Repeat([]byte("x"), 1024)

	for _, on := range []bool{false, true} {
		b.Run(fmt.Sprintf("timeout=%v", on), func(b *testing.B) {
			r := forge.NewRouter()
			_ = r.GET("/x", func(ctx forge.Context) error { return ctx.Bytes(http.StatusOK, payload) })

			var h http.Handler = r
			if on {
				h = Timeout(5*time.Second, nil)(r)
			}

			benchServe(b, h, nil)
		})
	}
}
