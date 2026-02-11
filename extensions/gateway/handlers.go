package gateway

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/xraph/forge"
)

// AdminHandlers provides REST API handlers for the gateway admin.
type AdminHandlers struct {
	ext *Extension
}

// NewAdminHandlers creates new admin handlers.
func NewAdminHandlers(ext *Extension) *AdminHandlers {
	return &AdminHandlers{ext: ext}
}

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// handleListRoutes returns all routes.
func (h *AdminHandlers) handleListRoutes(ctx forge.Context) error {
	routes := h.ext.routeManager.ListRoutes()

	// Filter by query params
	source := ctx.Query("source")
	protocol := ctx.Query("protocol")

	if source != "" || protocol != "" {
		filtered := make([]*Route, 0)

		for _, r := range routes {
			if source != "" && string(r.Source) != source {
				continue
			}

			if protocol != "" && string(r.Protocol) != protocol {
				continue
			}

			filtered = append(filtered, r)
		}

		routes = filtered
	}

	return ctx.JSON(http.StatusOK, routes)
}

// handleGetRoute returns a single route by ID.
func (h *AdminHandlers) handleGetRoute(ctx forge.Context) error {
	id := ctx.Param("id")

	route, ok := h.ext.routeManager.GetRoute(id)
	if !ok {
		return ctx.JSON(http.StatusNotFound, map[string]string{"error": "route not found"})
	}

	// Snapshot target stats
	for _, t := range route.Targets {
		t.Snapshot()
	}

	return ctx.JSON(http.StatusOK, route)
}

// handleCreateRoute creates a new manual route.
func (h *AdminHandlers) handleCreateRoute(ctx forge.Context) error {
	var dto RouteDTO
	if err := json.NewDecoder(ctx.Request().Body).Decode(&dto); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	route := dtoToRoute(dto, h.ext.config.BasePath)

	if err := h.ext.routeManager.AddRoute(route); err != nil {
		return ctx.JSON(http.StatusConflict, map[string]string{"error": err.Error()})
	}

	// Register targets with health monitor
	for _, target := range route.Targets {
		h.ext.healthMon.Register(route.ID, target)
	}

	h.ext.accessLog.LogAdminAction("create_route", route.ID, "success", ctx.Request())

	return ctx.JSON(http.StatusCreated, route)
}

// handleUpdateRoute updates an existing route.
func (h *AdminHandlers) handleUpdateRoute(ctx forge.Context) error {
	id := ctx.Param("id")

	existing, ok := h.ext.routeManager.GetRoute(id)
	if !ok {
		return ctx.JSON(http.StatusNotFound, map[string]string{"error": "route not found"})
	}

	if existing.Source != SourceManual {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "cannot update auto-discovered routes"})
	}

	var dto RouteDTO
	if err := json.NewDecoder(ctx.Request().Body).Decode(&dto); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	updated := dtoToRoute(dto, h.ext.config.BasePath)
	updated.ID = id
	updated.Source = SourceManual

	if err := h.ext.routeManager.UpdateRoute(updated); err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	h.ext.accessLog.LogAdminAction("update_route", id, "success", ctx.Request())

	return ctx.JSON(http.StatusOK, updated)
}

// handleDeleteRoute deletes a manual route.
func (h *AdminHandlers) handleDeleteRoute(ctx forge.Context) error {
	id := ctx.Param("id")

	route, ok := h.ext.routeManager.GetRoute(id)
	if !ok {
		return ctx.JSON(http.StatusNotFound, map[string]string{"error": "route not found"})
	}

	if route.Source != SourceManual {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "cannot delete auto-discovered routes"})
	}

	// Deregister targets from health monitor
	for _, target := range route.Targets {
		h.ext.healthMon.Deregister(target.ID)
	}

	if err := h.ext.routeManager.RemoveRoute(id); err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	h.ext.accessLog.LogAdminAction("delete_route", id, "success", ctx.Request())

	return ctx.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

// handleEnableRoute enables a route.
func (h *AdminHandlers) handleEnableRoute(ctx forge.Context) error {
	id := ctx.Param("id")

	route, ok := h.ext.routeManager.GetRoute(id)
	if !ok {
		return ctx.JSON(http.StatusNotFound, map[string]string{"error": "route not found"})
	}

	route.Enabled = true

	if err := h.ext.routeManager.UpdateRoute(route); err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return ctx.JSON(http.StatusOK, map[string]string{"status": "enabled"})
}

// handleDisableRoute disables a route.
func (h *AdminHandlers) handleDisableRoute(ctx forge.Context) error {
	id := ctx.Param("id")

	route, ok := h.ext.routeManager.GetRoute(id)
	if !ok {
		return ctx.JSON(http.StatusNotFound, map[string]string{"error": "route not found"})
	}

	route.Enabled = false

	if err := h.ext.routeManager.UpdateRoute(route); err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return ctx.JSON(http.StatusOK, map[string]string{"status": "disabled"})
}

// handleListUpstreams returns all targets with health status.
func (h *AdminHandlers) handleListUpstreams(ctx forge.Context) error {
	routes := h.ext.routeManager.ListRoutes()

	type UpstreamInfo struct {
		*Target
		RouteID   string `json:"routeId"`
		RoutePath string `json:"routePath"`
	}

	upstreams := make([]UpstreamInfo, 0)

	for _, r := range routes {
		for _, t := range r.Targets {
			t.Snapshot()
			upstreams = append(upstreams, UpstreamInfo{
				Target:    t,
				RouteID:   r.ID,
				RoutePath: r.Path,
			})
		}
	}

	return ctx.JSON(http.StatusOK, upstreams)
}

// handleGetStats returns aggregated gateway statistics.
func (h *AdminHandlers) handleGetStats(ctx forge.Context) error {
	stats := h.ext.stats.Snapshot(h.ext.routeManager, h.ext.healthMon)

	return ctx.JSON(http.StatusOK, stats)
}

// handleGetRouteStats returns per-route statistics.
func (h *AdminHandlers) handleGetRouteStats(ctx forge.Context) error {
	stats := h.ext.stats.Snapshot(h.ext.routeManager, h.ext.healthMon)

	return ctx.JSON(http.StatusOK, stats.RouteStats)
}

// handleGetConfig returns the current gateway configuration.
func (h *AdminHandlers) handleGetConfig(ctx forge.Context) error {
	// Return a sanitized config (no secrets)
	config := h.ext.config

	// Redact TLS keys
	if config.TLS.ClientKeyFile != "" {
		config.TLS.ClientKeyFile = "[REDACTED]"
	}

	return ctx.JSON(http.StatusOK, config)
}

// handleListDiscoveredServices returns all discovered services.
func (h *AdminHandlers) handleListDiscoveredServices(ctx forge.Context) error {
	if h.ext.disc == nil {
		return ctx.JSON(http.StatusOK, []DiscoveredService{})
	}

	services := h.ext.disc.DiscoveredServices()

	return ctx.JSON(http.StatusOK, services)
}

// handleRefreshDiscovery forces a FARP re-scan.
func (h *AdminHandlers) handleRefreshDiscovery(ctx forge.Context) error {
	if h.ext.disc == nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "discovery not enabled"})
	}

	if err := h.ext.disc.Refresh(ctx.Context()); err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	h.ext.accessLog.LogAdminAction("refresh_discovery", "", "success", ctx.Request())

	return ctx.JSON(http.StatusOK, map[string]string{"status": "refreshed"})
}

// handleWebSocket handles the dashboard WebSocket connection.
func (h *AdminHandlers) handleWebSocket(ctx forge.Context) error {
	if h.ext.hub == nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": "realtime not enabled"})
	}

	conn, err := wsUpgrader.Upgrade(ctx.Response(), ctx.Request(), nil)
	if err != nil {
		h.ext.Logger().Error("websocket upgrade failed", forge.F("error", err))

		return nil //nolint:nilerr // Upgrade already wrote response
	}

	client := NewClient(h.ext.hub, conn)
	h.ext.hub.register <- client
	client.Start()

	return nil
}

// handleDashboardPage serves the gateway dashboard HTML page.
func (h *AdminHandlers) handleDashboardPage(ctx forge.Context) error {
	// Check if ForgeUI is available
	if h.ext.forgeUI != nil {
		// ForgeUI handles it
		return nil
	}

	// Serve basic dashboard HTML
	html := generateDashboardHTML(h.ext.config.Dashboard)
	ctx.SetHeader("Content-Type", "text/html; charset=utf-8")
	ctx.SetHeader("Cache-Control", "no-cache, no-store, must-revalidate")

	return ctx.String(http.StatusOK, html)
}

// dtoToRoute converts a RouteDTO to a Route.
func dtoToRoute(dto RouteDTO, basePath string) *Route {
	path := basePath + dto.Path

	targets := make([]*Target, 0, len(dto.Targets))
	for _, td := range dto.Targets {
		t := &Target{
			ID:       "target-" + strings.ReplaceAll(td.URL, "://", "-"),
			URL:      td.URL,
			Weight:   td.Weight,
			Healthy:  true,
			Tags:     td.Tags,
			Metadata: td.Metadata,
			TLS:      td.TLS,
		}

		if t.Weight <= 0 {
			t.Weight = 1
		}

		targets = append(targets, t)
	}

	protocol := dto.Protocol
	if protocol == "" {
		protocol = ProtocolHTTP
	}

	return &Route{
		Path:           path,
		Methods:        dto.Methods,
		Targets:        targets,
		StripPrefix:    dto.StripPrefix,
		AddPrefix:      dto.AddPrefix,
		RewritePath:    dto.RewritePath,
		Headers:        dto.Headers,
		Protocol:       protocol,
		Source:         SourceManual,
		Priority:       dto.Priority + 100,
		Enabled:        dto.Enabled,
		Retry:          dto.Retry,
		Timeout:        dto.Timeout,
		RateLimit:      dto.RateLimit,
		Auth:           dto.Auth,
		CircuitBreaker: dto.CircuitBreaker,
		Cache:          dto.Cache,
		TrafficPolicy:  dto.TrafficPolicy,
		Transform:      dto.Transform,
		Metadata:       dto.Metadata,
	}
}

// generateDashboardHTML generates a basic dashboard page.
func generateDashboardHTML(config DashboardConfig) string {
	title := config.Title
	if title == "" {
		title = "Forge Gateway"
	}

	basePath := config.BasePath

	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>` + title + `</title>
<script src="https://cdn.tailwindcss.com"></script>
<script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.x.x/dist/cdn.min.js"></script>
<script src="https://cdn.jsdelivr.net/npm/apexcharts"></script>
<style>
[x-cloak] { display: none !important; }
</style>
</head>
<body class="min-h-screen bg-gray-50 dark:bg-gray-900 text-gray-900 dark:text-gray-100" x-data="gatewayDashboard()">
<script>
function gatewayDashboard() {
    return {
        tab: 'overview',
        stats: null,
        routes: [],
        upstreams: [],
        services: [],
        config: null,
        ws: null,
        init() {
            this.fetchAll();
            this.connectWS();
            setInterval(() => this.fetchAll(), 5000);
        },
        async fetchAll() {
            try {
                const [statsRes, routesRes, upstreamsRes, servicesRes] = await Promise.all([
                    fetch('` + basePath + `/api/stats'),
                    fetch('` + basePath + `/api/routes'),
                    fetch('` + basePath + `/api/upstreams'),
                    fetch('` + basePath + `/api/discovery/services'),
                ]);
                this.stats = await statsRes.json();
                this.routes = await routesRes.json();
                this.upstreams = await upstreamsRes.json();
                this.services = await servicesRes.json();
            } catch (e) { console.error('fetch error', e); }
        },
        connectWS() {
            const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
            this.ws = new WebSocket(proto + '//' + location.host + '` + basePath + `/ws');
            this.ws.onmessage = (e) => {
                try {
                    const msg = JSON.parse(e.data);
                    if (msg.type === 'stats') this.stats = msg.data;
                    if (msg.type === 'routes') this.routes = msg.data;
                } catch (err) {}
            };
            this.ws.onclose = () => setTimeout(() => this.connectWS(), 3000);
        },
        formatLatency(ms) {
            if (!ms) return '0ms';
            return ms < 1000 ? ms.toFixed(1) + 'ms' : (ms / 1000).toFixed(2) + 's';
        },
        statusColor(healthy) {
            return healthy ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200' : 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200';
        }
    }
}
</script>

<!-- Header -->
<nav class="bg-white dark:bg-gray-800 shadow">
    <div class="max-w-7xl mx-auto px-4 py-3 flex items-center justify-between">
        <div class="flex items-center space-x-3">
            <svg class="w-8 h-8 text-blue-600" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z"/></svg>
            <h1 class="text-xl font-bold">` + title + `</h1>
        </div>
        <div class="flex items-center space-x-4 text-sm text-gray-500">
            <template x-if="stats">
                <span x-text="stats.totalRoutes + ' routes | ' + stats.healthyUpstreams + '/' + stats.totalUpstreams + ' healthy'"></span>
            </template>
        </div>
    </div>
</nav>

<!-- Tabs -->
<div class="max-w-7xl mx-auto px-4 mt-4">
    <div class="border-b border-gray-200 dark:border-gray-700">
        <nav class="flex space-x-6 -mb-px">
            <button @click="tab='overview'" :class="tab==='overview' ? 'border-blue-500 text-blue-600' : 'border-transparent text-gray-500 hover:text-gray-700'" class="px-1 py-3 border-b-2 font-medium text-sm">Overview</button>
            <button @click="tab='routes'" :class="tab==='routes' ? 'border-blue-500 text-blue-600' : 'border-transparent text-gray-500 hover:text-gray-700'" class="px-1 py-3 border-b-2 font-medium text-sm">Routes</button>
            <button @click="tab='upstreams'" :class="tab==='upstreams' ? 'border-blue-500 text-blue-600' : 'border-transparent text-gray-500 hover:text-gray-700'" class="px-1 py-3 border-b-2 font-medium text-sm">Upstreams</button>
            <button @click="tab='services'" :class="tab==='services' ? 'border-blue-500 text-blue-600' : 'border-transparent text-gray-500 hover:text-gray-700'" class="px-1 py-3 border-b-2 font-medium text-sm">Services</button>
        </nav>
    </div>
</div>

<!-- Content -->
<main class="max-w-7xl mx-auto px-4 py-6">

<!-- Overview Tab -->
<div x-show="tab==='overview'" x-cloak>
    <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 mb-6">
        <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
            <div class="text-sm text-gray-500">Total Requests</div>
            <div class="text-2xl font-bold" x-text="stats ? stats.totalRequests.toLocaleString() : '—'"></div>
        </div>
        <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
            <div class="text-sm text-gray-500">Error Rate</div>
            <div class="text-2xl font-bold" x-text="stats && stats.totalRequests ? ((stats.totalErrors / stats.totalRequests) * 100).toFixed(2) + '%' : '0%'"></div>
        </div>
        <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
            <div class="text-sm text-gray-500">Avg Latency</div>
            <div class="text-2xl font-bold" x-text="stats ? formatLatency(stats.avgLatencyMs) : '—'"></div>
        </div>
        <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
            <div class="text-sm text-gray-500">Healthy Upstreams</div>
            <div class="text-2xl font-bold" x-text="stats ? stats.healthyUpstreams + '/' + stats.totalUpstreams : '—'"></div>
        </div>
    </div>
    <div class="grid grid-cols-1 md:grid-cols-3 gap-4">
        <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4"><div class="text-sm text-gray-500">Cache Hits</div><div class="text-xl font-bold" x-text="stats ? stats.cacheHits.toLocaleString() : '0'"></div></div>
        <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4"><div class="text-sm text-gray-500">Rate Limited</div><div class="text-xl font-bold" x-text="stats ? stats.rateLimited.toLocaleString() : '0'"></div></div>
        <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4"><div class="text-sm text-gray-500">Circuit Breaks</div><div class="text-xl font-bold" x-text="stats ? stats.circuitBreaks.toLocaleString() : '0'"></div></div>
    </div>
</div>

<!-- Routes Tab -->
<div x-show="tab==='routes'" x-cloak>
    <div class="bg-white dark:bg-gray-800 rounded-lg shadow overflow-hidden">
        <table class="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
            <thead class="bg-gray-50 dark:bg-gray-700"><tr>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Path</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Methods</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Protocol</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Source</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Targets</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
            </tr></thead>
            <tbody class="divide-y divide-gray-200 dark:divide-gray-700">
                <template x-for="route in routes" :key="route.id">
                    <tr class="hover:bg-gray-50 dark:hover:bg-gray-700">
                        <td class="px-4 py-3 text-sm font-mono" x-text="route.path"></td>
                        <td class="px-4 py-3 text-sm" x-text="route.methods ? route.methods.join(', ') : 'ALL'"></td>
                        <td class="px-4 py-3 text-sm"><span class="px-2 py-1 rounded text-xs font-medium bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200" x-text="route.protocol"></span></td>
                        <td class="px-4 py-3 text-sm" x-text="route.source"></td>
                        <td class="px-4 py-3 text-sm" x-text="route.targets ? route.targets.length : 0"></td>
                        <td class="px-4 py-3 text-sm"><span :class="route.enabled ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200' : 'bg-gray-100 text-gray-800'" class="px-2 py-1 rounded text-xs font-medium" x-text="route.enabled ? 'Active' : 'Disabled'"></span></td>
                    </tr>
                </template>
            </tbody>
        </table>
    </div>
</div>

<!-- Upstreams Tab -->
<div x-show="tab==='upstreams'" x-cloak>
    <div class="bg-white dark:bg-gray-800 rounded-lg shadow overflow-hidden">
        <table class="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
            <thead class="bg-gray-50 dark:bg-gray-700"><tr>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">URL</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Route</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Health</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Circuit</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Active Conns</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Requests</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Errors</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Avg Latency</th>
            </tr></thead>
            <tbody class="divide-y divide-gray-200 dark:divide-gray-700">
                <template x-for="u in upstreams" :key="u.id">
                    <tr class="hover:bg-gray-50 dark:hover:bg-gray-700">
                        <td class="px-4 py-3 text-sm font-mono" x-text="u.url"></td>
                        <td class="px-4 py-3 text-sm" x-text="u.routePath"></td>
                        <td class="px-4 py-3 text-sm"><span :class="statusColor(u.healthy)" class="px-2 py-1 rounded text-xs font-medium" x-text="u.healthy ? 'Healthy' : 'Unhealthy'"></span></td>
                        <td class="px-4 py-3 text-sm" x-text="u.circuitState"></td>
                        <td class="px-4 py-3 text-sm" x-text="u.activeConns"></td>
                        <td class="px-4 py-3 text-sm" x-text="u.totalRequests.toLocaleString()"></td>
                        <td class="px-4 py-3 text-sm" x-text="u.totalErrors.toLocaleString()"></td>
                        <td class="px-4 py-3 text-sm" x-text="formatLatency(u.avgLatencyMs)"></td>
                    </tr>
                </template>
            </tbody>
        </table>
    </div>
</div>

<!-- Services Tab -->
<div x-show="tab==='services'" x-cloak>
    <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        <template x-for="svc in services" :key="svc.name">
            <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
                <div class="flex items-center justify-between mb-2">
                    <h3 class="text-lg font-semibold" x-text="svc.name"></h3>
                    <span :class="statusColor(svc.healthy)" class="px-2 py-1 rounded text-xs font-medium" x-text="svc.healthy ? 'Healthy' : 'Unhealthy'"></span>
                </div>
                <div class="text-sm text-gray-500 space-y-1">
                    <div>Version: <span class="text-gray-900 dark:text-gray-100" x-text="svc.version || '—'"></span></div>
                    <div>Address: <span class="font-mono text-gray-900 dark:text-gray-100" x-text="svc.address + ':' + svc.port"></span></div>
                    <div>Protocols: <span class="text-gray-900 dark:text-gray-100" x-text="svc.protocols ? svc.protocols.join(', ') : '—'"></span></div>
                    <div>Routes: <span class="text-gray-900 dark:text-gray-100" x-text="svc.routeCount"></span></div>
                </div>
            </div>
        </template>
    </div>
    <div x-show="services.length === 0" class="text-center py-12 text-gray-500">
        <p>No discovered services</p>
        <p class="text-sm">Enable FARP discovery to auto-detect services</p>
    </div>
</div>

</main>
</body>
</html>`
}
