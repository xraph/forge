package dashboard

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/collector"
)

// maxAttrValueLen bounds a single span attribute value. Query strings and user
// agents come straight from the caller and are retained for the whole
// retention window, so they need a ceiling.
const maxAttrValueLen = 256

// truncateAttr shortens s to at most max bytes, ending on a rune boundary and
// marking the cut. Values already within the limit are returned unchanged.
func truncateAttr(s string, max int) string {
	if len(s) <= max {
		return s
	}

	const marker = "..."
	if max <= len(marker) {
		if max <= 0 {
			return ""
		}
		return marker[:max]
	}

	cut := max - len(marker)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}

	return s[:cut] + marker
}

// isDashboardPath reports whether a request path belongs to the dashboard: an
// exact match on basePath, or basePath followed by a "/". This is deliberately
// stricter than the plain-prefix skip logic below it — that logic only decides
// whether to trace a request that is already known to be a dashboard request,
// whereas this decides whether an unrelated sibling route (e.g. a
// "/dashboard-admin" service that gets polled) is allowed to hold the ingest
// gate open, which a bare prefix match would do incorrectly.
func isDashboardPath(path, basePath string) bool {
	return path == basePath || strings.HasPrefix(path, basePath+"/")
}

// TracingMiddleware creates a forge middleware that auto-captures request traces
// and feeds them into the given TraceStore. Only dashboard internals (static
// assets, SSE streams, and bridge calls) are excluded — page navigations and
// API calls are traced so the tracing UI has data out of the box.
func TracingMiddleware(store *collector.TraceStore, basePath string) forge.Middleware {
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

			// Build attributes. http.path and http.host are caller-controlled
			// and unbounded (Go accepts a request line up to MaxHeaderBytes+4096,
			// about 1MB by default), so they are truncated at the point of
			// storage just like the query and user-agent attributes below.
			attrs := map[string]string{
				"http.method": req.Method,
				"http.path":   truncateAttr(req.URL.Path, maxAttrValueLen),
				"http.host":   truncateAttr(req.Host, maxAttrValueLen),
				"protocol":    protocol,
			}
			if req.URL.RawQuery != "" {
				attrs["http.query"] = truncateAttr(req.URL.RawQuery, maxAttrValueLen)
			}
			if ua := req.UserAgent(); ua != "" {
				attrs["http.user_agent"] = truncateAttr(ua, maxAttrValueLen)
			}
			if err != nil {
				attrs["error"] = truncateAttr(err.Error(), maxAttrValueLen)
			}

			span := &collector.SpanView{
				SpanID:     spanID,
				TraceID:    traceID,
				Name:       truncateAttr(req.Method+" "+path, maxAttrValueLen),
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
