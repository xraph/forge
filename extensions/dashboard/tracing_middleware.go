package dashboard

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/collector"
)

// maxAttrValueLen bounds a single span attribute value. Query strings and user
// agents come straight from the caller and are retained for the whole
// retention window, so they need a ceiling.
const maxAttrValueLen = collector.MaxAttrValueLen
const maxTraceBodyBytes = 4096

type replayBody struct {
	io.Reader
	io.Closer
}

func safeTraceHeaders(headers http.Header, names ...string) map[string]string {
	out := make(map[string]string)

	for _, name := range names {
		if value := headers.Get(name); value != "" {
			out[http.CanonicalHeaderKey(name)] = truncateAttr(value, maxAttrValueLen)
		}
	}

	return out
}

func redactTraceJSON(value any) any {
	switch data := value.(type) {
	case map[string]any:
		for key, child := range data {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "credential") || strings.Contains(lower, "authorization") || strings.Contains(lower, "cookie") || strings.Contains(lower, "api_key") || strings.Contains(lower, "apikey") {
				data[key] = "[redacted]"
			} else {
				data[key] = redactTraceJSON(child)
			}
		}
	case []any:
		for i, child := range data {
			data[i] = redactTraceJSON(child)
		}
	}

	return value
}

func captureRequestJSON(req *http.Request) string {
	if req.Body == nil || req.ContentLength <= 0 || req.ContentLength > maxTraceBodyBytes || !strings.Contains(strings.ToLower(req.Header.Get("Content-Type")), "application/json") {
		return ""
	}

	original := req.Body
	raw, err := io.ReadAll(io.LimitReader(original, maxTraceBodyBytes+1))

	req.Body = &replayBody{Reader: io.MultiReader(bytes.NewReader(raw), original), Closer: original}
	if err != nil || len(raw) > maxTraceBodyBytes {
		return ""
	}

	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}

	encoded, err := json.Marshal(redactTraceJSON(value))
	if err != nil || len(encoded) > maxTraceBodyBytes {
		return ""
	}

	return string(encoded)
}

// truncateAttr shortens s to at most limit bytes, ending on a rune boundary and
// marking the cut. Values already within the limit are returned unchanged.
func truncateAttr(s string, limit int) string {
	return collector.Truncate(s, limit)
}

// isDashboardPath reports whether a request path belongs to the dashboard: an
// exact match on basePath, or basePath followed by a "/". This is deliberately
// stricter than collector.SkipTracePath, which only decides whether to trace a
// request that is already known to be a dashboard request, whereas this decides whether an unrelated sibling route (e.g. a
// "/dashboard-admin" service that gets polled) is allowed to hold the ingest
// gate open, which a bare prefix match would do incorrectly.
func isDashboardPath(path, basePath string) bool {
	return path == basePath || strings.HasPrefix(path, basePath+"/")
}

// TracingMiddleware stamps dashboard activity and captures each request's
// allowlisted headers, plus its redacted JSON body when captureRequestBody is
// true, for the trace viewer. It creates no spans: the app's tracing does,
// and the dashboard's span processor joins this capture to the server span by
// span ID. Nothing captured here is put on the OTel span, so headers and
// bodies never reach your tracing backend.
func TracingMiddleware(store *collector.TraceStore, basePath string, captureRequestBody ...bool) forge.Middleware {
	capture := len(captureRequestBody) > 0 && captureRequestBody[0]

	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			req := ctx.Request()
			path := req.URL.Path

			// Any dashboard request, including static assets and SSE, counts as
			// someone looking. This is what holds the ingest gate open; see the
			// gate installed in extension.go.
			if isDashboardPath(path, basePath) {
				store.MarkAccessed()
			}

			if collector.SkipTracePath(path, basePath) || !store.Accepting() {
				return next(ctx)
			}

			// A span that isn't recording never reaches the processor, so a
			// capture for it would only wait out its TTL.
			span := trace.SpanFromContext(ctx.Context())
			if !span.IsRecording() {
				return next(ctx)
			}

			exchange := &collector.HTTPExchange{
				Request: collector.HTTPMessage{Headers: safeTraceHeaders(req.Header, "Accept", "Content-Type", "Content-Length", "User-Agent", "X-Request-ID", "Traceparent")},
			}
			if capture {
				exchange.Request.Body = captureRequestJSON(req)
			}

			err := next(ctx)

			exchange.Response.Headers = safeTraceHeaders(ctx.Response().Header(), "Content-Type", "Content-Length", "Cache-Control", "X-Request-ID")
			store.AttachExchange(span.SpanContext().SpanID().String(), exchange)

			return err
		}
	}
}
