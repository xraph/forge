package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/security"
)

// Helper function to write JSON responses
func writeJSON(w http.ResponseWriter, status int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(data)
}

func main() {
	// Create a new Forge app
	app := forge.New(forge.AppConfig{
		Name:    "security-auto-middleware-example",
		Version: "1.0.0",
	})

	// Register the security extension with AUTO-APPLY middleware enabled
	// This automatically applies session middleware to ALL routes (except skip paths)
	securityExt := security.NewExtension(
		security.WithSessionStore("inmemory"),
		security.WithSessionCookieName("auto_session"),
		security.WithSessionTTL(24*time.Hour),
		security.WithSessionAutoRenew(true),
		security.WithCookieSecure(false), // Set to true in production with HTTPS
		security.WithCookieHttpOnly(true),
		security.WithCookieSameSite("lax"),
		// Enable automatic global middleware application
		security.WithAutoApplyMiddleware(true),
		// Skip session handling for these paths
		security.WithSkipPaths([]string{"/health", "/public"}),
	)

	if err := app.RegisterExtension(securityExt); err != nil {
		panic(err)
	}

	// Start the app - middleware is automatically applied during startup
	if err := app.Start(context.Background()); err != nil {
		panic(err)
	}

	// Get session store and cookie manager from DI container
	var sessionStore security.SessionStore
	var cookieManager *security.CookieManager

	sessionStore = forge.Must[security.SessionStore](app.Container(), "security.SessionStore")
	cookieManager = forge.Must[*security.CookieManager](app.Container(), "security.CookieManager")

	// Define routes
	router := app.Router()

	// Public route - session middleware is skipped due to skip paths
	router.GET("/public", func(w http.ResponseWriter, r *http.Request) error {
		// Session should not be available here
		_, hasSession := security.GetSession(r.Context())
		return writeJSON(w, http.StatusOK, map[string]interface{}{
			"message":     "This is a public endpoint",
			"has_session": hasSession,
		})
	})

	// Health check - session middleware is skipped
	router.GET("/health", func(w http.ResponseWriter, r *http.Request) error {
		return writeJSON(w, http.StatusOK, map[string]string{
			"status": "healthy",
		})
	})

	// Login endpoint - creates a session
	router.POST("/login", func(w http.ResponseWriter, r *http.Request) error {
		var creds struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}

		if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
			return writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid request body",
			})
		}

		// Validate credentials (simplified for demo)
		if creds.Username != "admin" || creds.Password != "password" {
			return writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "invalid credentials",
			})
		}

		// Create session
		session, err := security.CreateSession(
			r.Context(),
			w,
			creds.Username,
			sessionStore,
			cookieManager,
			security.SessionConfig{
				CookieName: "auto_session",
				TTL:        24 * time.Hour,
			},
			map[string]interface{}{
				"username": creds.Username,
				"role":     "admin",
			},
		)

		if err != nil {
			app.Logger().Error("failed to create session", forge.F("error", err))
			return writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to create session",
			})
		}

		return writeJSON(w, http.StatusOK, map[string]interface{}{
			"message":    "login successful",
			"session_id": session.ID,
			"user_id":    session.UserID,
		})
	})

	// These routes automatically have session middleware applied!
	// No need to manually add middleware to each route or group

	// Profile endpoint - session is automatically available
	router.GET("/profile", func(w http.ResponseWriter, r *http.Request) error {
		// Session middleware is automatically applied, so we can check for session
		session, ok := security.GetSession(r.Context())
		if !ok {
			return writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "no session - please login first",
			})
		}

		return writeJSON(w, http.StatusOK, map[string]interface{}{
			"session_id": session.ID,
			"user_id":    session.UserID,
			"data":       session.Data,
			"created_at": session.CreatedAt,
			"expires_at": session.ExpiresAt,
		})
	})

	// Dashboard - session is automatically checked
	router.GET("/dashboard", func(w http.ResponseWriter, r *http.Request) error {
		session, ok := security.GetSession(r.Context())
		if !ok {
			return writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "unauthorized - please login",
			})
		}

		return writeJSON(w, http.StatusOK, map[string]interface{}{
			"message":  "Welcome to dashboard",
			"username": session.Data["username"],
			"role":     session.Data["role"],
		})
	})

	// Update profile
	router.POST("/profile", func(w http.ResponseWriter, r *http.Request) error {
		session, ok := security.GetSession(r.Context())
		if !ok {
			return writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "no session found",
			})
		}

		var update map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			return writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid request body",
			})
		}

		// Update session data
		for k, v := range update {
			session.Data[k] = v
		}

		if err := security.UpdateSession(r.Context(), sessionStore, 24*time.Hour); err != nil {
			app.Logger().Error("failed to update session", forge.F("error", err))
			return writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to update session",
			})
		}

		return writeJSON(w, http.StatusOK, map[string]interface{}{
			"message": "profile updated",
			"data":    session.Data,
		})
	})

	// Logout
	router.POST("/logout", func(w http.ResponseWriter, r *http.Request) error {
		if err := security.DestroySession(r.Context(), w, sessionStore, cookieManager, "auto_session"); err != nil {
			app.Logger().Error("failed to destroy session", forge.F("error", err))
			return writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to logout",
			})
		}

		return writeJSON(w, http.StatusOK, map[string]string{
			"message": "logout successful",
		})
	})

	// Settings - automatically protected
	router.GET("/settings", func(w http.ResponseWriter, r *http.Request) error {
		session, ok := security.GetSession(r.Context())
		if !ok {
			return writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "unauthorized",
			})
		}

		return writeJSON(w, http.StatusOK, map[string]interface{}{
			"message": "User settings",
			"user":    session.UserID,
		})
	})

	// Start HTTP server
	app.Logger().Info("starting server on :8080")
	app.Logger().Info("session middleware is AUTOMATICALLY applied to all routes (except /health and /public)")
	if err := http.ListenAndServe(":8080", app.Router()); err != nil {
		app.Logger().Error("server error", forge.F("error", err))
	}
}

