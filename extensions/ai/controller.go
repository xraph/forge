package ai

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/internal"
	"github.com/xraph/forge/extensions/ai/llm"
)

// AgentController provides REST API for agent management.
type AgentController struct {
	agentManager *managerImpl
	agentFactory *AgentFactory
	llm          any // placeholder, not used
	logger       forge.Logger
}

// NewAgentController creates a new agent controller.
func NewAgentController(c forge.Container) *AgentController {
	var (
		aiManager    *managerImpl
		agentFactory *AgentFactory
		logger       forge.Logger
	)

	// Resolve dependencies using helper functions

	if manager, err := GetAIManager(c); err == nil {
		if impl, ok := manager.(*managerImpl); ok {
			aiManager = impl
		}
	}

	if factory, err := GetAgentFactory(c); err == nil {
		agentFactory = factory
	}

	if l, err := forge.GetLogger(c); err == nil {
		logger = l
	}

	return &AgentController{
		agentManager: aiManager,
		agentFactory: agentFactory,
		llm:          nil, // Not needed for controller
		logger:       logger,
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
	r.GET("/agents/:id/history", ctrl.GetAgentHistory)

	// Team Management
	r.POST("/teams", ctrl.CreateTeam)
	r.GET("/teams", ctrl.ListTeams)
	r.GET("/teams/:id", ctrl.GetTeam)
	r.POST("/teams/:id/agents/:agentId", ctrl.AddAgentToTeam)
	r.DELETE("/teams/:id/agents/:agentId", ctrl.RemoveAgentFromTeam)
	r.POST("/teams/:id/execute", ctrl.ExecuteTeam)
	r.DELETE("/teams/:id", ctrl.DeleteTeam)

	// Agent Templates
	r.GET("/agents/templates", ctrl.ListTemplates)
	r.POST("/agents/templates", ctrl.CreateTemplate)
}

// CreateAgent creates a new agent dynamically.
func (ctrl *AgentController) CreateAgent(c forge.Context) error {
	var req struct {
		Name         string         `json:"name"`
		Type         string         `json:"type"`
		SystemPrompt string         `json:"system_prompt"`
		Model        string         `json:"model"`
		Provider     string         `json:"provider,omitempty"`
		Temperature  *float64       `json:"temperature,omitempty"`
		MaxTokens    *int           `json:"max_tokens,omitempty"`
		Tools        []llm.Tool     `json:"tools,omitempty"`
		Config       map[string]any `json:"config,omitempty"`
	}

	if err := c.BindJSON(&req); err != nil {
		return c.JSON(400, map[string]string{"error": "invalid request"})
	}

	// Set defaults
	if req.Model == "" {
		req.Model = "gpt-4"
	}

	if req.Provider == "" {
		req.Provider = "openai"
	}

	// Create agent definition
	def := &AgentDefinition{
		ID:           uuid.New().String(),
		Name:         req.Name,
		Type:         req.Type,
		SystemPrompt: req.SystemPrompt,
		Model:        req.Model,
		Provider:     req.Provider,
		Temperature:  req.Temperature,
		MaxTokens:    req.MaxTokens,
		Tools:        req.Tools,
		Config:       req.Config,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	// Create and register agent
	agent, err := ctrl.agentManager.CreateAgent(c.Context(), def)
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(201, map[string]any{
		"id":   agent.ID(),
		"name": agent.Name(),
		"type": agent.Type(),
	})
}

// GetAgent retrieves an agent by ID.
func (ctrl *AgentController) GetAgent(c forge.Context) error {
	agentID := c.Param("id")

	agent, err := ctrl.agentManager.LoadAgent(c.Context(), agentID)
	if err != nil {
		return c.JSON(404, map[string]string{"error": "agent not found"})
	}

	return c.JSON(200, map[string]any{
		"id":   agent.ID(),
		"name": agent.Name(),
		"type": agent.Type(),
	})
}

// UpdateAgent updates an existing agent.
func (ctrl *AgentController) UpdateAgent(c forge.Context) error {
	agentID := c.Param("id")

	var req struct {
		Name         string         `json:"name"`
		SystemPrompt string         `json:"system_prompt"`
		Model        string         `json:"model"`
		Temperature  *float64       `json:"temperature,omitempty"`
		MaxTokens    *int           `json:"max_tokens,omitempty"`
		Config       map[string]any `json:"config,omitempty"`
	}

	if err := c.BindJSON(&req); err != nil {
		return c.JSON(400, map[string]string{"error": "invalid request"})
	}

	updates := &AgentDefinition{
		ID:           agentID,
		Name:         req.Name,
		SystemPrompt: req.SystemPrompt,
		Model:        req.Model,
		Temperature:  req.Temperature,
		MaxTokens:    req.MaxTokens,
		Config:       req.Config,
		UpdatedAt:    time.Now(),
	}

	if err := ctrl.agentManager.UpdateAgent(c.Context(), agentID, updates); err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, map[string]string{"status": "updated"})
}

// DeleteAgent deletes an agent.
func (ctrl *AgentController) DeleteAgent(c forge.Context) error {
	agentID := c.Param("id")

	if err := ctrl.agentManager.DeleteAgent(c.Context(), agentID); err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, map[string]string{"status": "deleted"})
}

// ListAgents lists all agents with optional filters.
func (ctrl *AgentController) ListAgents(c forge.Context) error {
	// Parse query parameters
	limitStr := c.Query("limit")
	offsetStr := c.Query("offset")

	limit := 20 // default
	offset := 0 // default

	if limitStr != "" {
		fmt.Sscanf(limitStr, "%d", &limit)
	}

	if offsetStr != "" {
		fmt.Sscanf(offsetStr, "%d", &offset)
	}

	filter := AgentFilter{
		Type:   c.Query("type"),
		Limit:  limit,
		Offset: offset,
	}

	agents, err := ctrl.agentManager.ListAgentsWithFilter(c.Context(), filter)
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, map[string]any{
		"agents": agents,
		"total":  len(agents),
	})
}

// ExecuteAgent executes an agent task.
func (ctrl *AgentController) ExecuteAgent(c forge.Context) error {
	agentID := c.Param("id")

	var req struct {
		Type    string         `json:"type"`
		Data    any            `json:"data"`
		Context map[string]any `json:"context"`
	}

	if err := c.BindJSON(&req); err != nil {
		return c.JSON(400, map[string]string{"error": "invalid request"})
	}

	// Get agent
	agent, err := ctrl.agentManager.GetAgent(agentID)
	if err != nil {
		return c.JSON(404, map[string]string{"error": "agent not found"})
	}

	// Execute agent
	output, err := agent.Process(c.Context(), internal.AgentInput{
		Type:    req.Type,
		Data:    req.Data,
		Context: req.Context,
	})
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, output)
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

	// Get agent
	agent, err := ctrl.agentManager.GetAgent(agentID)
	if err != nil {
		return c.JSON(404, map[string]string{"error": "agent not found"})
	}

	// Process as chat
	output, err := agent.Process(c.Context(), internal.AgentInput{
		Type: "chat",
		Data: req.Message,
	})
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, map[string]any{
		"agent_id": agentID,
		"message":  output.Data,
	})
}

// GetAgentHistory retrieves execution history.
func (ctrl *AgentController) GetAgentHistory(c forge.Context) error {
	agentID := c.Param("id")

	limitStr := c.Query("limit")

	limit := 10 // default
	if limitStr != "" {
		fmt.Sscanf(limitStr, "%d", &limit)
	}

	history, err := ctrl.agentManager.GetExecutionHistory(c.Context(), agentID, limit)
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, map[string]any{
		"agent_id": agentID,
		"history":  history,
		"total":    len(history),
	})
}

// CreateTeam creates a new agent team.
func (ctrl *AgentController) CreateTeam(c forge.Context) error {
	var req struct {
		Name     string   `json:"name"`
		AgentIDs []string `json:"agent_ids"`
	}

	if err := c.BindJSON(&req); err != nil {
		return c.JSON(400, map[string]string{"error": "invalid request"})
	}

	// Create team
	teamID := uuid.New().String()
	team := NewAgentTeam(teamID, req.Name, ctrl.logger)

	// Add agents to team
	for _, agentID := range req.AgentIDs {
		agent, err := ctrl.agentManager.GetAgent(agentID)
		if err != nil {
			return c.JSON(404, map[string]string{"error": fmt.Sprintf("agent %s not found", agentID)})
		}

		team.AddAgent(agent)
	}

	// Register team
	if err := ctrl.agentManager.RegisterTeam(team); err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(201, map[string]any{
		"id":     teamID,
		"name":   req.Name,
		"agents": len(req.AgentIDs),
	})
}

// GetTeam retrieves a team by ID.
func (ctrl *AgentController) GetTeam(c forge.Context) error {
	teamID := c.Param("id")

	team, err := ctrl.agentManager.GetTeam(teamID)
	if err != nil {
		return c.JSON(404, map[string]string{"error": "team not found"})
	}

	agents := team.Agents()

	agentInfos := make([]map[string]any, len(agents))
	for i, agent := range agents {
		agentInfos[i] = map[string]any{
			"id":   agent.ID(),
			"name": agent.Name(),
			"type": agent.Type(),
		}
	}

	return c.JSON(200, map[string]any{
		"id":     team.ID(),
		"name":   team.Name(),
		"agents": agentInfos,
	})
}

// ListTeams lists all teams.
func (ctrl *AgentController) ListTeams(c forge.Context) error {
	teams := ctrl.agentManager.ListTeams()

	teamInfos := make([]map[string]any, len(teams))
	for i, team := range teams {
		teamInfos[i] = map[string]any{
			"id":          team.ID(),
			"name":        team.Name(),
			"agent_count": len(team.Agents()),
		}
	}

	return c.JSON(200, map[string]any{
		"teams": teamInfos,
		"total": len(teams),
	})
}

// AddAgentToTeam adds an agent to a team.
func (ctrl *AgentController) AddAgentToTeam(c forge.Context) error {
	teamID := c.Param("id")
	agentID := c.Param("agentId")

	team, err := ctrl.agentManager.GetTeam(teamID)
	if err != nil {
		return c.JSON(404, map[string]string{"error": "team not found"})
	}

	agent, err := ctrl.agentManager.GetAgent(agentID)
	if err != nil {
		return c.JSON(404, map[string]string{"error": "agent not found"})
	}

	team.AddAgent(agent)

	return c.JSON(200, map[string]string{"status": "agent added to team"})
}

// RemoveAgentFromTeam removes an agent from a team.
func (ctrl *AgentController) RemoveAgentFromTeam(c forge.Context) error {
	teamID := c.Param("id")
	agentID := c.Param("agentId")

	team, err := ctrl.agentManager.GetTeam(teamID)
	if err != nil {
		return c.JSON(404, map[string]string{"error": "team not found"})
	}

	if err := team.RemoveAgent(agentID); err != nil {
		return c.JSON(404, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, map[string]string{"status": "agent removed from team"})
}

// DeleteTeam deletes a team.
func (ctrl *AgentController) DeleteTeam(c forge.Context) error {
	teamID := c.Param("id")

	if err := ctrl.agentManager.DeleteTeam(teamID); err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, map[string]string{"status": "deleted"})
}

// ExecuteTeam executes a team task.
func (ctrl *AgentController) ExecuteTeam(c forge.Context) error {
	teamID := c.Param("id")

	var req struct {
		Type    string         `json:"type"`
		Data    any            `json:"data"`
		Mode    string         `json:"mode"` // sequential, parallel, collaborative
		Context map[string]any `json:"context"`
	}

	if err := c.BindJSON(&req); err != nil {
		return c.JSON(400, map[string]string{"error": "invalid request"})
	}

	// Default mode
	if req.Mode == "" {
		req.Mode = "sequential"
	}

	team, err := ctrl.agentManager.GetTeam(teamID)
	if err != nil {
		return c.JSON(404, map[string]string{"error": "team not found"})
	}

	input := internal.AgentInput{
		Type:    req.Type,
		Data:    req.Data,
		Context: req.Context,
	}

	var output any

	switch req.Mode {
	case "parallel":
		output, err = team.ExecuteParallel(c.Context(), input)
	case "collaborative":
		output, err = team.ExecuteCollaborative(c.Context(), input)
	default: // sequential
		output, err = team.Execute(c.Context(), input)
	}

	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, map[string]any{
		"team_id": teamID,
		"mode":    req.Mode,
		"output":  output,
	})
}

// ListTemplates lists all available agent templates.
func (ctrl *AgentController) ListTemplates(c forge.Context) error {
	if ctrl.agentFactory == nil {
		return c.JSON(500, map[string]string{"error": "agent factory not configured"})
	}

	templates := ctrl.agentFactory.ListTemplates()

	return c.JSON(200, map[string]any{
		"templates": templates,
		"total":     len(templates),
	})
}

// CreateTemplate creates a new agent template.
func (ctrl *AgentController) CreateTemplate(c forge.Context) error {
	var template AgentTemplate

	if err := c.BindJSON(&template); err != nil {
		return c.JSON(400, map[string]string{"error": "invalid request"})
	}

	if ctrl.agentFactory == nil {
		return c.JSON(500, map[string]string{"error": "agent factory not configured"})
	}

	ctrl.agentFactory.RegisterTemplate(template.Name, template)

	return c.JSON(201, map[string]any{
		"name":   template.Name,
		"type":   template.Type,
		"status": "registered",
	})
}
