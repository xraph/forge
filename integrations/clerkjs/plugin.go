package clerkjs

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/user"
	"github.com/rs/xid"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/router"
)

const UserServiceKey = "forge.plugin.clerkjs.user-service"
const ClientKey = "forge.plugin.clerkjs.client"

// =============================================================================
// PLUGIN IMPLEMENTATION
// =============================================================================

// ClerkPlugin implements the Clerk authentication plugin for Forge
type ClerkPlugin struct {
	id             string
	config         *ClerkConfig
	configManager  common.ConfigManager
	client         clerk.Backend
	userService    UserService
	webhookHandler *WebhookHandler
	hooks          []common.Hook
	middleware     []any
	initialized    bool
	mu             sync.RWMutex
}

// NewClerkPlugin creates a new Clerk plugin instance
func NewClerkPlugin(configManager common.ConfigManager) *ClerkPlugin {
	return &ClerkPlugin{
		id:            xid.New().String(),
		configManager: configManager,
		hooks:         make([]common.Hook, 0),
		middleware:    make([]any, 0),
	}
}

func (p *ClerkPlugin) ID() string {
	return p.id
}

func (p *ClerkPlugin) Name() string {
	return "clerk-auth"
}

func (p *ClerkPlugin) Version() string {
	return "1.0.0"
}

func (p *ClerkPlugin) Description() string {
	return "Clerk authentication and user management plugin for Forge framework"
}

func (p *ClerkPlugin) Author() string {
	return "Forge Framework"
}

func (p *ClerkPlugin) License() string {
	return "MIT"
}

func (p *ClerkPlugin) Type() common.PluginType {
	return common.PluginTypeAuth
}

func (p *ClerkPlugin) Capabilities() []common.PluginCapability {
	return []common.PluginCapability{
		{
			Name:        "authentication",
			Version:     "1.0.0",
			Description: "JWT token validation and user authentication",
			Interface:   "AuthenticationProvider",
			Methods:     []string{"ValidateToken", "GetUser", "RefreshToken"},
			Metadata: map[string]interface{}{
				"provider": "clerk",
				"features": []string{"jwt", "webhooks", "user_management"},
			},
		},
		{
			Name:        "user_management",
			Version:     "1.0.0",
			Description: "User lifecycle management with hooks",
			Interface:   "UserManager",
			Methods:     []string{"CreateUser", "UpdateUser", "DeleteUser", "GetUser"},
			Metadata: map[string]interface{}{
				"hooks": []string{"user.created", "user.updated", "user.deleted"},
			},
		},
		{
			Name:        "webhooks",
			Version:     "1.0.0",
			Description: "Clerk webhook handling for user events",
			Interface:   "WebhookHandler",
			Methods:     []string{"HandleWebhook", "VerifyWebhook"},
			Metadata: map[string]interface{}{
				"supported_events": []string{
					"user.created",
					"user.updated",
					"user.deleted",
					"session.created",
					"session.ended",
				},
			},
		},
	}
}

func (p *ClerkPlugin) Dependencies() []common.PluginDependency {
	return []common.PluginDependency{
		{
			Name:       common.LoggerKey,
			Version:    ">=1.0.0",
			Type:       "service",
			Required:   true,
			Constraint: ">=1.0.0",
		},
		// {
		// 	Name:       "http-client",
		// 	Version:    ">=1.0.0",
		// 	Type:       "service",
		// 	Required:   false,
		// 	Constraint: ">=1.0.0",
		// },
	}
}

func (p *ClerkPlugin) Initialize(ctx context.Context, container common.Container) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.initialized {
		return nil
	}

	var conf ClerkConfig
	err := p.configManager.BindWithDefault("plugins.clerk", &conf, ClerkConfig{
		EnableMiddleware: true,
	})
	if err != nil {
		return fmt.Errorf("failed to bind clerk configuration through plugins.clerk: %w", err)
	}
	p.config = &conf

	// Initialize Clerk client
	p.client = clerk.NewBackend(&clerk.BackendConfig{
		Key: &p.config.SecretKey,
	})

	// Initialize user service if provided
	if p.config.UserServiceName != "" {
		service, err := container.ResolveNamed(p.config.UserServiceName)
		if err != nil {
			return fmt.Errorf("failed to resolve user service '%s': %w", p.config.UserServiceName, err)
		}

		userService, ok := service.(UserService)
		if !ok {
			return fmt.Errorf("service '%s' does not implement UserService interface", p.config.UserServiceName)
		}
		p.userService = userService
	}

	// Initialize webhook handler
	p.webhookHandler = NewWebhookHandler(p.config, p.userService)

	// Initialize middleware
	p.initializeMiddleware(container)

	// Initialize hooks
	p.initializeHooks()

	p.initialized = true
	return nil
}

func (p *ClerkPlugin) OnStart(ctx context.Context) error {
	if p.webhookHandler != nil {
		return p.webhookHandler.Start(ctx)
	}
	return nil
}

func (p *ClerkPlugin) OnStop(ctx context.Context) error {
	if p.webhookHandler != nil {
		return p.webhookHandler.Stop(ctx)
	}
	return nil
}

func (p *ClerkPlugin) Cleanup(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.initialized = false
	p.client = nil
	p.userService = nil
	p.webhookHandler = nil

	return nil
}

func (p *ClerkPlugin) Middleware() []any {
	if !p.config.EnableGlobalMiddleware {
		return nil
	}
	return nil
}

func (p *ClerkPlugin) ConfigureRoutes(r common.Router) error {
	// Register webhook endpoint
	if p.config.WebhookEndpoint != "" {
		return r.RegisterOpinionatedHandler("POST", p.config.WebhookEndpoint, p.HandleWebhook,
			WithClerkWebhook(),
			router.WithSummary("Clerk webhook handler"),
			router.WithDescription("Handles Clerk user lifecycle webhooks"),
		)
	}
	return nil
}

func (p *ClerkPlugin) Services() []common.ServiceDefinition {
	services := []common.ServiceDefinition{
		{
			Name:      ClientKey,
			Type:      (*clerk.Client)(nil),
			Instance:  p.client,
			Singleton: true,
			Tags:      map[string]string{"plugin": "clerk-auth", "type": "client"},
		},
	}

	if p.userService != nil {
		services = append(services, common.ServiceDefinition{
			Name:      UserServiceKey,
			Type:      (*UserService)(nil),
			Instance:  p.userService,
			Singleton: true,
			Tags:      map[string]string{"plugin": "clerk-auth", "type": "user-service"},
		})
	}

	return services
}

func (p *ClerkPlugin) Controllers() []common.Controller {
	return []common.Controller{
		NewClerkController(p.client, p.middleware, p.userService, p.config),
	}
}

func (p *ClerkPlugin) Commands() []common.CLICommand {
	return []common.CLICommand{
		{
			Name:        "clerk:sync",
			Description: "Synchronize users from Clerk to local database",
			Usage:       "clerk:sync [options]",
			Category:    "auth",
			Flags: []common.CLIFlag{
				{
					Name:        "dry-run",
					ShortName:   "d",
					Description: "Show what would be synced without making changes",
					Type:        "bool",
					Default:     false,
				},
				{
					Name:        "batch-size",
					ShortName:   "b",
					Description: "Number of users to process in each batch",
					Type:        "int",
					Default:     100,
				},
			},
			Handler: p.HandleSyncCommand,
		},
	}
}

func (p *ClerkPlugin) Hooks() []common.Hook {
	return p.hooks
}

func (p *ClerkPlugin) ConfigSchema() common.ConfigSchema {
	return common.ConfigSchema{
		Version: "1.0.0",
		Type:    "object",
		Title:   "Clerk Authentication Configuration",
		Properties: map[string]common.ConfigProperty{
			"secretKey": {
				Type:        "string",
				Description: "Clerk secret key for API authentication",
				Default:     "",
			},
			"publishableKey": {
				Type:        "string",
				Description: "Clerk publishable key for frontend integration",
				Default:     "",
			},
			"webhookSecret": {
				Type:        "string",
				Description: "Secret for webhook signature verification",
				Default:     "",
			},
			"webhookEndpoint": {
				Type:        "string",
				Description: "Endpoint path for Clerk webhooks",
				Default:     "/webhooks/clerk",
			},
			"userServiceName": {
				Type:        "string",
				Description: "Name of the user service to integrate with",
				Default:     "",
			},
			"autoCreateUsers": {
				Type:        "boolean",
				Description: "Automatically create users in local database when they sign up",
				Default:     true,
			},
			"syncUserData": {
				Type:        "boolean",
				Description: "Sync user profile data from Clerk",
				Default:     true,
			},
			"enableMiddleware": {
				Type:        "boolean",
				Description: "Enable Clerk authentication middleware",
				Default:     true,
			},
		},
		Required: []string{"secretKey"},
	}
}

func (p *ClerkPlugin) Configure(config interface{}) error {
	clerkConfig, ok := config.(*ClerkConfig)
	if !ok {
		return fmt.Errorf("invalid config type, expected *ClerkConfig")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.config = clerkConfig
	return nil
}

func (p *ClerkPlugin) GetConfig() interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.config
}

func (p *ClerkPlugin) HealthCheck(ctx context.Context) error {
	if p.client == nil {
		return fmt.Errorf("clerk client not initialized")
	}

	// Test connection by fetching user count
	_, err := user.Count(ctx, &user.ListParams{})
	if err != nil {
		return fmt.Errorf("clerk API health check failed: %w", err)
	}

	return nil
}

func (p *ClerkPlugin) GetMetrics() common.PluginMetrics {
	// TODO: Implement actual metrics collection
	return common.PluginMetrics{
		CallCount:      0,
		RouteCount:     1, // webhook endpoint
		ErrorCount:     0,
		AverageLatency: 0,
		LastExecuted:   time.Now(),
		MemoryUsage:    0,
		CPUUsage:       0,
		HealthScore:    1.0,
		Uptime:         time.Since(time.Now()),
	}
}

// =============================================================================
// CONFIGURATION
// =============================================================================

// ClerkConfig holds the configuration for the Clerk plugin
type ClerkConfig struct {
	SecretKey              string `json:"secretKey" yaml:"secretKey" env:"CLERK_SECRET_KEY"`
	PublishableKey         string `json:"publishableKey" yaml:"publishableKey" env:"CLERK_PUBLISHABLE_KEY"`
	WebhookSecret          string `json:"webhookSecret" yaml:"webhookSecret" env:"CLERK_WEBHOOK_SECRET"`
	WebhookEndpoint        string `json:"webhookEndpoint" yaml:"webhookEndpoint" default:"/webhooks/clerk"`
	UserServiceName        string `json:"userServiceName" yaml:"userServiceName" default:"user-service"`
	AutoCreateUsers        bool   `json:"autoCreateUsers" yaml:"autoCreateUsers" default:"true"`
	SyncUserData           bool   `json:"syncUserData" yaml:"syncUserData" default:"true"`
	EnableMiddleware       bool   `json:"enableMiddleware" yaml:"enableMiddleware" default:"true"`
	EnableGlobalMiddleware bool   `json:"enableGlobalMiddleware" yaml:"enableGlobalMiddleware" default:"true"`
}

// =============================================================================
// USER SERVICE INTERFACE
// =============================================================================

// UserService defines the interface for user management operations
type UserService interface {
	// CreateUserFromClerk creates a user from Clerk data
	CreateUserFromClerk(ctx context.Context, clerkUser *clerk.User) (*User, error)

	// UpdateUserFromClerk updates a user with Clerk data
	UpdateUserFromClerk(ctx context.Context, clerkUserID string, clerkUser *clerk.User) (*User, error)

	// GetUserByClerkID retrieves a user by Clerk ID
	GetUserByClerkID(ctx context.Context, clerkUserID string) (*User, error)

	// UserExistsByClerkID checks if a user exists by Clerk ID
	UserExistsByClerkID(ctx context.Context, clerkUserID string) (bool, error)

	// DeleteUserByClerkID soft deletes a user by Clerk ID
	DeleteUserByClerkID(ctx context.Context, clerkUserID string) error

	// SyncUsersFromClerk syncs users from Clerk (for admin operations)
	SyncUsersFromClerk(ctx context.Context, batchSize int, dryRun bool) (*SyncResult, error)
}

// User represents a user entity in the application
type User struct {
	ID              string                 `json:"id"`
	ClerkID         string                 `json:"clerkId"`
	Email           string                 `json:"email"`
	FirstName       string                 `json:"firstName,omitempty"`
	LastName        string                 `json:"lastName,omitempty"`
	ProfileImageURL string                 `json:"profileImageUrl,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt       time.Time              `json:"createdAt"`
	UpdatedAt       time.Time              `json:"updatedAt"`
	LastSignInAt    *time.Time             `json:"lastSignInAt,omitempty"`
}

// SyncResult represents the result of a user sync operation
type SyncResult struct {
	TotalProcessed int           `json:"totalProcessed"`
	Created        int           `json:"created"`
	Updated        int           `json:"updated"`
	Errors         []string      `json:"errors,omitempty"`
	Duration       time.Duration `json:"duration"`
}

// =============================================================================
// WEBHOOK HANDLING
// =============================================================================

// WebhookHandler handles Clerk webhooks
type WebhookHandler struct {
	config      *ClerkConfig
	userService UserService
}

// NewWebhookHandler creates a new webhook handler
func NewWebhookHandler(config *ClerkConfig, userService UserService) *WebhookHandler {
	return &WebhookHandler{
		config:      config,
		userService: userService,
	}
}

func (w *WebhookHandler) Start(ctx context.Context) error {
	return nil
}

func (w *WebhookHandler) Stop(ctx context.Context) error {
	return nil
}

// ClerkWebhookEvent represents a Clerk webhook event
type ClerkWebhookEvent struct {
	Type   string                 `json:"type"`
	Object string                 `json:"object"`
	Data   map[string]interface{} `json:"data"`
}

// WebhookRequest represents the incoming webhook request
type WebhookRequest struct {
	Event ClerkWebhookEvent `json:",inline" body:"body"`
}

// WebhookResponse represents the webhook response
type WebhookResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// HandleWebhook processes Clerk webhook events
// HandleWebhook processes Clerk webhook events
func (p *ClerkPlugin) HandleWebhook(ctx common.Context, req WebhookRequest) (*WebhookResponse, error) {
	// Verify webhook signature
	signature := ctx.Request().Header.Get("svix-signature")
	timestamp := ctx.Request().Header.Get("svix-timestamp")

	if !p.verifyWebhookSignature(ctx.Request(), signature, timestamp) {
		return nil, ErrUnauthorized("Invalid webhook signature")
	}

	// Process the event
	if err := p.processWebhookEvent(ctx, &req.Event); err != nil {
		ctx.Logger().Error("Failed to process webhook event",
			logger.String("event_type", req.Event.Type),
			logger.Error(err),
		)
		return nil, ErrInternalError("Failed to process webhook", err)
	}

	return &WebhookResponse{
		Success: true,
		Message: "Webhook processed successfully",
	}, nil
}

func (p *ClerkPlugin) verifyWebhookSignature(req *http.Request, signature, timestamp string) bool {
	if p.config.WebhookSecret == "" {
		return true // Skip verification if no secret configured
	}

	// Simple signature verification (implement proper HMAC verification in production)
	expected := fmt.Sprintf("%s.%s", timestamp, p.config.WebhookSecret)
	return subtle.ConstantTimeCompare([]byte(signature), []byte(expected)) == 1
}

func (p *ClerkPlugin) processWebhookEvent(ctx common.Context, event *ClerkWebhookEvent) error {
	switch event.Type {
	case "user.created":
		return p.handleUserCreated(ctx, event)
	case "user.updated":
		return p.handleUserUpdated(ctx, event)
	case "user.deleted":
		return p.handleUserDeleted(ctx, event)
	case "session.created":
		return p.handleSessionCreated(ctx, event)
	case "session.ended":
		return p.handleSessionEnded(ctx, event)
	default:
		ctx.Logger().Warn("Unhandled webhook event type", logger.String("type", event.Type))
		return nil
	}
}

func (p *ClerkPlugin) handleUserCreated(ctx common.Context, event *ClerkWebhookEvent) error {
	if p.userService == nil || !p.config.AutoCreateUsers {
		return nil
	}

	// Convert event data to Clerk user
	clerkUser, err := p.eventDataToClerkUser(event.Data)
	if err != nil {
		return fmt.Errorf("failed to parse user data: %w", err)
	}

	// Check if user already exists
	exists, err := p.userService.UserExistsByClerkID(ctx, clerkUser.ID)
	if err != nil {
		return fmt.Errorf("failed to check if user exists: %w", err)
	}

	if exists {
		return nil // User already exists
	}

	// Create user
	user, err := p.userService.CreateUserFromClerk(ctx, clerkUser)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	// Execute hooks
	hookData := common.HookData{
		Type:    "user_created",
		Context: ctx,
		Data:    user,
		Metadata: map[string]interface{}{
			"source":     "clerk_webhook",
			"clerk_id":   clerkUser.ID,
			"event_type": event.Type,
		},
		Timestamp: time.Now(),
	}

	for _, hook := range p.hooks {
		if hook.Type() == "user_created" {
			_, err := hook.Execute(ctx, hookData)
			if err != nil {
				ctx.Logger().Error("Hook execution failed",
					logger.String("hook", hook.Name()),
					logger.Error(err),
				)
			}
		}
	}

	return nil
}

func (p *ClerkPlugin) handleUserUpdated(ctx common.Context, event *ClerkWebhookEvent) error {
	if p.userService == nil || !p.config.SyncUserData {
		return nil
	}

	clerkUser, err := p.eventDataToClerkUser(event.Data)
	if err != nil {
		return fmt.Errorf("failed to parse user data: %w", err)
	}

	user, err := p.userService.UpdateUserFromClerk(ctx, clerkUser.ID, clerkUser)
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	// Execute hooks
	hookData := common.HookData{
		Type:    "user_updated",
		Context: ctx,
		Data:    user,
		Metadata: map[string]interface{}{
			"source":     "clerk_webhook",
			"clerk_id":   clerkUser.ID,
			"event_type": event.Type,
		},
		Timestamp: time.Now(),
	}

	for _, hook := range p.hooks {
		if hook.Type() == "user_updated" {
			_, err := hook.Execute(ctx, hookData)
			if err != nil {
				ctx.Logger().Error("Hook execution failed",
					logger.String("hook", hook.Name()),
					logger.Error(err),
				)
			}
		}
	}

	return nil
}

func (p *ClerkPlugin) handleUserDeleted(ctx common.Context, event *ClerkWebhookEvent) error {
	if p.userService == nil {
		return nil
	}

	clerkUserID, ok := event.Data["id"].(string)
	if !ok {
		return fmt.Errorf("invalid user ID in webhook data")
	}

	err := p.userService.DeleteUserByClerkID(ctx, clerkUserID)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	// Execute hooks
	hookData := common.HookData{
		Type:    "user_deleted",
		Context: ctx,
		Data:    map[string]interface{}{"clerk_id": clerkUserID},
		Metadata: map[string]interface{}{
			"source":     "clerk_webhook",
			"clerk_id":   clerkUserID,
			"event_type": event.Type,
		},
		Timestamp: time.Now(),
	}

	for _, hook := range p.hooks {
		if hook.Type() == "user_deleted" {
			_, err := hook.Execute(ctx, hookData)
			if err != nil {
				ctx.Logger().Error("Hook execution failed",
					logger.String("hook", hook.Name()),
					logger.Error(err),
				)
			}
		}
	}

	return nil
}

func (p *ClerkPlugin) handleSessionCreated(ctx common.Context, event *ClerkWebhookEvent) error {
	// Execute session hooks
	hookData := common.HookData{
		Type:    "session_created",
		Context: ctx,
		Data:    event.Data,
		Metadata: map[string]interface{}{
			"source":     "clerk_webhook",
			"event_type": event.Type,
		},
		Timestamp: time.Now(),
	}

	for _, hook := range p.hooks {
		if hook.Type() == "session_created" {
			_, err := hook.Execute(ctx, hookData)
			if err != nil {
				ctx.Logger().Error("Hook execution failed",
					logger.String("hook", hook.Name()),
					logger.Error(err),
				)
			}
		}
	}

	return nil
}

func (p *ClerkPlugin) handleSessionEnded(ctx common.Context, event *ClerkWebhookEvent) error {
	// Execute session hooks
	hookData := common.HookData{
		Type:    "session_ended",
		Context: ctx,
		Data:    event.Data,
		Metadata: map[string]interface{}{
			"source":     "clerk_webhook",
			"event_type": event.Type,
		},
		Timestamp: time.Now(),
	}

	for _, hook := range p.hooks {
		if hook.Type() == "session_ended" {
			_, err := hook.Execute(ctx, hookData)
			if err != nil {
				ctx.Logger().Error("Hook execution failed",
					logger.String("hook", hook.Name()),
					logger.Error(err),
				)
			}
		}
	}

	return nil
}

func (p *ClerkPlugin) eventDataToClerkUser(data map[string]interface{}) (*clerk.User, error) {
	// Convert map to JSON and back to parse into clerk.User struct
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	var clerkUser clerk.User
	if err := json.Unmarshal(jsonData, &clerkUser); err != nil {
		return nil, err
	}

	return &clerkUser, nil
}

func (p *ClerkPlugin) initializeHooks() {
	p.hooks = []common.Hook{
		&UserCreatedHook{},
		&UserUpdatedHook{},
		&UserDeletedHook{},
		&SessionCreatedHook{},
		&SessionEndedHook{},
	}
}

// =============================================================================
// MIDDLEWARE
// =============================================================================

func (p *ClerkPlugin) initializeMiddleware(container common.Container) {
	if !p.config.EnableMiddleware {
		return
	}

	authMiddleware := &ClerkAuthMiddleware{
		client:      p.client,
		config:      p.config,
		userService: p.userService,
	}

	p.middleware = append(p.middleware, authMiddleware)
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

func getStringFromMap(m map[string]interface{}, key string) *string {
	if val, ok := m[key].(string); ok {
		return &val
	}
	return nil
}

// WithClerkWebhook returns a handler option for Clerk webhook endpoints
func WithClerkWebhook() common.HandlerOption {
	return func(info *common.RouteHandlerInfo) {
		if info.Tags == nil {
			info.Tags = make(map[string]string)
		}
		info.Tags["type"] = "webhook"
		info.Tags["provider"] = "clerk"
	}
}
