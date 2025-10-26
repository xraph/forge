package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// SimpleCacheExtension is an example extension that provides in-memory caching
type SimpleCacheExtension struct {
	*forge.BaseExtension
	cache map[string]string
	mu    sync.RWMutex
}

// NewSimpleCacheExtension creates a new simple cache extension
func NewSimpleCacheExtension() forge.Extension {
	return &SimpleCacheExtension{
		BaseExtension: forge.NewBaseExtension("simple-cache", "1.0.0", "Simple in-memory cache"),
		cache:         make(map[string]string),
	}
}

// Register registers the cache service with the DI container
func (e *SimpleCacheExtension) Register(app forge.App) error {
	// Call base registration (sets logger, metrics)
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	// Register cache service with DI
	return forge.RegisterSingleton(app.Container(), "cache", func(c forge.Container) (*SimpleCacheExtension, error) {
		return e, nil
	})
}

// Start starts the extension
func (e *SimpleCacheExtension) Start(ctx context.Context) error {
	e.MarkStarted()
	e.Logger().Info("simple cache extension started")
	return nil
}

// Stop stops the extension
func (e *SimpleCacheExtension) Stop(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Clear cache
	e.cache = make(map[string]string)
	e.MarkStopped()
	e.Logger().Info("simple cache extension stopped")
	return nil
}

// Health checks if the extension is healthy
func (e *SimpleCacheExtension) Health(ctx context.Context) error {
	if !e.IsStarted() {
		return fmt.Errorf("cache extension not started")
	}
	return nil
}

// Set stores a value in the cache
func (e *SimpleCacheExtension) Set(key, value string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cache[key] = value
}

// Get retrieves a value from the cache
func (e *SimpleCacheExtension) Get(key string) (string, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	val, ok := e.cache[key]
	return val, ok
}

// Delete removes a value from the cache
func (e *SimpleCacheExtension) Delete(key string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.cache, key)
}

// Size returns the number of items in the cache
func (e *SimpleCacheExtension) Size() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.cache)
}

// UserController demonstrates using the cache extension
type UserController struct {
	cache *SimpleCacheExtension
}

func NewUserController(c forge.Container) *UserController {
	return &UserController{
		cache: forge.Must[*SimpleCacheExtension](c, "cache"),
	}
}

func (ctrl *UserController) Name() string {
	return "user"
}

func (ctrl *UserController) Routes(r forge.Router) error {
	r.GET("/users/:id", ctrl.getUser)
	r.POST("/users", ctrl.createUser)
	r.DELETE("/users/:id", ctrl.deleteUser)
	return nil
}

func (ctrl *UserController) getUser(ctx forge.Context) error {
	id := ctx.Param("id")

	// Try to get from cache
	if user, ok := ctrl.cache.Get(id); ok {
		return ctx.JSON(200, map[string]string{
			"id":     id,
			"name":   user,
			"source": "cache",
		})
	}

	// Not in cache
	return ctx.JSON(404, map[string]string{
		"error": "user not found",
	})
}

func (ctrl *UserController) createUser(ctx forge.Context) error {
	var body struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	if err := ctx.Bind(&body); err != nil {
		return ctx.JSON(400, map[string]string{
			"error": err.Error(),
		})
	}

	// Store in cache
	ctrl.cache.Set(body.ID, body.Name)

	return ctx.JSON(201, map[string]string{
		"id":   body.ID,
		"name": body.Name,
	})
}

func (ctrl *UserController) deleteUser(ctx forge.Context) error {
	id := ctx.Param("id")
	ctrl.cache.Delete(id)

	return ctx.JSON(204, nil)
}

func main() {
	// Create cache extension
	cacheExt := NewSimpleCacheExtension()

	// Create app with extension
	app := forge.NewApp(forge.AppConfig{
		Name:        "simple-extension-demo",
		Version:     "1.0.0",
		Description: "Demonstrates simple extension usage",
		Environment: "development",
		Extensions: []forge.Extension{
			cacheExt,
		},
		HTTPAddress: ":8080",
	})

	// Register controller
	app.RegisterController(NewUserController(app.Container()))

	// Add some test data
	time.Sleep(100 * time.Millisecond) // Let app start
	if cache, err := app.GetExtension("simple-cache"); err == nil {
		cacheExt := cache.(*SimpleCacheExtension)
		cacheExt.Set("1", "Alice")
		cacheExt.Set("2", "Bob")
		cacheExt.Set("3", "Charlie")
		app.Logger().Info("added test data to cache", forge.F("count", cacheExt.Size()))
	}

	// Print info
	fmt.Println("\n🚀 Simple Extension Demo Started!")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("\nEndpoints:")
	fmt.Println("  GET    /_/info          - App info (includes extension)")
	fmt.Println("  GET    /_/health        - Health check (includes extension)")
	fmt.Println("  GET    /users/:id       - Get user from cache")
	fmt.Println("  POST   /users           - Create user in cache")
	fmt.Println("  DELETE /users/:id       - Delete user from cache")
	fmt.Println("\nTry:")
	fmt.Println("  curl http://localhost:8080/users/1")
	fmt.Println("  curl http://localhost:8080/_/info")
	fmt.Println("  curl http://localhost:8080/_/health")
	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// Run app (blocks until SIGINT/SIGTERM)
	if err := app.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}
