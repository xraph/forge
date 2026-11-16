package middleware

import (
	"bytes"
	"compress/gzip"
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

func BenchmarkRateLimiterParallel(b *testing.B) {
	rl := NewRateLimiter(1_000_000_000, 1_000_000_000)
	defer rl.Stop()

	keys := make([]string, 1024)
	for i := range keys {
		keys[i] = fmt.Sprintf("10.0.%d.%d", i/256, i%256)
	}

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			rl.Allow(keys[i&1023])
			i++
		}
	})
}

func BenchmarkCompress(b *testing.B) {
	// 1000 B sits just under the default minimum size, so after the fix it is
	// sent uncompressed. 8000 B is compressed either way and isolates the cost
	// of setting up a gzip.Writer.
	for _, n := range []int{40, 320} {
		payload := bytes.Repeat([]byte(`{"id":1,"name":"widget"},`), n)

		for _, on := range []bool{false, true} {
			b.Run(fmt.Sprintf("size=%d/compress=%v", len(payload), on), func(b *testing.B) {
				r := forge.NewRouter()
				if on {
					r.Use(Compress(gzip.DefaultCompression))
				}

				_ = r.GET("/x", func(ctx forge.Context) error { return ctx.Bytes(http.StatusOK, payload) })

				benchServe(b, r, map[string]string{"Accept-Encoding": "gzip"})
			})
		}
	}
}
