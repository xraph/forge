package dashboard

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forgeui/bridge"

	"github.com/xraph/forge/extensions/dashboard/collector"
)

// DashboardBridge wraps a forgeui bridge.Bridge to provide Go↔JS communication
// for the dashboard. Bridge functions can be called from the browser via Alpine.js
// magic helpers ($go, $goBatch, $goStream) or the ForgeBridge JS client.
type DashboardBridge struct {
	bridge    *bridge.Bridge
	collector *collector.DataCollector
	history   *collector.DataHistory
}

// NewDashboardBridge creates a new bridge with built-in dashboard functions registered.
func NewDashboardBridge(c *collector.DataCollector, h *collector.DataHistory) *DashboardBridge {
	b := bridge.New(
		bridge.WithTimeout(15*time.Second),
		bridge.WithCSRF(false), // CSRF handled at forge router level
		bridge.WithMaxBatchSize(10),
	)

	db := &DashboardBridge{
		bridge:    b,
		collector: c,
		history:   h,
	}

	db.registerBuiltinFunctions()

	return db
}

// NewDashboardBridgeWithBridge wraps an existing forgeui bridge instance with dashboard functions.
// Use this when the bridge is created by forgeui.App (via forgeui.WithBridge()).
func NewDashboardBridgeWithBridge(b *bridge.Bridge, c *collector.DataCollector, h *collector.DataHistory) *DashboardBridge {
	db := &DashboardBridge{
		bridge:    b,
		collector: c,
		history:   h,
	}

	db.registerBuiltinFunctions()

	return db
}

// Bridge returns the underlying forgeui bridge instance.
func (db *DashboardBridge) Bridge() *bridge.Bridge {
	return db.bridge
}

// Register registers a custom bridge function.
// Extensions can use this to expose Go functions callable from the dashboard UI.
func (db *DashboardBridge) Register(name string, handler any, opts ...bridge.FunctionOption) error {
	return db.bridge.Register(name, handler, opts...)
}

// ---- Built-in bridge functions ----

// registerBuiltinFunctions registers the core dashboard bridge functions.
func (db *DashboardBridge) registerBuiltinFunctions() {
	// Overview data
	_ = db.bridge.Register("dashboard.getOverview", db.getOverview,
		bridge.WithDescription("Get dashboard overview data"),
		bridge.WithFunctionCache(10*time.Second),
	)

	// Health data
	_ = db.bridge.Register("dashboard.getHealth", db.getHealth,
		bridge.WithDescription("Get health check data"),
		bridge.WithFunctionCache(5*time.Second),
	)

	// Metrics data
	_ = db.bridge.Register("dashboard.getMetrics", db.getMetrics,
		bridge.WithDescription("Get metrics data"),
		bridge.WithFunctionCache(10*time.Second),
	)

	// Services list
	_ = db.bridge.Register("dashboard.getServices", db.getServices,
		bridge.WithDescription("Get registered services list"),
		bridge.WithFunctionCache(10*time.Second),
	)

	// Service detail
	_ = db.bridge.Register("dashboard.getServiceDetail", db.getServiceDetail,
		bridge.WithDescription("Get detailed info for a specific service"),
	)

	// History data
	_ = db.bridge.Register("dashboard.getHistory", db.getHistory,
		bridge.WithDescription("Get historical dashboard data"),
		bridge.WithFunctionCache(5*time.Second),
	)

	// Metrics report
	_ = db.bridge.Register("dashboard.getMetricsReport", db.getMetricsReport,
		bridge.WithDescription("Get comprehensive metrics report"),
		bridge.WithFunctionCache(10*time.Second),
	)

	// Refresh trigger (no cache)
	_ = db.bridge.Register("dashboard.refresh", db.refresh,
		bridge.WithDescription("Force a data refresh and return overview"),
	)
}

// ---- Bridge function handlers ----
// Each function follows the signature: func(ctx bridge.Context, params T) (R, error)

// EmptyParams is used for functions that take no input.
type EmptyParams struct{}

// ServiceNameParams is used for functions that take a service name.
type ServiceNameParams struct {
	Name string `json:"name"`
}

func (db *DashboardBridge) getOverview(ctx bridge.Context, params EmptyParams) (*collector.OverviewData, error) {
	return db.collector.CollectOverview(context.Background()), nil
}

func (db *DashboardBridge) getHealth(ctx bridge.Context, params EmptyParams) (*collector.HealthData, error) {
	return db.collector.CollectHealth(context.Background()), nil
}

func (db *DashboardBridge) getMetrics(ctx bridge.Context, params EmptyParams) (*collector.MetricsData, error) {
	return db.collector.CollectMetrics(context.Background()), nil
}

func (db *DashboardBridge) getServices(ctx bridge.Context, params EmptyParams) ([]collector.ServiceInfo, error) {
	return db.collector.CollectServices(context.Background()), nil
}

func (db *DashboardBridge) getServiceDetail(ctx bridge.Context, params ServiceNameParams) (*collector.ServiceDetail, error) {
	if params.Name == "" {
		return nil, fmt.Errorf("service name is required")
	}

	return db.collector.CollectServiceDetail(context.Background(), params.Name), nil
}

func (db *DashboardBridge) getHistory(ctx bridge.Context, params EmptyParams) (collector.HistoryData, error) {
	return db.history.GetAll(), nil
}

func (db *DashboardBridge) getMetricsReport(ctx bridge.Context, params EmptyParams) (*collector.MetricsReport, error) {
	return db.collector.CollectMetricsReport(context.Background()), nil
}

func (db *DashboardBridge) refresh(ctx bridge.Context, params EmptyParams) (*collector.OverviewData, error) {
	return db.collector.CollectOverview(context.Background()), nil
}
