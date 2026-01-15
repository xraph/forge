package ai

import (
	"time"

	"github.com/google/uuid"
	"github.com/xraph/forge"
)

// AgentController provides REST API for agent management.
type AgentController struct {
	agentMgr *AgentManager
	logger   forge.Logger
}

// NewAgentController creates a new agent controller.
func NewAgentController(c forge.Container) *AgentController {
	var (
		agentMgr *AgentManager
		logger   forge.Logger
	)

	// Resolve dependencies
	if mgr, err := c.Resolve(AgentManagerKey); err == nil {
		if am, ok := mgr.(*AgentManager); ok {
			agentMgr = am
		}
	}

	if l, err := forge.GetLogger(c); err == nil {
		logger = l
	}

	return &AgentController{
		agentMgr: agentMgr,
		logger:   logger,
	}
}

// Routes registers all agent management routes.
func (ctrl *AgentController) Routes(r forge.Router) {
	// Agent CRUD
	r.GET("/agents", ctrl.ListAgents)
	r.POST("/agents", ctrl.CreateAgent)
	r.GET("/agents/:id", ctrl.GetAgent)
	r.PUT("/agents/:id", ctrl.UpdateAgent)
	r.DELETE("/agents/:id", ctrl.DeleteAgent)

	// Agent Execution
	r.POST("/agents/:id/execute", ctrl.ExecuteAgent)
	r.POST("/agents/:id/chat", ctrl.ChatWithAgent)

	// Agent Templates
	r.GET("/agents/templates", ctrl.ListTemplates)
}

// CreateAgent creates a new agent dynamically.
func (ctrl *AgentController) CreateAgent(c forge.Context) error {
	var req struct {
		Name        string                 `json:"name"`
		Type        string                 `json:"type"`
		Model       string                 `json:"model"`
		Temperature float64                `json:"temperature"`
		Config      map[string]interface{} `json:"config"`
	}

	if err := c.BindJSON(&req); err != nil {
		return c.JSON(400, map[string]string{"error": "invalid request"})
	}

	// Set defaults
	if req.Model == "" {
		req.Model = "gpt-4"
	}

	if req.Temperature == 0 {
		req.Temperature = 0.7
	}

	if req.Config == nil {
		req.Config = make(map[string]interface{})
	}

	// Create agent definition
	def := &AgentDefinition{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Type:        req.Type,
		Model:       req.Model,
		Temperature: req.Temperature,
		Config:      req.Config,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Create agent
	agent, err := ctrl.agentMgr.CreateAgent(c.Context(), def)
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	if agent == nil {
		return c.JSON(500, map[string]string{"error": "failed to create agent"})
	}

	return c.JSON(201, map[string]any{
		"id":   def.ID,
		"name": def.Name,
		"type": def.Type,
	})
}

// GetAgent retrieves an agent by ID.
func (ctrl *AgentController) GetAgent(c forge.Context) error {
	agentID := c.Param("id")

	def, err := ctrl.agentMgr.GetDefinition(c.Context(), agentID)
	if err != nil {
		return c.JSON(404, map[string]string{"error": "agent not found"})
	}

	return c.JSON(200, def)
}

// UpdateAgent updates an existing agent.
func (ctrl *AgentController) UpdateAgent(c forge.Context) error {
	agentID := c.Param("id")

	var req struct {
		Name        string                 `json:"name"`
		Model       string                 `json:"model"`
		Temperature float64                `json:"temperature"`
		Config      map[string]interface{} `json:"config"`
	}

	if err := c.BindJSON(&req); err != nil {
		return c.JSON(400, map[string]string{"error": "invalid request"})
	}

	updates := &AgentDefinition{
		Name:        req.Name,
		Model:       req.Model,
		Temperature: req.Temperature,
		Config:      req.Config,
		UpdatedAt:   time.Now(),
	}

	if err := ctrl.agentMgr.UpdateAgent(c.Context(), agentID, updates); err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, map[string]string{"status": "updated"})
}

// DeleteAgent deletes an agent.
func (ctrl *AgentController) DeleteAgent(c forge.Context) error {
	agentID := c.Param("id")

	if err := ctrl.agentMgr.DeleteAgent(c.Context(), agentID); err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, map[string]string{"status": "deleted"})
}

// ListAgents lists all agents.
func (ctrl *AgentController) ListAgents(c forge.Context) error {
	agents, err := ctrl.agentMgr.ListAgents(c.Context())
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, map[string]any{
		"agents": agents,
		"total":  len(agents),
	})
}

// ExecuteAgent executes an agent with a message.
func (ctrl *AgentController) ExecuteAgent(c forge.Context) error {
	agentID := c.Param("id")

	var req struct {
		Message string `json:"message"`
	}

	if err := c.BindJSON(&req); err != nil {
		return c.JSON(400, map[string]string{"error": "invalid request"})
	}

	agent, err := ctrl.agentMgr.GetAgent(c.Context(), agentID)
	if err != nil {
		return c.JSON(404, map[string]string{"error": "agent not found"})
	}

	// Execute using ai-sdk
	result, err := agent.Execute(c.Context(), req.Message)
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, map[string]any{
		"agent_id": agentID,
		"response": result,
	})
}

// ChatWithAgent provides conversational interface with an agent.
func (ctrl *AgentController) ChatWithAgent(c forge.Context) error {
	agentID := c.Param("id")

	var req struct {
		Message string `json:"message"`
	}

	if err := c.BindJSON(&req); err != nil {
		return c.JSON(400, map[string]string{"error": "invalid request"})
	}

	agent, err := ctrl.agentMgr.GetAgent(c.Context(), agentID)
	if err != nil {
		return c.JSON(404, map[string]string{"error": "agent not found"})
	}

	// Execute chat using ai-sdk
	result, err := agent.Execute(c.Context(), req.Message)
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, map[string]any{
		"agent_id": agentID,
		"response": result,
	})
}

// ListTemplates lists all available agent templates.
func (ctrl *AgentController) ListTemplates(c forge.Context) error {
	if ctrl.agentMgr == nil || ctrl.agentMgr.factory == nil {
		return c.JSON(500, map[string]string{"error": "agent factory not configured"})
	}

	templates := ctrl.agentMgr.factory.ListTemplates()

	return c.JSON(200, map[string]any{
		"templates": templates,
		"total":     len(templates),
	})
}
