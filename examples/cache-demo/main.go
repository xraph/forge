package main

import (
	"fmt"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/cache"
)

// User model for demonstration
type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// UserService demonstrates using the cache extension
type UserService struct {
	cache *cache.InMemoryCache
}

func NewUserService(c forge.Container) (any, error) {
	cacheInstance := forge.Must[*cache.InMemoryCache](c, "cache:inmemory")
	return &UserService{cache: cacheInstance}, nil
}

// GetUser retrieves a user with caching
func (s *UserService) GetUser(ctx forge.Context, id string) (*User, error) {
	// Try cache first
	var user User
	cacheKey := fmt.Sprintf("user:%s", id)

	if err := s.cache.GetJSON(ctx.Request().Context(), cacheKey, &user); err == nil {
		ctx.Set("cache_hit", "true")
		return &user, nil
	}

	// Simulate database fetch
	user = User{
		ID:    1,
		Name:  "Alice Johnson",
		Email: "alice@example.com",
	}

	// Cache for 5 minutes
	_ = s.cache.SetJSON(ctx.Request().Context(), cacheKey, user, 5*time.Minute)
	ctx.Set("cache_hit", "false")

	return &user, nil
}

// UserController handles HTTP requests
type UserController struct {
	userService *UserService
	cache       cache.Cache
}

func NewUserController(c forge.Container) *UserController {
	return &UserController{
		userService: forge.Must[*UserService](c, "userService"),
		cache:       forge.Must[cache.Cache](c, "cache"),
	}
}

func (ctrl *UserController) Name() string {
	return "user"
}

func (ctrl *UserController) Routes(r forge.Router) error {
	r.GET("/users/:id", ctrl.getUser)
	r.POST("/users/:id/invalidate", ctrl.invalidateUser)
	r.GET("/cache/stats", ctrl.cacheStats)
	return nil
}

func (ctrl *UserController) getUser(ctx forge.Context) error {
	id := ctx.Param("id")

	user, err := ctrl.userService.GetUser(ctx, id)
	if err != nil {
		return ctx.JSON(500, map[string]string{
			"error": err.Error(),
		})
	}

	// Add cache hit header
	cacheHit := ctx.Get("cache_hit")
	if cacheHit != nil {
		ctx.SetHeader("X-Cache-Hit", cacheHit.(string))
	}

	return ctx.JSON(200, user)
}

func (ctrl *UserController) invalidateUser(ctx forge.Context) error {
	id := ctx.Param("id")
	cacheKey := fmt.Sprintf("user:%s", id)

	err := ctrl.cache.Delete(ctx.Request().Context(), cacheKey)
	if err != nil {
		return ctx.JSON(500, map[string]string{
			"error": err.Error(),
		})
	}

	return ctx.JSON(200, map[string]string{
		"message": "cache invalidated",
		"key":     cacheKey,
	})
}

func (ctrl *UserController) cacheStats(ctx forge.Context) error {
	keys, err := ctrl.cache.Keys(ctx.Request().Context(), "*")
	if err != nil {
		return ctx.JSON(500, map[string]string{
			"error": err.Error(),
		})
	}

	return ctx.JSON(200, map[string]interface{}{
		"total_keys": len(keys),
		"keys":       keys,
	})
}

func main() {
	// Configure cache
	cacheConfig := cache.DefaultConfig()
	cacheConfig.DefaultTTL = 5 * time.Minute
	cacheConfig.MaxSize = 1000
	cacheConfig.CleanupInterval = 1 * time.Minute

	// Create cache extension
	cacheExt := cache.NewExtension(cache.WithConfig(cacheConfig))

	// Create app with cache extension
	app := forge.NewApp(forge.AppConfig{
		Name:        "cache-demo",
		Version:     "1.0.0",
		Description: "Demonstrates cache extension usage",
		Environment: "development",
		Extensions: []forge.Extension{
			cacheExt,
		},
		HTTPAddress: ":8080",
		MetricsConfig: forge.MetricsConfig{
			Enabled:     true,
			MetricsPath: "/_/metrics",
			Namespace:   "cache_demo",
		},
		HealthConfig: forge.HealthConfig{
			Enabled:    true,
			HealthPath: "/_/health",
		},
	})

	// Register user service
	app.RegisterService("userService", NewUserService)

	// Register controller
	app.RegisterController(NewUserController(app.Container()))

	// Print startup info
	fmt.Println("\n🚀 Cache Demo Started!")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("\n📦 Cache Extension:")
	fmt.Println("  Driver:         in-memory")
	fmt.Println("  Default TTL:    5 minutes")
	fmt.Println("  Max Size:       1000 items")
	fmt.Println("  Cleanup:        1 minute interval")
	fmt.Println("\n🌐 Endpoints:")
	fmt.Println("  GET    /users/:id              - Get user (cached)")
	fmt.Println("  POST   /users/:id/invalidate   - Invalidate cache")
	fmt.Println("  GET    /cache/stats            - Cache statistics")
	fmt.Println("  GET    /_/info                 - App info")
	fmt.Println("  GET    /_/health               - Health check")
	fmt.Println("  GET    /_/metrics              - Metrics")
	fmt.Println("\n💡 Try it:")
	fmt.Println("  # Get user (miss)")
	fmt.Println("  curl -i http://localhost:8080/users/1")
	fmt.Println("\n  # Get user again (hit)")
	fmt.Println("  curl -i http://localhost:8080/users/1")
	fmt.Println("\n  # Cache stats")
	fmt.Println("  curl http://localhost:8080/cache/stats")
	fmt.Println("\n  # Invalidate cache")
	fmt.Println("  curl -X POST http://localhost:8080/users/1/invalidate")
	fmt.Println("\n  # Extension health")
	fmt.Println("  curl http://localhost:8080/_/health")
	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	// Run app (blocks until SIGINT/SIGTERM)
	if err := app.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}
