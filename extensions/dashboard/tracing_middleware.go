package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/collector"
)

// TracingMiddleware creates a forge middleware that auto-captures request traces
// and feeds them into the given TraceStore. Only dashboard internals (static
// assets, SSE streams, and bridge calls) are excluded — page navigations and
// API calls are traced so the tracing UI has data out of the box.
func TracingMiddleware(store *collector.TraceStore, basePath string) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			req := ctx.Request()
			path := req.URL.Path

			// Skip dashboard internal plumbing — these are high-frequency,
			// low-value requests that would create noise in the trace list.
			if strings.HasPrefix(path, basePath) {
				if strings.HasPrefix(path, basePath+"/static/") ||
					strings.HasPrefix(path, basePath+"/sse") ||
					strings.HasPrefix(path, basePath+"/bridge/") {
					return next(ctx)
				}
				// Dashboard page navigations and API calls fall through
				// and are traced normally.
			}

			// Skip static assets (outside dashboard) and SSE streams.
			if strings.Contains(path, "/static/") ||
				strings.HasSuffix(path, ".css") ||
				strings.HasSuffix(path, ".js") ||
				strings.HasSuffix(path, ".ico") ||
				strings.HasSuffix(path, ".png") ||
				strings.HasSuffix(path, ".jpg") ||
				strings.HasSuffix(path, ".svg") ||
				strings.HasSuffix(path, ".woff2") ||
				strings.HasSuffix(path, ".woff") ||
				strings.Contains(path, "/sse") {
				return next(ctx)
			}

			traceID := fmt.Sprintf("%016x", time.Now().UnixNano())
			spanID := fmt.Sprintf("%08x", time.Now().UnixNano()&0xFFFFFFFF)
			start := time.Now()

			// Determine protocol from request/path.
			protocol := inferRequestProtocol(req.Header.Get("Upgrade"), path)

			// Execute the handler.
			err := next(ctx)

			end := time.Now()

			status := collector.SpanStatusOK
			if err != nil {
				status = collector.SpanStatusError
			}

			// Build attributes.
			attrs := map[string]string{
				"http.method": req.Method,
				"http.path":   path,
				"http.host":   req.Host,
				"protocol":    protocol,
			}
			if req.URL.RawQuery != "" {
				attrs["http.query"] = req.URL.RawQuery
			}
			if ua := req.UserAgent(); ua != "" {
				attrs["http.user_agent"] = ua
			}
			if err != nil {
				attrs["error"] = err.Error()
			}

			span := &collector.SpanView{
				SpanID:     spanID,
				TraceID:    traceID,
				Name:       req.Method + " " + path,
				Kind:       collector.SpanKindServer,
				Status:     status,
				StartTime:  start,
				EndTime:    end,
				Duration:   end.Sub(start),
				Attributes: attrs,
				Events:     []collector.SpanEventView{},
			}

			store.AddSpan(span)

			return err
		}
	}
}

// inferRequestProtocol determines the protocol type from request metadata.
func inferRequestProtocol(upgradeHeader, path string) string {
	if strings.EqualFold(upgradeHeader, "websocket") {
		return "WS"
	}
	if strings.Contains(path, "/sse") || strings.Contains(path, "/events") {
		return "SSE"
	}
	return "REST"
}
