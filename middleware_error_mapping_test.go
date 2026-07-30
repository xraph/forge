package forge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge"
)

type meReq struct{}
type meResp struct {
	OK bool `json:"ok"`
}

// post drives a route that either returns err from middleware or from the
// handler, so the two paths can be compared directly.
func post(t *testing.T, fromMiddleware bool, err error) *httptest.ResponseRecorder {
	t.Helper()
	r := forge.NewRouter()

	handler := func(ctx forge.Context, _ *meReq) (*meResp, error) {
		if fromMiddleware {
			return &meResp{OK: true}, nil
		}
		return nil, err
	}

	opts := []forge.RouteOption{}
	if fromMiddleware {
		opts = append(opts, forge.WithMiddleware(func(next forge.Handler) forge.Handler {
			return func(ctx forge.Context) error { return err }
		}))
	}
	if regErr := r.POST("/x", handler, opts...); regErr != nil {
		t.Fatalf("register route: %v", regErr)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/x",
		bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	return rec
}

// An error returned from middleware must produce the same status and body as
// the same error returned from a handler.
//
// Previously the middleware path fell back to http.Error, so a
// forge.Unauthorized became a 500 with a plain-text body. Callers worked
// around it by hand-writing ctx.JSON responses in middleware, which
// duplicates the envelope and drifts from the handler-side shape.
func TestMiddlewareError_MapsLikeHandlerError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"unauthorized", forge.Unauthorized("nope"), http.StatusUnauthorized},
		{"forbidden", forge.Forbidden("denied"), http.StatusForbidden},
		{"bad request", forge.BadRequest("bad"), http.StatusBadRequest},
		{"not found", forge.NotFound("gone"), http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fromHandler := post(t, false, tc.err)
			fromMiddleware := post(t, true, tc.err)

			if fromHandler.Code != tc.want {
				t.Fatalf("handler: got %d, want %d (body %q)",
					fromHandler.Code, tc.want, fromHandler.Body.String())
			}
			if fromMiddleware.Code != tc.want {
				t.Errorf("middleware: got %d, want %d (body %q)",
					fromMiddleware.Code, tc.want, fromMiddleware.Body.String())
			}
			if fromMiddleware.Body.String() != fromHandler.Body.String() {
				t.Errorf("bodies differ:\n  handler:    %q\n  middleware: %q",
					fromHandler.Body.String(), fromMiddleware.Body.String())
			}
		})
	}
}

// The body must be the JSON envelope, not a plain-text dump.
func TestMiddlewareError_ProducesJSONEnvelope(t *testing.T) {
	rec := post(t, true, forge.Unauthorized("nope"))

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("middleware error body is not JSON: %q", rec.Body.String())
	}
	if body["error"] != "nope" {
		t.Errorf("error field: got %v, want %q", body["error"], "nope")
	}
}

// A plain error carries no status, so it must still be a 500 — the fix
// promotes typed errors, it does not swallow untyped ones.
func TestMiddlewareError_UntypedStaysInternalError(t *testing.T) {
	rec := post(t, true, context.DeadlineExceeded)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want 500 (body %q)", rec.Code, rec.Body.String())
	}
}
