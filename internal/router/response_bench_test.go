package router

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type benchSensitiveItem struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Password string `json:"password" sensitive:"true"`
}

type benchSensitiveList struct {
	Items []benchSensitiveItem `json:"items"`
}

type benchSensitiveReq struct {
	Slug string `json:"slug"`
}

// BenchmarkOpinionatedHandler_SensitiveCleaning serves a typed handler that
// returns a 100 item list, with and without WithSensitiveFieldCleaning. The
// response used to be processed twice, once by the router and again inside
// ctx.JSON, so with cleaning on the whole list was deep-copied twice.
func BenchmarkOpinionatedHandler_SensitiveCleaning(b *testing.B) {
	items := make([]benchSensitiveItem, 100)
	for i := range items {
		items[i] = benchSensitiveItem{ID: i, Name: "n", Password: "p"}
	}

	body := []byte(`{"slug":"a"}`)

	for _, clean := range []bool{false, true} {
		b.Run(fmt.Sprintf("sensitive=%v", clean), func(b *testing.B) {
			r := NewRouter()

			var opts []RouteOption
			if clean {
				opts = append(opts, WithSensitiveFieldCleaning())
			}

			if err := r.POST("/x", func(ctx Context, req *benchSensitiveReq) (*benchSensitiveList, error) {
				return &benchSensitiveList{Items: items}, nil
			}, opts...); err != nil {
				b.Fatal(err)
			}

			rd := bytes.NewReader(body)
			req := httptest.NewRequestWithContext(b.Context(), http.MethodPost, "/x", nil)
			req.Header.Set("Content-Type", "application/json")

			w := &benchWriter{}

			b.ReportAllocs()

			for b.Loop() {
				clear(w.h)
				rd.Reset(body)
				req.Body = nopCloser{rd}
				req.ContentLength = int64(len(body))
				r.ServeHTTP(w, req)
			}
		})
	}
}
