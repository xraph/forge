package admin

import (
	"context"
	"encoding/json"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
	"github.com/xraph/forge/extensions/consensus/observability"
)

// API provides admin endpoints for consensus management.
type API struct {
	service          internal.ConsensusService
	healthChecker    *observability.HealthChecker
	metricsCollector *observability.MetricsCollector
	logger           forge.Logger
}

// NewAPI creates a new admin API.
func NewAPI(
	service internal.ConsensusService,
	healthChecker *observability.HealthChecker,
	metricsCollector *observability.MetricsCollector,
	logger forge.Logger,
) *API {
	return &API{
		service:          service,
		healthChecker:    healthChecker,
		metricsCollector: metricsCollector,
		logger:           logger,
	}
}

// HandleHealth handles health check requests.
func (a *API) HandleHealth(ctx forge.Context) error {
	checkCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.service.HealthCheck(checkCtx); err != nil {
		return ctx.JSON(503, map[string]any{
			"healthy": false,
			"error":   err.Error(),
		})
	}

	return ctx.JSON(200, map[string]any{
		"healthy": true,
		"status":  "ok",
	})
}

// HandleDetailedHealth handles detailed health check requests.
func (a *API) HandleDetailedHealth(ctx forge.Context) error {
	checkCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var status internal.HealthStatus
	if a.healthChecker != nil {
		status = a.healthChecker.GetHealthStatus(checkCtx)
	} else {
		// Fallback to simple health check
		err := a.service.HealthCheck(checkCtx)
		status = internal.HealthStatus{
			Healthy:   err == nil,
			Status:    "ok",
			LastCheck: time.Now(),
		}
	}

	statusCode := 200
	if !status.Healthy {
		statusCode = 503
	}

	return ctx.JSON(statusCode, status)
}

// HandleStatus handles status requests.
func (a *API) HandleStatus(ctx forge.Context) error {
	stats := a.service.GetStats()
	clusterInfo := a.service.GetClusterInfo()

	return ctx.JSON(200, map[string]any{
		"stats":   stats,
		"cluster": clusterInfo,
	})
}

// HandleMetrics handles metrics requests.
func (a *API) HandleMetrics(ctx forge.Context) error {
	if a.metricsCollector == nil {
		return ctx.JSON(404, map[string]any{
			"error": "metrics collector not configured",
		})
	}

	metrics := a.metricsCollector.GetMetrics()

	return ctx.JSON(200, metrics)
}

// HandleGetLeader handles get leader requests.
func (a *API) HandleGetLeader(ctx forge.Context) error {
	leaderID := a.service.GetLeader()
	role := a.service.GetRole()

	return ctx.JSON(200, map[string]any{
		"leader_id":    leaderID,
		"is_leader":    a.service.IsLeader(),
		"current_role": role,
		"term":         a.service.GetTerm(),
	})
}

// HandleListNodes handles list nodes requests.
func (a *API) HandleListNodes(ctx forge.Context) error {
	clusterInfo := a.service.GetClusterInfo()

	return ctx.JSON(200, map[string]any{
		"nodes":      clusterInfo.Nodes,
		"total":      clusterInfo.TotalNodes,
		"active":     clusterInfo.ActiveNodes,
		"has_quorum": clusterInfo.HasQuorum,
	})
}

// HandleTransferLeadership handles leadership transfer requests.
func (a *API) HandleTransferLeadership(ctx forge.Context) error {
	var req struct {
		TargetNodeID string `json:"target_node_id"`
	}

	if err := ctx.BindJSON(&req); err != nil {
		return ctx.JSON(400, map[string]any{
			"error": "invalid request body",
		})
	}

	if req.TargetNodeID == "" {
		return ctx.JSON(400, map[string]any{
			"error": "target_node_id is required",
		})
	}

	transferCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := a.service.TransferLeadership(transferCtx, req.TargetNodeID); err != nil {
		return ctx.JSON(500, map[string]any{
			"error": err.Error(),
		})
	}

	return ctx.JSON(200, map[string]any{
		"message": "leadership transfer initiated",
		"target":  req.TargetNodeID,
	})
}

// HandleStepDown handles step down requests.
func (a *API) HandleStepDown(ctx forge.Context) error {
	stepDownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := a.service.StepDown(stepDownCtx); err != nil {
		return ctx.JSON(500, map[string]any{
			"error": err.Error(),
		})
	}

	return ctx.JSON(200, map[string]any{
		"message": "stepped down as leader",
	})
}

// HandleSnapshot handles snapshot requests.
func (a *API) HandleSnapshot(ctx forge.Context) error {
	snapshotCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := a.service.Snapshot(snapshotCtx); err != nil {
		return ctx.JSON(500, map[string]any{
			"error": err.Error(),
		})
	}

	return ctx.JSON(200, map[string]any{
		"message": "snapshot created successfully",
	})
}

// HandleAddNode handles add node requests.
func (a *API) HandleAddNode(ctx forge.Context) error {
	var req struct {
		NodeID  string `json:"node_id"`
		Address string `json:"address"`
		Port    int    `json:"port"`
	}

	if err := ctx.BindJSON(&req); err != nil {
		return ctx.JSON(400, map[string]any{
			"error": "invalid request body",
		})
	}

	if req.NodeID == "" || req.Address == "" || req.Port == 0 {
		return ctx.JSON(400, map[string]any{
			"error": "node_id, address, and port are required",
		})
	}

	addCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := a.service.AddNode(addCtx, req.NodeID, req.Address, req.Port); err != nil {
		return ctx.JSON(500, map[string]any{
			"error": err.Error(),
		})
	}

	return ctx.JSON(200, map[string]any{
		"message": "node added successfully",
		"node_id": req.NodeID,
	})
}

// HandleRemoveNode handles remove node requests.
func (a *API) HandleRemoveNode(ctx forge.Context) error {
	var req struct {
		NodeID string `json:"node_id"`
	}

	if err := ctx.BindJSON(&req); err != nil {
		return ctx.JSON(400, map[string]any{
			"error": "invalid request body",
		})
	}

	if req.NodeID == "" {
		return ctx.JSON(400, map[string]any{
			"error": "node_id is required",
		})
	}

	removeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := a.service.RemoveNode(removeCtx, req.NodeID); err != nil {
		return ctx.JSON(500, map[string]any{
			"error": err.Error(),
		})
	}

	return ctx.JSON(200, map[string]any{
		"message": "node removed successfully",
		"node_id": req.NodeID,
	})
}

// HandleApplyCommand handles apply command requests (for testing).
func (a *API) HandleApplyCommand(ctx forge.Context) error {
	var req internal.Command

	if err := ctx.BindJSON(&req); err != nil {
		return ctx.JSON(400, map[string]any{
			"error": "invalid request body",
		})
	}

	applyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := a.service.Apply(applyCtx, req); err != nil {
		if internal.IsNotLeaderError(err) {
			return ctx.JSON(503, map[string]any{
				"error":     "not the leader",
				"leader_id": a.service.GetLeader(),
			})
		}

		return ctx.JSON(500, map[string]any{
			"error": err.Error(),
		})
	}

	return ctx.JSON(200, map[string]any{
		"message": "command applied successfully",
	})
}

// HandleReadQuery handles read query requests.
func (a *API) HandleReadQuery(ctx forge.Context) error {
	var query any

	body := ctx.Request().Body
	if err := json.NewDecoder(body).Decode(&query); err != nil {
		return ctx.JSON(400, map[string]any{
			"error": "invalid request body",
		})
	}

	readCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := a.service.Read(readCtx, query)
	if err != nil {
		return ctx.JSON(500, map[string]any{
			"error": err.Error(),
		})
	}

	return ctx.JSON(200, map[string]any{
		"result": result,
	})
}

// RegisterRoutes registers all admin API routes.
func (a *API) RegisterRoutes(router forge.Router, prefix string) {
	// Health endpoints
	router.GET(prefix+"/health", a.HandleHealth)
	router.GET(prefix+"/health/detailed", a.HandleDetailedHealth)

	// Status endpoints
	router.GET(prefix+"/status", a.HandleStatus)
	router.GET(prefix+"/metrics", a.HandleMetrics)
	router.GET(prefix+"/leader", a.HandleGetLeader)
	router.GET(prefix+"/nodes", a.HandleListNodes)

	// Management endpoints (require leadership)
	router.POST(prefix+"/leadership/transfer", a.HandleTransferLeadership)
	router.POST(prefix+"/leadership/stepdown", a.HandleStepDown)
	router.POST(prefix+"/snapshot", a.HandleSnapshot)
	router.POST(prefix+"/nodes/add", a.HandleAddNode)
	router.POST(prefix+"/nodes/remove", a.HandleRemoveNode)

	// Data endpoints
	router.POST(prefix+"/apply", a.HandleApplyCommand)
	router.POST(prefix+"/read", a.HandleReadQuery)

	a.logger.Info("admin API routes registered",
		forge.F("prefix", prefix),
	)
}
