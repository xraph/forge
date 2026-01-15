package main

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/auth"
	"github.com/xraph/forge/extensions/auth/providers"
)

// User represents a simple user model
type User struct {
	ID    string   `json:"id"`
	Email string   `json:"email"`
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}

// UserService simulates a user database service
type UserService struct {
	users map[string]*User
}

func NewUserService() *UserService {
	return &UserService{
		users: map[string]*User{
			"admin-key": {
				ID:    "admin-1",
				Email: "admin@example.com",
				Name:  "Admin User",
				Roles: []string{"admin", "user"},
			},
			"user-key": {
				ID:    "user-1",
				Email: "user@example.com",
				Name:  "Regular User",
				Roles: []string{"user"},
			},
		},
	}
}

func (s *UserService) ValidateAPIKey(ctx context.Context, apiKey string) (*auth.AuthContext, error) {
	user, ok := s.users[apiKey]
	if !ok {
		return nil, auth.ErrInvalidCredentials
	}

	return &auth.AuthContext{
		Subject: user.ID,
		Claims: map[string]interface{}{
			"email": user.Email,
			"name":  user.Name,
		},
		Scopes: user.Roles,
	}, nil
}

func (s *UserService) ValidateJWT(ctx context.Context, token string) (*auth.AuthContext, error) {
	// In a real application, you would:
	// 1. Parse and validate the JWT signature
	// 2. Check expiration
	// 3. Extract claims

	// For demo purposes, we'll do a simple check
	parts := strings.Split(token, ".")
	if len(parts) < 3 {
		return nil, auth.ErrTokenInvalid
	}

	// Simulate extracting user info from token
	// In reality, you'd decode the JWT payload
	return &auth.AuthContext{
		Subject: "jwt-user-1",
		Claims: map[string]interface{}{
			"email": "jwt@example.com",
			"name":  "JWT User",
		},
		Scopes: []string{"read:users", "write:users"},
	}, nil
}

func (s *UserService) ValidateBasicAuth(ctx context.Context, username, password string) (*auth.AuthContext, error) {
	// Simple validation - in production, hash passwords!
	if username == "admin" && password == "admin123" {
		return &auth.AuthContext{
			Subject: "basic-admin-1",
			Claims: map[string]interface{}{
				"username": username,
			},
			Scopes: []string{"admin"},
		}, nil
	}

	return nil, auth.ErrInvalidCredentials
}

func main() {
	// Create app
	app := forge.NewApp(forge.AppConfig{
		Name:    "Auth Example",
		Version: "1.0.0",
	})

	// Register auth extension
	authExt := auth.NewExtension()
	if err := app.RegisterExtension(authExt); err != nil {
		log.Fatal("Failed to register auth extension:", err)
	}

	// Start app (this initializes extensions)
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		log.Fatal("Failed to start app:", err)
	}

	// Get auth registry
	registry := forge.Must[auth.Registry](app.Container(), "auth:registry")

	// Create user service
	userService := NewUserService()

	// Register API Key provider
	apiKeyProvider := providers.NewAPIKeyProvider("api-key",
		providers.WithAPIKeyHeader("X-API-Key"),
		providers.WithAPIKeyDescription("API Key in X-API-Key header"),
		providers.WithAPIKeyValidator(userService.ValidateAPIKey),
	)
	if err := registry.Register(apiKeyProvider); err != nil {
		log.Fatal("Failed to register API key provider:", err)
	}

	// Register JWT Bearer provider
	jwtProvider := providers.NewBearerTokenProvider("jwt",
		providers.WithBearerFormat("JWT"),
		providers.WithBearerDescription("JWT token in Authorization header"),
		providers.WithBearerValidator(userService.ValidateJWT),
	)
	if err := registry.Register(jwtProvider); err != nil {
		log.Fatal("Failed to register JWT provider:", err)
	}

	// Register Basic Auth provider
	basicAuthProvider := providers.NewBasicAuthProvider("basic",
		providers.WithBasicAuthDescription("HTTP Basic Authentication"),
		providers.WithBasicAuthValidator(userService.ValidateBasicAuth),
	)
	if err := registry.Register(basicAuthProvider); err != nil {
		log.Fatal("Failed to register basic auth provider:", err)
	}

	// Register OAuth2 provider (example)
	oauth2Provider := providers.NewOAuth2Provider("oauth2",
		&auth.OAuthFlows{
			AuthorizationCode: &auth.OAuthFlow{
				AuthorizationURL: "https://example.com/oauth/authorize",
				TokenURL:         "https://example.com/oauth/token",
				Scopes: map[string]string{
					"read":  "Read access",
					"write": "Write access",
					"admin": "Admin access",
				},
			},
		},
		providers.WithOAuth2Description("OAuth 2.0 Authorization Code Flow"),
		providers.WithOAuth2Validator(userService.ValidateJWT), // Reuse JWT validator
	)
	if err := registry.Register(oauth2Provider); err != nil {
		log.Fatal("Failed to register OAuth2 provider:", err)
	}

	// Create router with OpenAPI
	router := app.Router()

	// Enable OpenAPI with auth schemes
	router = forge.NewRouter(
		forge.WithAdapter(router.Handler().(forge.RouterAdapter)),
		forge.WithContainer(app.Container()),
		forge.WithLogger(app.Logger()),
		forge.WithOpenAPI(forge.OpenAPIConfig{
			Title:       "Auth Example API",
			Description: "Demonstrates authentication with multiple providers",
			Version:     "1.0.0",
			Servers: []forge.OpenAPIServer{
				{
					URL:         "http://localhost:8080",
					Description: "Development server",
				},
			},
			UIPath:      "/swagger",
			SpecPath:    "/openapi.json",
			UIEnabled:   true,
			SpecEnabled: true,
		}),
	)

	// Public routes (no auth)
	router.GET("/", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]string{
			"message": "Welcome to the Auth Example API",
			"docs":    "Visit /swagger for API documentation",
		})
	}, forge.WithSummary("Welcome endpoint"))

	router.GET("/health", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]string{
			"status": "healthy",
			"time":   time.Now().Format(time.RFC3339),
		})
	}, forge.WithSummary("Health check"))

	// Protected route - API Key OR JWT
	router.GET("/protected", func(ctx forge.Context) error {
		authCtx, _ := auth.FromContext(ctx.Request().Context())
		return ctx.JSON(200, map[string]interface{}{
			"message":  "This is a protected resource",
			"user":     authCtx.Subject,
			"provider": authCtx.ProviderName,
			"claims":   authCtx.Claims,
		})
	},
		forge.WithAuth("api-key", "jwt"),
		forge.WithSummary("Protected endpoint"),
		forge.WithDescription("Requires either API key or JWT authentication"),
		forge.WithTags("auth"),
	)

	// Protected route - JWT only with required scopes
	router.GET("/users", func(ctx forge.Context) error {
		authCtx, _ := auth.FromContext(ctx.Request().Context())
		return ctx.JSON(200, map[string]interface{}{
			"users": []User{
				{ID: "1", Email: "user1@example.com", Name: "User 1"},
				{ID: "2", Email: "user2@example.com", Name: "User 2"},
			},
			"authenticated_as": authCtx.Subject,
		})
	},
		forge.WithRequiredAuth("jwt", "read:users"),
		forge.WithSummary("List users"),
		forge.WithDescription("Requires JWT with read:users scope"),
		forge.WithTags("users"),
		forge.WithResponseSchema(200, "Success", []User{}),
	)

	// Admin only route
	router.POST("/admin/users", func(ctx forge.Context) error {
		authCtx, _ := auth.FromContext(ctx.Request().Context())
		return ctx.JSON(201, map[string]interface{}{
			"message": "User created successfully",
			"admin":   authCtx.Subject,
		})
	},
		forge.WithRequiredAuth("jwt", "write:users", "admin"),
		forge.WithSummary("Create user (Admin only)"),
		forge.WithDescription("Requires JWT with write:users and admin scopes"),
		forge.WithTags("users", "admin"),
	)

	// Basic auth protected route
	router.GET("/basic", func(ctx forge.Context) error {
		authCtx, _ := auth.FromContext(ctx.Request().Context())
		return ctx.JSON(200, map[string]interface{}{
			"message": "Authenticated with Basic Auth",
			"user":    authCtx.Subject,
		})
	},
		forge.WithAuth("basic"),
		forge.WithSummary("Basic auth endpoint"),
		forge.WithTags("auth"),
	)

	// Group with auth (all routes inherit)
	apiV1 := router.Group("/api/v1",
		forge.WithGroupAuth("jwt"),
		forge.WithGroupTags("api", "v1"),
	)
	{
		apiV1.GET("/profile", func(ctx forge.Context) error {
			authCtx, _ := auth.FromContext(ctx.Request().Context())
			return ctx.JSON(200, map[string]interface{}{
				"id":    authCtx.Subject,
				"email": authCtx.Claims["email"],
				"name":  authCtx.Claims["name"],
			})
		}, forge.WithSummary("Get user profile"))

		apiV1.PUT("/profile", func(ctx forge.Context) error {
			authCtx, _ := auth.FromContext(ctx.Request().Context())
			return ctx.JSON(200, map[string]interface{}{
				"message": "Profile updated",
				"user":    authCtx.Subject,
			})
		}, forge.WithSummary("Update user profile"))
	}

	// Multi-auth route (requires both API key AND JWT - rare use case)
	router.GET("/high-security", func(ctx forge.Context) error {
		authCtx, _ := auth.FromContext(ctx.Request().Context())
		return ctx.JSON(200, map[string]interface{}{
			"message": "This requires multiple authentication methods",
			"user":    authCtx.Subject,
		})
	},
		forge.WithAuthAnd("api-key", "jwt"),
		forge.WithSummary("High security endpoint"),
		forge.WithDescription("Requires both API key AND JWT (uncommon)"),
		forge.WithTags("auth"),
	)

	// Start HTTP server
	log.Println("Starting server on :8080")
	log.Println("OpenAPI docs: http://localhost:8080/swagger")
	log.Println("")
	log.Println("Test with:")
	log.Println("  curl http://localhost:8080/protected -H 'X-API-Key: admin-key'")
	log.Println("  curl http://localhost:8080/protected -H 'Authorization: Bearer fake.jwt.token'")
	log.Println("  curl http://localhost:8080/basic -u admin:admin123")
	log.Println("")

	if err := http.ListenAndServe(":8080", router.Handler()); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
