package gateway

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/xraph/forge"
)

// GRPCProxy handles gRPC reverse proxying for both unary and streaming RPCs.
// It works at the HTTP/2 transport layer, forwarding raw gRPC frames between
// client and upstream, preserving headers, trailers, and streaming semantics.
type GRPCProxy struct {
	config    Config
	logger    forge.Logger
	transport *http.Transport
}

// NewGRPCProxy creates a new gRPC reverse proxy engine.
func NewGRPCProxy(config Config, logger forge.Logger) *GRPCProxy {
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: config.TLS.InsecureSkipVerify, //nolint:gosec // user-configured
		NextProtos:         []string{"h2"}, // Force HTTP/2 for gRPC
	}

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   config.Timeouts.Connect,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig:       tlsConfig,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   50,
		IdleConnTimeout:       config.Timeouts.Idle,
		ResponseHeaderTimeout: 0, // Disable for streaming
		ForceAttemptHTTP2:     true,
	}

	return &GRPCProxy{
		config:    config,
		logger:    logger,
		transport: transport,
	}
}

// ProxyGRPC proxies a gRPC request to the upstream target.
// It handles:
//   - gRPC metadata (headers) propagation
//   - gRPC deadline/timeout propagation
//   - Unary, server-streaming, client-streaming, and bidirectional streaming RPCs
//   - gRPC trailers
//   - gRPC status codes
func ProxyGRPC(w http.ResponseWriter, r *http.Request, route *Route, target *Target, config Config, logger forge.Logger) {
	proxy := NewGRPCProxy(config, logger)
	proxy.ServeHTTP(w, r, route, target)
}

// ServeHTTP processes a gRPC proxy request.
func (gp *GRPCProxy) ServeHTTP(w http.ResponseWriter, r *http.Request, route *Route, target *Target) {
	start := time.Now()

	// Validate gRPC content type
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/grpc") {
		http.Error(w, "invalid content type for gRPC", http.StatusUnsupportedMediaType)
		return
	}

	// Build upstream URL
	upstreamURL := target.URL
	if !strings.HasPrefix(upstreamURL, "http") {
		upstreamURL = "https://" + upstreamURL // gRPC typically uses TLS
	}

	// Apply path rewriting
	path := r.URL.Path
	if route.StripPrefix && route.Path != "" {
		prefix := strings.TrimSuffix(route.Path, "/*")
		prefix = strings.TrimSuffix(prefix, "/*")
		path = strings.TrimPrefix(path, prefix)
		if path == "" {
			path = "/"
		}
	}

	if route.AddPrefix != "" {
		path = route.AddPrefix + path
	}

	// Construct the full upstream URL
	targetURL := strings.TrimRight(upstreamURL, "/") + path

	// Create the upstream request
	ctx := r.Context()

	// Propagate gRPC timeout/deadline
	if timeout := extractGRPCTimeout(r); timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	upstreamReq, err := http.NewRequestWithContext(ctx, r.Method, targetURL, r.Body)
	if err != nil {
		gp.logger.Error("failed to create gRPC upstream request",
			forge.F("error", err),
			forge.F("target", target.URL),
		)

		writeGRPCError(w, http.StatusBadGateway, "failed to create upstream request")
		return
	}

	// Copy headers, preserving gRPC metadata
	copyGRPCHeaders(r, upstreamReq)

	// Set proxy headers
	if clientIP, _, splitErr := net.SplitHostPort(r.RemoteAddr); splitErr == nil {
		if prior := r.Header.Get("X-Forwarded-For"); prior != "" {
			upstreamReq.Header.Set("X-Forwarded-For", prior+", "+clientIP)
		} else {
			upstreamReq.Header.Set("X-Forwarded-For", clientIP)
		}
	}

	// Apply route header policies
	applyHeaderPolicy(upstreamReq, route.Headers)
	if route.Transform != nil {
		applyHeaderPolicy(upstreamReq, route.Transform.RequestHeaders)
	}

	target.IncrConns()
	defer target.DecrConns()

	// Execute the upstream request
	resp, err := gp.transport.RoundTrip(upstreamReq)
	if err != nil {
		latency := time.Since(start)
		target.RecordRequest(latency, true)

		gp.logger.Error("gRPC upstream error",
			forge.F("route_id", route.ID),
			forge.F("target_url", target.URL),
			forge.F("error", err),
			forge.F("latency_ms", latency.Milliseconds()),
		)

		writeGRPCError(w, http.StatusBadGateway, fmt.Sprintf("upstream error: %v", err))
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	copyResponseHeaders(w, resp)

	// Set status code
	w.WriteHeader(resp.StatusCode)

	// Stream the response body
	// For gRPC, we need to flush frames as they arrive
	flusher, canFlush := w.(http.Flusher)

	buf := make([]byte, 32*1024) // 32KB buffer for frame streaming
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				gp.logger.Debug("gRPC client write error",
					forge.F("route_id", route.ID),
					forge.F("error", writeErr),
				)
				break
			}

			if canFlush {
				flusher.Flush()
			}
		}

		if readErr != nil {
			if readErr != io.EOF {
				gp.logger.Debug("gRPC upstream read error",
					forge.F("route_id", route.ID),
					forge.F("error", readErr),
				)
			}
			break
		}
	}

	// Copy trailers (gRPC sends important status info in trailers)
	copyGRPCTrailers(w, resp)

	latency := time.Since(start)
	isError := resp.StatusCode >= 400
	target.RecordRequest(latency, isError)
}

// extractGRPCTimeout extracts the gRPC timeout from the request headers.
// The gRPC-Timeout header format is: <value> <unit>
// Units: n=nanoseconds, u=microseconds, m=milliseconds, S=seconds, M=minutes, H=hours
func extractGRPCTimeout(r *http.Request) time.Duration {
	timeoutHeader := r.Header.Get("Grpc-Timeout")
	if timeoutHeader == "" {
		return 0
	}

	if len(timeoutHeader) < 2 {
		return 0
	}

	unit := timeoutHeader[len(timeoutHeader)-1]
	valueStr := timeoutHeader[:len(timeoutHeader)-1]

	var value int
	for _, ch := range valueStr {
		if ch < '0' || ch > '9' {
			return 0
		}
		value = value*10 + int(ch-'0')
	}

	switch unit {
	case 'n':
		return time.Duration(value) * time.Nanosecond
	case 'u':
		return time.Duration(value) * time.Microsecond
	case 'm':
		return time.Duration(value) * time.Millisecond
	case 'S':
		return time.Duration(value) * time.Second
	case 'M':
		return time.Duration(value) * time.Minute
	case 'H':
		return time.Duration(value) * time.Hour
	default:
		return 0
	}
}

// copyGRPCHeaders copies relevant headers from client to upstream request.
// gRPC metadata is transmitted via HTTP/2 headers.
func copyGRPCHeaders(src *http.Request, dst *http.Request) {
	for key, values := range src.Header {
		lowerKey := strings.ToLower(key)

		// Skip hop-by-hop headers
		switch lowerKey {
		case "connection", "keep-alive", "transfer-encoding", "upgrade":
			continue
		}

		for _, v := range values {
			dst.Header.Add(key, v)
		}
	}

	// Ensure critical gRPC headers are set
	dst.Header.Set("Content-Type", src.Header.Get("Content-Type"))

	if te := src.Header.Get("TE"); te != "" {
		dst.Header.Set("TE", te)
	}
}

// copyResponseHeaders copies response headers from upstream to client.
func copyResponseHeaders(w http.ResponseWriter, resp *http.Response) {
	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
}

// copyGRPCTrailers copies gRPC trailers from the upstream response.
// gRPC trailers contain the grpc-status, grpc-message, and custom metadata.
func copyGRPCTrailers(w http.ResponseWriter, resp *http.Response) {
	for key, values := range resp.Trailer {
		for _, v := range values {
			w.Header().Add(http.TrailerPrefix+key, v)
		}
	}
}

// writeGRPCError writes a gRPC-compatible error response.
func writeGRPCError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/grpc")
	w.Header().Set("Grpc-Status", grpcStatusFromHTTP(statusCode))
	w.Header().Set("Grpc-Message", message)
	w.WriteHeader(statusCode)
}

// grpcStatusFromHTTP maps HTTP status codes to gRPC status code strings.
func grpcStatusFromHTTP(httpStatus int) string {
	switch httpStatus {
	case http.StatusOK:
		return "0" // OK
	case http.StatusBadRequest:
		return "3" // INVALID_ARGUMENT
	case http.StatusUnauthorized:
		return "16" // UNAUTHENTICATED
	case http.StatusForbidden:
		return "7" // PERMISSION_DENIED
	case http.StatusNotFound:
		return "5" // NOT_FOUND
	case http.StatusConflict:
		return "6" // ALREADY_EXISTS
	case http.StatusTooManyRequests:
		return "8" // RESOURCE_EXHAUSTED
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return "14" // UNAVAILABLE
	case http.StatusRequestTimeout:
		return "4" // DEADLINE_EXCEEDED
	default:
		return "2" // UNKNOWN
	}
}
