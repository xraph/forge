package clerkjs

import (
	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/user"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/router"
)

// =============================================================================
// CONTROLLER
// =============================================================================

// ClerkController provides additional Clerk-related endpoints
type ClerkController struct {
	client      clerk.Backend
	userService UserService
	config      *ClerkConfig
	middleware  []any
}

func NewClerkController(client clerk.Backend, middleware []any, userService UserService, config *ClerkConfig) *ClerkController {
	return &ClerkController{
		client:      client,
		userService: userService,
		config:      config,
		middleware:  middleware,
	}
}

func (c *ClerkController) Name() string {
	return "clerk-controller"
}

func (c *ClerkController) Prefix() string {
	return "/auth"
}

func (c *ClerkController) Dependencies() []string {
	return []string{"clerk-client"}
}

func (c *ClerkController) Middleware() []any {
	return c.middleware
}

func (c *ClerkController) Initialize(container common.Container) error {
	return nil
}

func (c *ClerkController) ConfigureRoutes(r common.Router) error {
	// User profile endpoints
	if err := r.RegisterOpinionatedHandler("GET", "/me", c.GetCurrentUser,
		router.WithSummary("Get current user profile"),
		router.WithOpenAPITags("ClerkJS"),
		router.WithDescription("Retrieves the current authenticated user's profile"),
		// router.WithMiddleware(c.middleware...),
	); err != nil {
		return err
	}

	if err := r.RegisterOpinionatedHandler("PUT", "/me", c.UpdateCurrentUser,
		router.WithSummary("Update current user profile"),
		router.WithOpenAPITags("ClerkJS"),
		router.WithDescription("Updates the current authenticated user's profile"),
		// router.WithMiddleware(c.middleware...),
	); err != nil {
		return err
	}

	// Admin endpoints
	if err := r.RegisterOpinionatedHandler("POST", "/sync-users", c.SyncUsers,
		router.WithSummary("Sync users from Clerk"),
		router.WithOpenAPITags("ClerkJS"),
		router.WithDescription("Synchronizes users from Clerk to the local database (admin only)"),
	); err != nil {
		return err
	}

	return nil
}

// GetCurrentUserRequest represents the request for getting current user
type GetCurrentUserRequest struct {
	// No additional fields needed - user comes from auth context
}

// UpdateCurrentUserRequest represents the request for updating current user
type UpdateCurrentUserRequest struct {
	FirstName       *string                `json:"firstName,omitempty"`
	LastName        *string                `json:"lastName,omitempty"`
	ProfileImageURL *string                `json:"profileImageUrl,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// SyncUsersRequest represents the request for syncing users
type SyncUsersRequest struct {
	BatchSize int  `json:"batchSize" default:"100" validate:"min=1,max=1000"`
	DryRun    bool `json:"dryRun" default:"false"`
}

func (c *ClerkController) GetCurrentUser(ctx common.Context, req GetCurrentUserRequest) (*User, error) {
	clerkUserID := ctx.Get("clerk_user_id")
	if clerkUserID == nil {
		return nil, ErrUnauthorized("User not authenticated")
	}

	if c.userService != nil {
		user, err := c.userService.GetUserByClerkID(ctx, clerkUserID.(string))
		if err != nil {
			return nil, ErrNotFound("User not found")
		}
		return user, nil
	}

	// Fallback to Clerk API
	clerkUser, err := user.Get(ctx, clerkUserID.(string))
	if err != nil {
		return nil, ErrNotFound("User not found")
	}

	// Convert Clerk user to our User struct
	user := &User{
		ID:              clerkUser.ID,
		ClerkID:         clerkUser.ID,
		Email:           clerkUser.EmailAddresses[0].EmailAddress,
		FirstName:       *clerkUser.FirstName,
		LastName:        *clerkUser.LastName,
		ProfileImageURL: *clerkUser.ImageURL,
		// CreatedAt:       time.Time(clerkUser.CreatedAt),
		// UpdatedAt:       time.Time(clerkUser.UpdatedAt),
	}

	return user, nil
}

func (c *ClerkController) UpdateCurrentUser(ctx common.Context, req UpdateCurrentUserRequest) (*User, error) {
	clerkUserID := ctx.Get("clerk_user_id")
	if clerkUserID == nil {
		return nil, ErrUnauthorized("User not authenticated")
	}

	// Update user in Clerk
	clerkUser, err := user.Update(ctx, clerkUserID.(string), &user.UpdateParams{
		FirstName: req.FirstName,
		LastName:  req.LastName,
	})
	if err != nil {
		return nil, ErrInternalError("Failed to update user", err)
	}

	// Update local user if service is available
	if c.userService != nil {
		user, err := c.userService.UpdateUserFromClerk(ctx, clerkUserID.(string), clerkUser)
		if err == nil {
			return user, nil
		}
	}

	// Convert Clerk user to our User struct
	user := &User{
		ID:              clerkUser.ID,
		ClerkID:         clerkUser.ID,
		Email:           clerkUser.EmailAddresses[0].EmailAddress,
		FirstName:       *clerkUser.FirstName,
		LastName:        *clerkUser.LastName,
		ProfileImageURL: *clerkUser.ImageURL,
		// CreatedAt:       time.Time(clerkUser.CreatedAt),
		// UpdatedAt:       time.Time(clerkUser.UpdatedAt),
	}

	return user, nil
}

func (c *ClerkController) SyncUsers(ctx common.Context, req SyncUsersRequest) (*SyncResult, error) {
	if c.userService == nil {
		return nil, ErrServiceUnavailable("User service not available")
	}

	result, err := c.userService.SyncUsersFromClerk(ctx, req.BatchSize, req.DryRun)
	if err != nil {
		return nil, ErrInternalError("Sync failed", err)
	}

	return result, nil
}
