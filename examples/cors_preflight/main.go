package main

import (
	"log"
	"net/http"

	"github.com/xraph/forge"
	"github.com/xraph/forge/middleware"
)

// User represents a user in our system.
type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func main() {
	// Create Forge application
	app := forge.New()
	router := app.Router()

	// Apply CORS middleware at router level
	// This will automatically handle OPTIONS preflight requests
	corsConfig := middleware.CORSConfig{
		AllowOrigins: []string{
			"http://localhost:3000",   // React dev server
			"http://localhost:5173",   // Vite dev server
			"https://app.example.com", // Production frontend
		},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type", "Authorization", "X-Request-ID"},
		ExposeHeaders:    []string{"X-Request-ID", "X-Total-Count"},
		AllowCredentials: true,
		MaxAge:           3600, // 1 hour
	}

	cors := middleware.CORS(corsConfig)
	router.Use(cors)

	// Register API routes
	// Note: No need to manually register OPTIONS handlers!
	// The CORS middleware will automatically handle preflight requests
	api := router.Group("/api")

	// User routes
	api.GET("/users", func(ctx forge.Context) error {
		users := []User{
			{ID: "1", Name: "John Doe", Email: "john@example.com"},
			{ID: "2", Name: "Jane Smith", Email: "jane@example.com"},
		}
		return ctx.JSON(http.StatusOK, users)
	})

	api.GET("/users/:id", func(ctx forge.Context) error {
		id := ctx.Param("id")
		user := User{
			ID:    id,
			Name:  "John Doe",
			Email: "john@example.com",
		}
		return ctx.JSON(http.StatusOK, user)
	})

	api.POST("/users", func(ctx forge.Context) error {
		var user User
		if err := ctx.Bind(&user); err != nil {
			return ctx.String(http.StatusBadRequest, "Invalid request body")
		}

		// In a real app, you would save to database here
		user.ID = "3"

		return ctx.JSON(http.StatusCreated, user)
	})

	api.PUT("/users/:id", func(ctx forge.Context) error {
		id := ctx.Param("id")
		var user User
		if err := ctx.Bind(&user); err != nil {
			return ctx.String(http.StatusBadRequest, "Invalid request body")
		}

		user.ID = id

		return ctx.JSON(http.StatusOK, user)
	})

	api.DELETE("/users/:id", func(ctx forge.Context) error {
		id := ctx.Param("id")
		return ctx.JSON(http.StatusOK, map[string]string{
			"message": "User deleted",
			"id":      id,
		})
	})

	// Health check endpoint
	router.GET("/health", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{
			"status": "ok",
		})
	})

	// Start server
	log.Println("Server starting on :8080")
	log.Println("CORS enabled for:", corsConfig.AllowOrigins)
	log.Println("\nTry these curl commands to test CORS:")
	log.Println("\n1. Preflight request:")
	log.Println("   curl -X OPTIONS http://localhost:8080/api/users \\")
	log.Println("     -H 'Origin: http://localhost:3000' \\")
	log.Println("     -H 'Access-Control-Request-Method: GET' \\")
	log.Println("     -v")
	log.Println("\n2. Actual request:")
	log.Println("   curl http://localhost:8080/api/users \\")
	log.Println("     -H 'Origin: http://localhost:3000' \\")
	log.Println("     -v")

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
