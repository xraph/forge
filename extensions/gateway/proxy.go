package gateway

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// ProxyEngine handles HTTP reverse proxying to upstream targets.
type ProxyEngine struct {
	config Config
	logger forge.Logger
	lb     LoadBalancer
	cbm    *CircuitBreakerManager
	hm     *HealthMonitor
	rm     *RouteManager
	rl     *RateLimiter
	stats  *StatsCollector
	hooks  *HookEngine

	// Optional components (set after construction)
	auth   *GatewayAuth
	cache  *ResponseCache
	tlsMgr *TLSManager

	transport *http.Transport
	bufPool   sync.Pool
}

// StatsCollector collects gateway-level statistics.
type StatsCollector struct {
	mu            sync.RWMutex
	totalReqs     int64
	totalErrors   int64
	cacheHits     int64
	cacheMisses   int64
	rateLimited   int64
	circuitBreaks int64
	retryAttempts int64
	startedAt     time.Time
	routeStats    map[string]*RouteStats
}

// NewStatsCollector creates a new stats collector.
func NewStatsCollector() *StatsCollector {
	return &StatsCollector{
		startedAt:  time.Now(),
		routeStats: make(map[string]*RouteStats),
	}
}

// Snapshot returns the current gateway stats.
func (sc *StatsCollector) Snapshot(rm *RouteManager, hm *HealthMonitor) *GatewayStats {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	stats := &GatewayStats{
		TotalRequests: sc.totalReqs,
		TotalErrors:   sc.totalErrors,
		CacheHits:     sc.cacheHits,
		CacheMisses:   sc.cacheMisses,
		RateLimited:   sc.rateLimited,
		CircuitBreaks: sc.circuitBreaks,
		RetryAttempts: sc.retryAttempts,
		TotalRoutes:   rm.RouteCount(),
		StartedAt:     sc.startedAt,
		Uptime:        int64(time.Since(sc.startedAt).Seconds()),
		RouteStats:    make(map[string]*RouteStats),
	}

	// Copy route stats
	for k, v := range sc.routeStats {
		stats.RouteStats[k] = &RouteStats{
			RouteID:       v.RouteID,
			Path:          v.Path,
			TotalRequests: v.TotalRequests,
			TotalErrors:   v.TotalErrors,
			AvgLatencyMs:  v.AvgLatencyMs,
			CacheHits:     v.CacheHits,
			CacheMisses:   v.CacheMisses,
			RateLimited:   v.RateLimited,
		}
	}

	// Count healthy upstreams
	routes := rm.ListRoutes()
	for _, route := range routes {
		stats.TotalUpstreams += len(route.Targets)

		for _, t := range route.Targets {
			if t.Healthy {
				stats.HealthyUpstreams++
			}
		}
	}

	return stats
}

func (sc *StatsCollector) recordRequest(routeID, path string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.totalReqs++

	if _, ok := sc.routeStats[routeID]; !ok {
		sc.routeStats[routeID] = &RouteStats{
			RouteID: routeID,
			Path:    path,
		}
	}

	sc.routeStats[routeID].TotalRequests++
}

func (sc *StatsCollector) recordError(routeID string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.totalErrors++

	if rs, ok := sc.routeStats[routeID]; ok {
		rs.TotalErrors++
	}
}

func (sc *StatsCollector) recordRateLimited() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.rateLimited++
}

func (sc *StatsCollector) recordCircuitBreak() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.circuitBreaks++
}

// NewProxyEngine creates a new proxy engine.
func NewProxyEngine(
	config Config,
	logger forge.Logger,
	rm *RouteManager,
	hm *HealthMonitor,
	cbm *CircuitBreakerManager,
	rl *RateLimiter,
	stats *StatsCollector,
	hooks *HookEngine,
) *ProxyEngine {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	if config.TLS.InsecureSkipVerify {
		tlsConfig.InsecureSkipVerify = true //nolint:gosec // user-configured
	}

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   config.Timeouts.Connect,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig:       tlsConfig,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   50,
		IdleConnTimeout:       config.Timeouts.Idle,
		ResponseHeaderTimeout: config.Timeouts.Read,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}

	return &ProxyEngine{
		config:    config,
		logger:    logger,
		lb:        NewLoadBalancer(config.LoadBalancing.Strategy),
		cbm:       cbm,
		hm:        hm,
		rm:        rm,
		rl:        rl,
		stats:     stats,
		hooks:     hooks,
		transport: transport,
		bufPool: sync.Pool{
			New: func() any {
				buf := make([]byte, 32*1024)

				return &buf
			},
		},
	}
}

// SetAuth sets the gateway auth handler on the proxy engine.
func (pe *ProxyEngine) SetAuth(auth *GatewayAuth) { pe.auth = auth }

// SetCache sets the response cache on the proxy engine.
func (pe *ProxyEngine) SetCache(cache *ResponseCache) { pe.cache = cache }

// SetTLSManager sets the TLS manager on the proxy engine.
func (pe *ProxyEngine) SetTLSManager(tlsMgr *TLSManager) { pe.tlsMgr = tlsMgr }

// ServeHTTP is the main gateway handler.
func (pe *ProxyEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Rate limiting
	if !pe.rl.Allow(r) {
		pe.stats.recordRateLimited()
		http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)

		return
	}

	// Route matching
	route := pe.rm.MatchRoute(r.URL.Path, r.Method)
	if route == nil {
		http.Error(w, `{"error":"no matching route"}`, http.StatusNotFound)

		return
	}

	// Per-route rate limiting
	if route.RateLimit != nil && !pe.rl.AllowWithConfig(r, route.RateLimit) {
		pe.stats.recordRateLimited()
		http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)

		return
	}

	pe.stats.recordRequest(route.ID, route.Path)

	// Authentication check
	if pe.auth != nil {
		authCtx, authErr := pe.auth.Authenticate(r, route)
		if authErr != nil {
			if ae, ok := authErr.(*AuthError); ok {
				http.Error(w, fmt.Sprintf(`{"error":"%s"}`, ae.Message), ae.Code)
			} else {
				http.Error(w, `{"error":"authentication failed"}`, http.StatusUnauthorized)
			}

			return
		}

		// Forward auth headers to upstream
		if authCtx != nil {
			pe.auth.ForwardAuthHeaders(r, authCtx)
		}
	}

	// Response cache check (before proxying)
	if pe.cache != nil {
		if cached := pe.cache.Get(r, route); cached != nil {
			pe.cache.WriteCachedResponse(w, cached)
			return
		}
	}

	// OnRequest hook
	if pe.hooks != nil {
		if err := pe.hooks.RunOnRequest(r, route); err != nil {
			pe.logger.Warn("on_request hook rejected request",
				forge.F("route_id", route.ID),
				forge.F("error", err),
			)

			http.Error(w, `{"error":"request rejected"}`, http.StatusForbidden)

			return
		}
	}

	// Protocol-specific handling
	switch {
	case isWebSocketUpgrade(r):
		pe.proxyWebSocket(w, r, route)

		return
	case isSSERequest(r):
		pe.proxySSE(w, r, route)

		return
	case isGRPCRequest(r):
		pe.proxyGRPC(w, r, route)

		return
	}

	// Select target via load balancer
	target := pe.selectTarget(r, route)
	if target == nil {
		pe.stats.recordError(route.ID)
		http.Error(w, `{"error":"no healthy upstream"}`, http.StatusServiceUnavailable)

		return
	}

	// Circuit breaker check
	cb := pe.cbm.Get(target.ID)
	if !cb.Allow() {
		pe.stats.recordCircuitBreak()
		http.Error(w, `{"error":"circuit breaker open"}`, http.StatusServiceUnavailable)

		return
	}

	// Build reverse proxy
	targetURL, err := url.Parse(target.URL)
	if err != nil {
		pe.stats.recordError(route.ID)
		http.Error(w, `{"error":"invalid upstream URL"}`, http.StatusBadGateway)

		return
	}

	proxy := &httputil.ReverseProxy{
		Director:       pe.director(r, route, targetURL),
		Transport:      pe.transport,
		ModifyResponse: pe.modifyResponse(route, target, start),
		ErrorHandler:   pe.errorHandler(route, target, cb),
		BufferPool:     &proxyBufferPool{pool: &pe.bufPool},
	}

	target.IncrConns()
	defer target.DecrConns()

	proxy.ServeHTTP(w, r)
}

func (pe *ProxyEngine) director(origReq *http.Request, route *Route, target *url.URL) func(req *http.Request) {
	return func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host

		// Path rewriting
		path := origReq.URL.Path

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

		req.URL.Path = singleJoiningSlash(target.Path, path)
		req.URL.RawQuery = origReq.URL.RawQuery

		// Set host
		req.Host = target.Host

		// Standard proxy headers
		if clientIP, _, err := net.SplitHostPort(origReq.RemoteAddr); err == nil {
			if prior := origReq.Header.Get("X-Forwarded-For"); prior != "" {
				clientIP = prior + ", " + clientIP
			}

			req.Header.Set("X-Forwarded-For", clientIP)
			req.Header.Set("X-Real-IP", clientIP)
		}

		req.Header.Set("X-Forwarded-Host", origReq.Host)
		req.Header.Set("X-Forwarded-Proto", schemeFromRequest(origReq))

		// Apply header policy
		applyHeaderPolicy(req, route.Headers)

		// Apply transform headers
		if route.Transform != nil {
			applyHeaderPolicy(req, route.Transform.RequestHeaders)
		}
	}
}

func (pe *ProxyEngine) modifyResponse(route *Route, target *Target, start time.Time) func(*http.Response) error {
	return func(resp *http.Response) error {
		latency := time.Since(start)
		isError := resp.StatusCode >= 500
		target.RecordRequest(latency, isError)

		if isError {
			pe.stats.recordError(route.ID)
			pe.hm.RecordPassiveFailure(target.ID)
		} else {
			pe.hm.RecordPassiveSuccess(target.ID)
		}

		// Apply response header transforms
		if route.Transform != nil {
			applyResponseHeaderPolicy(resp, route.Transform.ResponseHeaders)
		}

		// Add gateway headers
		resp.Header.Set("X-Gateway-Route", route.ID)

		// Run OnResponse hooks
		if pe.hooks != nil {
			pe.hooks.RunOnResponse(resp, route)
		}

		return nil
	}
}

func (pe *ProxyEngine) errorHandler(route *Route, target *Target, cb *CircuitBreaker) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, _ *http.Request, err error) {
		pe.logger.Warn("upstream error",
			forge.F("route_id", route.ID),
			forge.F("target_url", target.URL),
			forge.F("error", err),
		)

		cb.RecordFailure()
		pe.stats.recordError(route.ID)
		pe.hm.RecordPassiveFailure(target.ID)

		if pe.hooks != nil {
			pe.hooks.RunOnError(err, route, w)
		}

		http.Error(w, `{"error":"upstream error"}`, http.StatusBadGateway)
	}
}

func (pe *ProxyEngine) selectTarget(r *http.Request, route *Route) *Target {
	if len(route.Targets) == 0 {
		return nil
	}

	// Determine consistent hash key
	var key string
	if pe.config.LoadBalancing.Strategy == LBConsistentHash {
		key = pe.config.LoadBalancing.ConsistentKey
		if key != "" {
			key = r.Header.Get(key)
		}

		if key == "" {
			key = r.RemoteAddr
		}
	}

	return pe.lb.Select(route.Targets, key)
}

// proxyWebSocket handles WebSocket upgrade requests.
func (pe *ProxyEngine) proxyWebSocket(w http.ResponseWriter, r *http.Request, route *Route) {
	target := pe.selectTarget(r, route)
	if target == nil {
		http.Error(w, `{"error":"no healthy upstream"}`, http.StatusServiceUnavailable)

		return
	}

	ProxyWebSocket(w, r, route, target, pe.config.WebSocket, pe.logger)
}

// proxyGRPC handles gRPC requests.
func (pe *ProxyEngine) proxyGRPC(w http.ResponseWriter, r *http.Request, route *Route) {
	target := pe.selectTarget(r, route)
	if target == nil {
		writeGRPCError(w, http.StatusServiceUnavailable, "no healthy upstream")

		return
	}

	ProxyGRPC(w, r, route, target, pe.config, pe.logger)
}

// proxySSE handles SSE requests.
func (pe *ProxyEngine) proxySSE(w http.ResponseWriter, r *http.Request, route *Route) {
	target := pe.selectTarget(r, route)
	if target == nil {
		http.Error(w, `{"error":"no healthy upstream"}`, http.StatusServiceUnavailable)

		return
	}

	ProxySSE(w, r, route, target, pe.config, pe.logger)
}

// Helper functions

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func isSSERequest(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

func isGRPCRequest(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc")
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")

	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}

	return a + b
}

func schemeFromRequest(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}

	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}

	return "http"
}

func applyHeaderPolicy(req *http.Request, policy HeaderPolicy) {
	for k, v := range policy.Add {
		req.Header.Add(k, v)
	}

	for k, v := range policy.Set {
		req.Header.Set(k, v)
	}

	for _, k := range policy.Remove {
		req.Header.Del(k)
	}
}

func applyResponseHeaderPolicy(resp *http.Response, policy HeaderPolicy) {
	for k, v := range policy.Add {
		resp.Header.Add(k, v)
	}

	for k, v := range policy.Set {
		resp.Header.Set(k, v)
	}

	for _, k := range policy.Remove {
		resp.Header.Del(k)
	}
}

// proxyBufferPool wraps sync.Pool for httputil.BufferPool interface.
type proxyBufferPool struct {
	pool *sync.Pool
}

func (p *proxyBufferPool) Get() []byte {
	buf := p.pool.Get().(*[]byte)

	return *buf
}

func (p *proxyBufferPool) Put(buf []byte) {
	p.pool.Put(&buf)
}

// copyBody copies body without loading it all into memory.
func copyBody(_ context.Context, dst io.Writer, src io.Reader) error {
	_, err := io.Copy(dst, src)

	return err
}
