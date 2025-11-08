package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/mcp"
)

func TestAuthMiddleware_Success(t *testing.T) {
	config := mcp.Config{
		RequireAuth: true,
		AuthHeader:  "X-API-Key",
		AuthTokens:  []string{"valid-token-123"},
	}

	logger := forge.NewNoopLogger()
	middleware := mcp.AuthMiddleware(config, logger)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Api-Key", "valid-token-123")

	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if rec.Body.String() != "success" {
		t.Errorf("Expected body 'success', got %s", rec.Body.String())
	}
}

func TestAuthMiddleware_BearerToken(t *testing.T) {
	config := mcp.Config{
		RequireAuth: true,
		AuthHeader:  "Authorization",
		AuthTokens:  []string{"bearer-token"},
	}

	logger := forge.NewNoopLogger()
	middleware := mcp.AuthMiddleware(config, logger)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer bearer-token")

	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	config := mcp.Config{
		RequireAuth: true,
		AuthHeader:  "X-API-Key",
		AuthTokens:  []string{"valid-token"},
	}

	logger := forge.NewNoopLogger()
	middleware := mcp.AuthMiddleware(config, logger)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// No auth header
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	config := mcp.Config{
		RequireAuth: true,
		AuthHeader:  "X-API-Key",
		AuthTokens:  []string{"valid-token"},
	}

	logger := forge.NewNoopLogger()
	middleware := mcp.AuthMiddleware(config, logger)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Api-Key", "invalid-token")

	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_Disabled(t *testing.T) {
	config := mcp.Config{
		RequireAuth: false,
	}

	logger := forge.NewNoopLogger()
	middleware := mcp.AuthMiddleware(config, logger)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// No auth header, but auth is disabled
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200 when auth disabled, got %d", rec.Code)
	}
}

func TestRateLimitMiddleware_WithinLimit(t *testing.T) {
	config := mcp.Config{
		RateLimitPerMinute: 10,
	}

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	middleware := mcp.RateLimitMiddleware(config, logger, metrics)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make 5 requests (within limit of 10)
	for i := range 5 {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Request %d: Expected status 200, got %d", i+1, rec.Code)
		}
	}
}

func TestRateLimitMiddleware_ExceedLimit(t *testing.T) {
	config := mcp.Config{
		RateLimitPerMinute: 3, // Low limit for testing
	}

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	middleware := mcp.RateLimitMiddleware(config, logger, metrics)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make requests exceeding the limit
	for i := range 5 {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345" // Same client
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if i < 3 {
			// First 3 should succeed
			if rec.Code != http.StatusOK {
				t.Errorf("Request %d: Expected status 200, got %d", i+1, rec.Code)
			}
		} else {
			// After limit, should get 429
			if rec.Code != http.StatusTooManyRequests {
				t.Errorf("Request %d: Expected status 429, got %d", i+1, rec.Code)
			}

			// Should have Retry-After header
			if rec.Header().Get("Retry-After") == "" {
				t.Error("Expected Retry-After header")
			}
		}
	}
}

func TestRateLimitMiddleware_Disabled(t *testing.T) {
	config := mcp.Config{
		RateLimitPerMinute: 0, // Disabled
	}

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	middleware := mcp.RateLimitMiddleware(config, logger, metrics)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make many requests - should all succeed when rate limiting disabled
	for i := range 100 {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Request %d: Expected status 200 with rate limiting disabled, got %d", i+1, rec.Code)
		}
	}
}

func TestRateLimitMiddleware_DifferentClients(t *testing.T) {
	config := mcp.Config{
		RateLimitPerMinute: 2,
	}

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	middleware := mcp.RateLimitMiddleware(config, logger, metrics)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Client 1 - make 2 requests (at limit)
	for i := range 2 {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Client 1 Request %d: Expected status 200, got %d", i+1, rec.Code)
		}
	}

	// Client 2 - should still be able to make requests
	for i := range 2 {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.2:12345" // Different IP
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Client 2 Request %d: Expected status 200, got %d", i+1, rec.Code)
		}
	}
}

func TestRateLimiter_WindowReset(t *testing.T) {
	config := mcp.Config{
		RateLimitPerMinute: 2,
	}

	logger := forge.NewNoopLogger()

	limiter := mcp.NewRateLimiter(config, logger)
	defer limiter.Stop()

	clientID := "test-client"

	// First request
	if !limiter.Allow(clientID) {
		t.Error("First request should be allowed")
	}

	// Second request
	if !limiter.Allow(clientID) {
		t.Error("Second request should be allowed")
	}

	// Third request - should be denied (at limit)
	if limiter.Allow(clientID) {
		t.Error("Third request should be denied")
	}

	// Note: In production, the rate limit window would reset after 1 minute
	// This test verifies the limiter correctly tracks and enforces limits
	t.Log("Rate limit enforced correctly")
}
