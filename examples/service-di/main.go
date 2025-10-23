package main

import (
	"context"
	"time"

	forge "github.com/xraph/forge"
)

// UserService demonstrates service registration with DI
type UserService struct {
	logger  forge.Logger
	metrics forge.Metrics
}

func NewUserService(c forge.Container) (*UserService, error) {
	logger := forge.Must[forge.Logger](c, "logger")
	metrics := forge.Must[forge.Metrics](c, "metrics")

	return &UserService{
		logger:  logger,
		metrics: metrics,
	}, nil
}

func (s *UserService) GetUser(ctx context.Context, id string) (map[string]interface{}, error) {
	s.logger.Info("getting user", forge.F("id", id))
	s.metrics.Counter("users_get_total").Inc()

	start := time.Now()
	defer s.metrics.Histogram("users_get_duration_seconds").ObserveDuration(start)

	// Simulate user retrieval
	return map[string]interface{}{
		"id":   id,
		"name": "John Doe",
		"role": "user",
	}, nil
}

// UserController demonstrates controller pattern
type UserController struct {
	userService *UserService
}

func NewUserController(c forge.Container) *UserController {
	return &UserController{
		userService: forge.Must[*UserService](c, "userService"),
	}
}

func (ctrl *UserController) Name() string { return "user" }

func (ctrl *UserController) Routes(r forge.Router) error {
	// Register routes under /users prefix
	users := r.Group("/users")

	users.GET("/:id", ctrl.getUser)
	users.GET("", ctrl.listUsers)

	return nil
}

func (ctrl *UserController) getUser(ctx forge.Context) error {
	id := ctx.Param("id")
	user, err := ctrl.userService.GetUser(ctx.Request().Context(), id)
	if err != nil {
		return err
	}
	return ctx.JSON(200, user)
}

func (ctrl *UserController) listUsers(ctx forge.Context) error {
	// Simplified list implementation
	users := []map[string]interface{}{
		{"id": "1", "name": "John Doe"},
		{"id": "2", "name": "Jane Smith"},
	}
	return ctx.JSON(200, users)
}

func main() {
	// Create app
	app := forge.NewApp(forge.AppConfig{
		Name:        "service-di-example",
		Version:     "1.0.0",
		Description: "Example demonstrating DI and service pattern",
		Environment: "development",
	})

	// Register service with DI container
	forge.RegisterSingleton(app.Container(), "userService", NewUserService)

	// Register controller
	controller := NewUserController(app.Container())
	if err := app.RegisterController(controller); err != nil {
		app.Logger().Fatal("failed to register controller", forge.F("error", err))
	}

	// Run the application
	if err := app.Run(); err != nil {
		app.Logger().Fatal("app failed", forge.F("error", err))
	}
}
