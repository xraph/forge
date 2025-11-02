package main

import (
	"context"
	"log"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/security"
)

// Product represents a product in our system
type Product struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Price       float64 `json:"price"`
	Description string  `json:"description"`
	InStock     bool    `json:"in_stock"`
}

// Mock database
var products = map[int64]*Product{
	1: {ID: 1, Name: "Laptop", Price: 999.99, Description: "High-performance laptop", InStock: true},
	2: {ID: 2, Name: "Mouse", Price: 29.99, Description: "Wireless mouse", InStock: true},
	3: {ID: 3, Name: "Keyboard", Price: 79.99, Description: "Mechanical keyboard", InStock: false},
}

func main() {
	// Create app
	app := forge.New()

	// Register security extension for sessions
	securityExt := security.NewExtension(
		security.WithSessionStore("inmemory"),
		security.WithSessionEnabled(true),
		security.WithCookieEnabled(true),
	)
	app.RegisterExtension(securityExt)

	// Session middleware
	app.Use(SessionMiddleware(securityExt.SessionStore()))

	// Routes
	app.GET("/", HomeHandler)
	app.POST("/login", LoginHandler)
	app.POST("/logout", LogoutHandler)
	app.GET("/profile", RequireAuth(GetProfileHandler))
	app.POST("/preferences", RequireAuth(UpdatePreferencesHandler))

	// Product routes - demonstrating path parameter type conversion
	app.GET("/products", ListProductsHandler)
	app.GET("/products/:id", GetProductHandler)
	app.POST("/products/:id/price", RequireAuth(UpdateProductPriceHandler))
	app.POST("/products/:id/stock", RequireAuth(ToggleProductStockHandler))

	// Start server
	if err := app.Start(context.Background()); err != nil {
		log.Fatal(err)
	}

	log.Println("Server started on http://localhost:8080")
	select {} // Keep running
}

// HomeHandler demonstrates cookie usage
func HomeHandler(ctx forge.Context) error {
	// Check for preferences cookie
	theme := "light"
	if ctx.HasCookie("theme") {
		if t, err := ctx.Cookie("theme"); err == nil {
			theme = t
		}
	}

	// Check if user is logged in
	var username string
	if session, err := ctx.Session(); err == nil {
		if u, ok := ctx.GetSessionValue("username"); ok {
			username = u.(string)
		}
	}

	return ctx.Status(200).JSON(forge.Map{
		"message":  "Welcome to Context Example API",
		"theme":    theme,
		"username": username,
		"authenticated": username != "",
	})
}

// LoginHandler demonstrates session creation
func LoginHandler(ctx forge.Context) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := ctx.BindJSON(&req); err != nil {
		return ctx.Status(400).JSON(forge.Map{
			"error": "invalid request body",
		})
	}

	// Simple authentication (in real app, check against database)
	if req.Username == "" || req.Password == "" {
		return ctx.Status(401).JSON(forge.Map{
			"error": "invalid credentials",
		})
	}

	// Create session
	session, err := security.NewSession(req.Username, 24*time.Hour)
	if err != nil {
		return ctx.Status(500).JSON(forge.Map{
			"error": "failed to create session",
		})
	}

	// Store user data in session
	session.SetData("username", req.Username)
	session.SetData("email", req.Username+"@example.com")
	session.SetData("roles", []string{"user"})
	session.SetData("login_time", time.Now())

	// Get session store from DI
	store, err := ctx.Resolve("security.SessionStore")
	if err != nil {
		return ctx.Status(500).JSON(forge.Map{
			"error": "session store not available",
		})
	}

	sessionStore := store.(security.SessionStore)

	// Save session to store
	if err := sessionStore.Create(ctx.Context(), session, 24*time.Hour); err != nil {
		return ctx.Status(500).JSON(forge.Map{
			"error": "failed to save session",
		})
	}

	// Set session in context
	ctx.SetSession(session)

	// Set session cookie (expires in 24 hours)
	ctx.SetCookie("session_id", session.ID, 24*3600)

	// Set welcome cookie
	ctx.SetCookie("last_login", time.Now().Format(time.RFC3339), 30*24*3600)

	return ctx.Status(200).JSON(forge.Map{
		"message": "logged in successfully",
		"user": forge.Map{
			"username": req.Username,
			"email":    req.Username + "@example.com",
		},
	})
}

// LogoutHandler demonstrates session destruction
func LogoutHandler(ctx forge.Context) error {
	// Destroy session in store
	if err := ctx.DestroySession(); err != nil {
		// Log but don't fail
		log.Printf("failed to destroy session: %v", err)
	}

	// Delete cookies
	ctx.DeleteCookie("session_id")
	ctx.DeleteCookie("theme")

	return ctx.Status(200).JSON(forge.Map{
		"message": "logged out successfully",
	})
}

// GetProfileHandler demonstrates session value retrieval
func GetProfileHandler(ctx forge.Context) error {
	session, err := ctx.Session()
	if err != nil {
		return ctx.Status(401).JSON(forge.Map{
			"error": "unauthorized",
		})
	}

	username, _ := ctx.GetSessionValue("username")
	email, _ := ctx.GetSessionValue("email")
	roles, _ := ctx.GetSessionValue("roles")
	loginTime, _ := ctx.GetSessionValue("login_time")

	return ctx.Status(200).JSON(forge.Map{
		"username":   username,
		"email":      email,
		"roles":      roles,
		"login_time": loginTime,
		"session_id": session.GetID(),
		"created_at": session.GetCreatedAt(),
		"expires_at": session.GetExpiresAt(),
	})
}

// UpdatePreferencesHandler demonstrates session updates
func UpdatePreferencesHandler(ctx forge.Context) error {
	var req struct {
		Theme    string `json:"theme"`
		Language string `json:"language"`
	}

	if err := ctx.BindJSON(&req); err != nil {
		return ctx.Status(400).JSON(forge.Map{
			"error": "invalid request body",
		})
	}

	// Update session values
	ctx.SetSessionValue("theme", req.Theme)
	ctx.SetSessionValue("language", req.Language)
	ctx.SetSessionValue("updated_at", time.Now())

	// Save to store
	if err := ctx.SaveSession(); err != nil {
		return ctx.Status(500).JSON(forge.Map{
			"error": "failed to save preferences",
		})
	}

	// Also set cookie for theme (can be read without session)
	ctx.SetCookie("theme", req.Theme, 30*24*3600)

	return ctx.Status(200).JSON(forge.Map{
		"message": "preferences updated",
	})
}

// ListProductsHandler demonstrates path parameter defaults for pagination
func ListProductsHandler(ctx forge.Context) error {
	// Get pagination parameters with defaults
	page := ctx.ParamIntDefault("page", 1)
	limit := ctx.ParamIntDefault("limit", 10)
	
	// Get filtering parameters
	minPrice := ctx.ParamFloat64Default("min_price", 0.0)
	maxPrice := ctx.ParamFloat64Default("max_price", 999999.99)
	inStockOnly := ctx.ParamBoolDefault("in_stock_only", false)

	// Validate ranges
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	// Filter products
	var filtered []*Product
	for _, p := range products {
		if p.Price >= minPrice && p.Price <= maxPrice {
			if !inStockOnly || p.InStock {
				filtered = append(filtered, p)
			}
		}
	}

	return ctx.Status(200).JSON(forge.Map{
		"products": filtered,
		"page":     page,
		"limit":    limit,
		"total":    len(filtered),
	})
}

// GetProductHandler demonstrates ParamInt64 for resource IDs
func GetProductHandler(ctx forge.Context) error {
	// Parse product ID with type safety
	id, err := ctx.ParamInt64("id")
	if err != nil {
		return ctx.Status(400).JSON(forge.Map{
			"error":   "invalid product ID",
			"details": err.Error(),
		})
	}

	// Validate range
	if id < 1 {
		return ctx.Status(400).JSON(forge.Map{
			"error": "product ID must be positive",
		})
	}

	// Find product
	product, exists := products[id]
	if !exists {
		return ctx.Status(404).JSON(forge.Map{
			"error": "product not found",
		})
	}

	// Track view in session if authenticated
	if session, err := ctx.Session(); err == nil {
		views, _ := ctx.GetSessionValue("product_views")
		viewCount := 0
		if views != nil {
			viewCount = views.(int)
		}
		ctx.SetSessionValue("product_views", viewCount+1)
		_ = ctx.SaveSession()
		
		log.Printf("User %s viewed product %d", session.GetUserID(), id)
	}

	return ctx.Status(200).JSON(product)
}

// UpdateProductPriceHandler demonstrates ParamFloat64 for prices
func UpdateProductPriceHandler(ctx forge.Context) error {
	// Parse product ID
	id, err := ctx.ParamInt64("id")
	if err != nil {
		return ctx.Status(400).JSON(forge.Map{
			"error": "invalid product ID",
		})
	}

	// Parse new price
	var req struct {
		Price float64 `json:"price"`
	}

	if err := ctx.BindJSON(&req); err != nil {
		return ctx.Status(400).JSON(forge.Map{
			"error": "invalid request body",
		})
	}

	// Validate price
	if req.Price < 0 {
		return ctx.Status(400).JSON(forge.Map{
			"error": "price must be positive",
		})
	}

	// Find product
	product, exists := products[id]
	if !exists {
		return ctx.Status(404).JSON(forge.Map{
			"error": "product not found",
		})
	}

	// Update price
	oldPrice := product.Price
	product.Price = req.Price

	// Log activity in session
	ctx.SetSessionValue("last_action", "update_product_price")
	ctx.SetSessionValue("last_action_time", time.Now())
	_ = ctx.SaveSession()

	return ctx.Status(200).JSON(forge.Map{
		"message":   "price updated",
		"old_price": oldPrice,
		"new_price": req.Price,
		"product":   product,
	})
}

// ToggleProductStockHandler demonstrates ParamBool for boolean flags
func ToggleProductStockHandler(ctx forge.Context) error {
	// Parse product ID
	id, err := ctx.ParamInt64("id")
	if err != nil {
		return ctx.Status(400).JSON(forge.Map{
			"error": "invalid product ID",
		})
	}

	// Find product
	product, exists := products[id]
	if !exists {
		return ctx.Status(404).JSON(forge.Map{
			"error": "product not found",
		})
	}

	// Toggle stock status
	product.InStock = !product.InStock

	return ctx.Status(200).JSON(forge.Map{
		"message":  "stock status updated",
		"in_stock": product.InStock,
		"product":  product,
	})
}

// SessionMiddleware loads sessions from cookies
func SessionMiddleware(sessionStore security.SessionStore) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			// Try to get session ID from cookie
			sessionID, err := ctx.Cookie("session_id")
			if err == nil && sessionID != "" {
				// Load session from store
				session, err := sessionStore.Get(ctx.Context(), sessionID)
				if err == nil && session.IsValid() {
					// Set session in context
					ctx.SetSession(session)

					// Touch session to update last accessed time
					session.Touch()
					_ = sessionStore.Touch(ctx.Context(), sessionID, 1*time.Hour)
				} else if err != nil {
					// Session expired or invalid, delete cookie
					ctx.DeleteCookie("session_id")
				}
			}

			// Call next handler
			return next(ctx)
		}
	}
}

// RequireAuth is a middleware that requires authentication
func RequireAuth(next forge.Handler) forge.Handler {
	return func(ctx forge.Context) error {
		session, err := ctx.Session()
		if err != nil {
			return ctx.Status(401).JSON(forge.Map{
				"error": "unauthorized - please login",
			})
		}

		if !session.IsValid() {
			return ctx.Status(401).JSON(forge.Map{
				"error": "session expired - please login again",
			})
		}

		return next(ctx)
	}
}

