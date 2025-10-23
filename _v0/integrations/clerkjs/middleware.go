package clerkjs

import (
	"context"
	"net/http"
	"strings"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/jwt"
	"github.com/xraph/forge/v0/pkg/common"
)

// ClerkAuthMiddleware provides Clerk JWT authentication
type ClerkAuthMiddleware struct {
	client      clerk.Backend
	config      *ClerkConfig
	userService UserService
}

func (m *ClerkAuthMiddleware) Name() string {
	return "clerk-auth"
}

func (m *ClerkAuthMiddleware) Priority() int {
	return 10 // High priority for auth
}

func (m *ClerkAuthMiddleware) Handler() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for webhook endpoints
			if strings.HasPrefix(r.URL.Path, "/webhooks/") {
				next.ServeHTTP(w, r)
				return
			}

			// Extract JWT token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				e := common.ErrServiceUnAuthorized("Missing authorization header", nil)
				e.HandleHTTPError(http.StatusUnauthorized, w, r)
				return
			}

			if !strings.HasPrefix(authHeader, "Bearer ") {
				e := common.ErrServiceUnAuthorized("Invalid authorization header format", nil)
				e.HandleHTTPError(http.StatusUnauthorized, w, r)
				return
			}

			token := strings.TrimPrefix(authHeader, "Bearer ")

			// Verify JWT token with Clerk
			claims, err := jwt.Verify(context.Background(), &jwt.VerifyParams{
				Token: token,
			})
			if err != nil {
				e := common.ErrServiceUnAuthorized("Invalid token", nil)
				e.HandleHTTPError(http.StatusUnauthorized, w, r)
				return
			}

			// Extract user ID from claims
			userID := claims.RegisteredClaims.Subject
			if userID == "" {
				e := common.ErrServiceUnAuthorized("Invalid token claims", nil)
				e.HandleHTTPError(http.StatusUnauthorized, w, r)
				return
			}

			// Add user context to request
			ctx := r.Context()
			ctx = context.WithValue(ctx, "clerk_user_id", userID)
			ctx = context.WithValue(ctx, "clerk_claims", claims)

			// Optionally fetch user data if user service is available
			if m.userService != nil {
				user, err := m.userService.GetUserByClerkID(ctx, userID)
				if err == nil {
					ctx = context.WithValue(ctx, "user", user)
				}
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
