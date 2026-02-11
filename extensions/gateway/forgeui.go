package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	g "maragu.dev/gomponents"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/gateway/ui"
	"github.com/xraph/forgeui/router"
)

// ForgeUIIntegration provides ForgeUI-specific functionality for the gateway dashboard.
type ForgeUIIntegration struct {
	config       DashboardConfig
	gwConfig     Config
	routeManager *RouteManager
	healthMon    *HealthMonitor
	stats        *StatsCollector
	disc         *ServiceDiscovery
	hub          *Hub
	logger       forge.Logger
}

// NewForgeUIIntegration creates a new ForgeUI integration for the gateway.
func NewForgeUIIntegration(
	config DashboardConfig,
	gwConfig Config,
	routeManager *RouteManager,
	healthMon *HealthMonitor,
	stats *StatsCollector,
	disc *ServiceDiscovery,
	hub *Hub,
	logger forge.Logger,
) *ForgeUIIntegration {
	return &ForgeUIIntegration{
		config:       config,
		gwConfig:     gwConfig,
		routeManager: routeManager,
		healthMon:    healthMon,
		stats:        stats,
		disc:         disc,
		hub:          hub,
		logger:       logger,
	}
}

// RegisterRoutes registers gateway dashboard routes with a ForgeUI router.
func (fi *ForgeUIIntegration) RegisterRoutes(r *router.Router) {
	basePath := fi.config.BasePath

	// Main dashboard page
	r.Get(basePath, ui.GatewayDashboardPageHandler(
		fi.config.Title,
		fi.config.BasePath,
		fi.config.Realtime,
	))

	// API endpoints
	r.Get(basePath+"/api/routes", fi.handleAPIRoutes)
	r.Get(basePath+"/api/upstreams", fi.handleAPIUpstreams)
	r.Get(basePath+"/api/stats", fi.handleAPIStats)
	r.Get(basePath+"/api/discovery/services", fi.handleAPIDiscoveryServices)
	r.Get(basePath+"/api/config", fi.handleAPIConfig)

	// WebSocket endpoint
	if fi.config.Realtime && fi.hub != nil {
		r.Get(basePath+"/ws", fi.handleWebSocket)
	}
}

// Start starts background services.
func (fi *ForgeUIIntegration) Start(ctx context.Context) error {
	if fi.hub != nil {
		go fi.hub.Run()
		go fi.broadcastUpdates(ctx)
	}

	fi.logger.Info("gateway ForgeUI integration started",
		forge.F("base_path", fi.config.BasePath),
		forge.F("realtime", fi.config.Realtime),
	)

	return nil
}

// Stop stops background services.
func (fi *ForgeUIIntegration) Stop(_ context.Context) error {
	fi.logger.Info("gateway ForgeUI integration stopped")

	return nil
}

// API handlers

func (fi *ForgeUIIntegration) handleAPIRoutes(ctx *router.PageContext) (g.Node, error) {
	routes := fi.routeManager.ListRoutes()

	return respondJSON(ctx.ResponseWriter, routes), nil
}

func (fi *ForgeUIIntegration) handleAPIUpstreams(ctx *router.PageContext) (g.Node, error) {
	routes := fi.routeManager.ListRoutes()

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

	return respondJSON(ctx.ResponseWriter, upstreams), nil
}

func (fi *ForgeUIIntegration) handleAPIStats(ctx *router.PageContext) (g.Node, error) {
	stats := fi.stats.Snapshot(fi.routeManager, fi.healthMon)

	return respondJSON(ctx.ResponseWriter, stats), nil
}

func (fi *ForgeUIIntegration) handleAPIDiscoveryServices(ctx *router.PageContext) (g.Node, error) {
	if fi.disc == nil {
		return respondJSON(ctx.ResponseWriter, []DiscoveredService{}), nil
	}

	services := fi.disc.DiscoveredServices()

	return respondJSON(ctx.ResponseWriter, services), nil
}

func (fi *ForgeUIIntegration) handleAPIConfig(ctx *router.PageContext) (g.Node, error) {
	config := fi.gwConfig
	if config.TLS.ClientKeyFile != "" {
		config.TLS.ClientKeyFile = "[REDACTED]"
	}

	return respondJSON(ctx.ResponseWriter, config), nil
}

func (fi *ForgeUIIntegration) handleWebSocket(ctx *router.PageContext) (g.Node, error) {
	if fi.hub == nil {
		ctx.ResponseWriter.WriteHeader(http.StatusServiceUnavailable)

		return nil, nil //nolint:nilnil // No HTML node returned for WebSocket
	}

	conn, err := wsUpgrader.Upgrade(ctx.ResponseWriter, ctx.Request, nil)
	if err != nil {
		fi.logger.Error("websocket upgrade failed", forge.F("error", err))

		return nil, err
	}

	client := NewClient(fi.hub, conn)
	fi.hub.register <- client
	client.Start()

	return nil, nil //nolint:nilnil // No HTML node returned after WebSocket upgrade
}

func (fi *ForgeUIIntegration) broadcastUpdates(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if fi.hub == nil || fi.hub.ClientCount() == 0 {
				continue
			}

			stats := fi.stats.Snapshot(fi.routeManager, fi.healthMon)
			msg := NewWSMessage("stats", stats)

			if err := fi.hub.Broadcast(msg); err != nil {
				fi.logger.Error("failed to broadcast stats", forge.F("error", err))
			}
		}
	}
}

// respondJSON writes JSON to a response writer and returns nil Node.
func respondJSON(w http.ResponseWriter, data any) g.Node {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	return nil
}
