package middleware_test

import (
	"net/http"

	"github.com/xraph/forge"
	"github.com/xraph/forge/middleware"
)

// ExampleCORS_development shows a permissive CORS configuration for development.
//
//nolint:testableexamples // configuration example without console output
func ExampleCORS_development() {
	app := forge.New()
	router := app.Router()

	// Development: Allow all origins (NOT for production)
	cors := middleware.CORS(middleware.DefaultCORSConfig())

	router.Use(cors)

	router.GET("/api/users", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"user": "john"})
	})

	_ = app.Run()
}

// ExampleCORS_production shows a secure CORS configuration for production.
//
//nolint:testableexamples // configuration example without console output
func ExampleCORS_production() {
	app := forge.New()
	router := app.Router()

	// Production: Specific origins with credentials
	corsConfig := middleware.CORSConfig{
		AllowOrigins: []string{
			"https://app.example.com",
			"https://admin.example.com",
		},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE"},
		AllowHeaders:     []string{"Content-Type", "Authorization"},
		ExposeHeaders:    []string{"X-Request-ID", "X-Total-Count"},
		AllowCredentials: true, // Enable cookies/auth headers
		MaxAge:           7200, // 2 hours
	}

	cors := middleware.CORS(corsConfig)
	router.Use(cors)

	router.GET("/api/users", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"user": "john"})
	})

	_ = app.Run()
}

// ExampleCORS_wildcardSubdomain shows wildcard subdomain support.
//
//nolint:testableexamples // configuration example without console output
func ExampleCORS_wildcardSubdomain() {
	app := forge.New()
	router := app.Router()

	// Allow all subdomains of example.com
	corsConfig := middleware.CORSConfig{
		AllowOrigins:     []string{"*.example.com"},
		AllowMethods:     []string{"GET", "POST"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: false,
		MaxAge:           3600,
	}

	cors := middleware.CORS(corsConfig)
	router.Use(cors)

	router.GET("/api/data", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"data": "value"})
	})

	_ = app.Run()
}

// ExampleCORS_multipleOrigins shows configuration with multiple specific origins.
//
//nolint:testableexamples // configuration example without console output
func ExampleCORS_multipleOrigins() {
	app := forge.New()
	router := app.Router()

	corsConfig := middleware.CORSConfig{
		AllowOrigins: []string{
			"https://web.example.com",
			"https://mobile.example.com",
			"https://partner.acme.com",
		},
		AllowMethods:     []string{"GET", "POST", "PATCH"},
		AllowHeaders:     []string{"Content-Type", "Authorization", "X-API-Key"},
		ExposeHeaders:    []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           3600,
	}

	cors := middleware.CORS(corsConfig)
	router.Use(cors)

	router.POST("/api/orders", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusCreated, map[string]string{"id": "123"})
	})

	_ = app.Run()
}

// ExampleCORS_specificRoutes shows applying CORS to specific route groups.
//
//nolint:testableexamples // configuration example without console output
func ExampleCORS_specificRoutes() {
	app := forge.New()
	router := app.Router()

	// Public API: Permissive CORS
	publicCORS := middleware.CORS(middleware.CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: false,
		MaxAge:           3600,
	})

	// Private API: Strict CORS with credentials
	privateCORS := middleware.CORS(middleware.CORSConfig{
		AllowOrigins:     []string{"https://app.example.com"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE"},
		AllowHeaders:     []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
		MaxAge:           3600,
	})

	// Public routes
	public := router.Group("/public")
	public.Use(publicCORS)
	public.GET("/status", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Private routes
	private := router.Group("/api")
	private.Use(privateCORS)
	private.GET("/users", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"user": "john"})
	})

	_ = app.Run()
}
