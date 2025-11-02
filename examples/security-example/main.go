package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/security"
)

func main() {
	// Create a new Forge app
	app := forge.New(
		forge.WithName("security-example"),
		forge.WithVersion("1.0.0"),
	)

	// Register the security extension with configuration
	securityExt := security.NewExtension(
		security.WithSessionStore("inmemory"),
		security.WithSessionCookieName("my_session"),
		security.WithSessionTTL(24*time.Hour),
		security.WithSessionAutoRenew(true),
		security.WithCookieSecure(false), // Set to true in production with HTTPS
		security.WithCookieHttpOnly(true),
		security.WithCookieSameSite("lax"),
	)

	if err := app.RegisterExtension(securityExt); err != nil {
		panic(err)
	}

	// Start the app
	if err := app.Start(context.Background()); err != nil {
		panic(err)
	}

	// Get session store and cookie manager from DI container
	var sessionStore security.SessionStore
	var cookieManager *security.CookieManager

	if err := app.Container().Resolve("security.SessionStore", &sessionStore); err != nil {
		panic(fmt.Sprintf("failed to resolve session store: %v", err))
	}

	if err := app.Container().Resolve("security.CookieManager", &cookieManager); err != nil {
		panic(fmt.Sprintf("failed to resolve cookie manager: %v", err))
	}

	// Create session middleware
	sessionMiddleware := security.SessionMiddleware(security.SessionMiddlewareOptions{
		Store:         sessionStore,
		CookieManager: cookieManager,
		Config: security.SessionConfig{
			CookieName:  "my_session",
			TTL:         24 * time.Hour,
			AutoRenew:   true,
		},
		Logger:  app.Logger(),
		Metrics: app.Metrics(),
		OnSessionCreated: func(session *security.Session) {
			app.Logger().Info("session created",
				forge.F("session_id", session.ID),
				forge.F("user_id", session.UserID),
			)
		},
		OnSessionExpired: func(sessionID string) {
			app.Logger().Info("session expired",
				forge.F("session_id", sessionID),
			)
		},
		SkipPaths: []string{"/health", "/public"},
	})

	// Define routes
	router := app.Router()

	// Public route - no session required
	router.GET("/public", func(w http.ResponseWriter, r *http.Request) error {
		return forge.JSON(w, http.StatusOK, map[string]string{
			"message": "This is a public endpoint",
		})
	})

	// Login endpoint - creates a session
	router.POST("/login", func(w http.ResponseWriter, r *http.Request) error {
		// Parse credentials
		var creds struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}

		if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
			return forge.JSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid request body",
			})
		}

		// Validate credentials (simplified for demo)
		if creds.Username != "admin" || creds.Password != "password" {
			return forge.JSON(w, http.StatusUnauthorized, map[string]string{
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
				CookieName: "my_session",
				TTL:        24 * time.Hour,
			},
			map[string]interface{}{
				"username": creds.Username,
				"role":     "admin",
			},
		)

		if err != nil {
			app.Logger().Error("failed to create session", forge.F("error", err))
			return forge.JSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to create session",
			})
		}

		return forge.JSON(w, http.StatusOK, map[string]interface{}{
			"message":    "login successful",
			"session_id": session.ID,
			"user_id":    session.UserID,
		})
	})

	// Protected routes - require session
	protectedRouter := router.Group("/api")
	protectedRouter.Use(sessionMiddleware)

	// Require session middleware for all routes in this group
	unauthorizedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forge.JSON(w, http.StatusUnauthorized, map[string]string{
			"error": "unauthorized - please login first",
		})
	})
	protectedRouter.Use(security.RequireSession(unauthorizedHandler))

	// Get user profile
	protectedRouter.GET("/profile", func(w http.ResponseWriter, r *http.Request) error {
		session, ok := security.GetSession(r.Context())
		if !ok {
			return forge.JSON(w, http.StatusUnauthorized, map[string]string{
				"error": "no session found",
			})
		}

		return forge.JSON(w, http.StatusOK, map[string]interface{}{
			"session_id": session.ID,
			"user_id":    session.UserID,
			"data":       session.Data,
			"created_at": session.CreatedAt,
			"expires_at": session.ExpiresAt,
		})
	})

	// Update profile
	protectedRouter.POST("/profile", func(w http.ResponseWriter, r *http.Request) error {
		session, ok := security.GetSession(r.Context())
		if !ok {
			return forge.JSON(w, http.StatusUnauthorized, map[string]string{
				"error": "no session found",
			})
		}

		// Parse update data
		var update map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			return forge.JSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid request body",
			})
		}

		// Update session data
		for k, v := range update {
			session.Data[k] = v
		}

		// Save session
		if err := security.UpdateSession(r.Context(), sessionStore, 24*time.Hour); err != nil {
			app.Logger().Error("failed to update session", forge.F("error", err))
			return forge.JSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to update session",
			})
		}

		return forge.JSON(w, http.StatusOK, map[string]interface{}{
			"message": "profile updated",
			"data":    session.Data,
		})
	})

	// Logout endpoint
	protectedRouter.POST("/logout", func(w http.ResponseWriter, r *http.Request) error {
		if err := security.DestroySession(r.Context(), w, sessionStore, cookieManager, "my_session"); err != nil {
			app.Logger().Error("failed to destroy session", forge.F("error", err))
			return forge.JSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to logout",
			})
		}

		return forge.JSON(w, http.StatusOK, map[string]string{
			"message": "logout successful",
		})
	})

	// Cookie management examples
	router.GET("/cookies/set", func(w http.ResponseWriter, r *http.Request) error {
		// Set various cookies
		cookieManager.SetCookie(w, "theme", "dark", &security.CookieOptions{
			MaxAge: 7 * 24 * 60 * 60, // 7 days
		})

		cookieManager.SetCookie(w, "language", "en", &security.CookieOptions{
			MaxAge: 365 * 24 * 60 * 60, // 1 year
		})

		return forge.JSON(w, http.StatusOK, map[string]string{
			"message": "cookies set successfully",
		})
	})

	router.GET("/cookies/get", func(w http.ResponseWriter, r *http.Request) error {
		theme, err := cookieManager.GetCookie(r, "theme")
		if err != nil && err != security.ErrCookieNotFound {
			return err
		}

		language, err := cookieManager.GetCookie(r, "language")
		if err != nil && err != security.ErrCookieNotFound {
			return err
		}

		allCookies := cookieManager.GetAllCookies(r)
		cookieNames := make([]string, len(allCookies))
		for i, c := range allCookies {
			cookieNames[i] = c.Name
		}

		return forge.JSON(w, http.StatusOK, map[string]interface{}{
			"theme":        theme,
			"language":     language,
			"all_cookies":  cookieNames,
		})
	})

	router.GET("/cookies/delete", func(w http.ResponseWriter, r *http.Request) error {
		cookieManager.DeleteCookie(w, "theme", nil)
		cookieManager.DeleteCookie(w, "language", nil)

		return forge.JSON(w, http.StatusOK, map[string]string{
			"message": "cookies deleted successfully",
		})
	})

	// Health check endpoint
	router.GET("/health", func(w http.ResponseWriter, r *http.Request) error {
		return forge.JSON(w, http.StatusOK, map[string]string{
			"status": "healthy",
		})
	})

	// Start HTTP server
	app.Logger().Info("starting server on :8080")
	if err := app.ListenAndServe(":8080"); err != nil {
		app.Logger().Error("server error", forge.F("error", err))
	}
}

