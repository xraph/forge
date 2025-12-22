package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// UserResponse is a test response type with sensitive fields.
type UserResponse struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Password string `json:"password" sensitive:"true"`
	APIKey   string `json:"api_key" sensitive:"redact"`
	Token    string `json:"token" sensitive:"mask:***"`
}

func TestWithSensitiveFieldCleaning_ContextHandler(t *testing.T) {
	router := NewRouter()

	// Route with sensitive field cleaning enabled
	err := router.GET("/user", func(ctx Context) error {
		return ctx.JSON(200, &UserResponse{
			ID:       "123",
			Email:    "test@example.com",
			Password: "secret123",
			APIKey:   "sk-1234567890",
			Token:    "jwt-token-here",
		})
	}, WithSensitiveFieldCleaning())
	if err != nil {
		t.Fatalf("Failed to register route: %v", err)
	}

	// Make request
	req := httptest.NewRequest("GET", "/user", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}

	// Parse response
	var response UserResponse
	body, _ := io.ReadAll(rec.Body)
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check that non-sensitive fields are preserved
	if response.ID != "123" {
		t.Errorf("ID = %q, want %q", response.ID, "123")
	}
	if response.Email != "test@example.com" {
		t.Errorf("Email = %q, want %q", response.Email, "test@example.com")
	}

	// Check that sensitive fields are cleaned
	if response.Password != "" {
		t.Errorf("Password = %q, want empty string (sensitive:true)", response.Password)
	}
	if response.APIKey != "[REDACTED]" {
		t.Errorf("APIKey = %q, want %q (sensitive:redact)", response.APIKey, "[REDACTED]")
	}
	if response.Token != "***" {
		t.Errorf("Token = %q, want %q (sensitive:mask:***)", response.Token, "***")
	}
}

func TestWithoutSensitiveFieldCleaning_ContextHandler(t *testing.T) {
	router := NewRouter()

	// Route WITHOUT sensitive field cleaning
	err := router.GET("/user", func(ctx Context) error {
		return ctx.JSON(200, &UserResponse{
			ID:       "123",
			Email:    "test@example.com",
			Password: "secret123",
			APIKey:   "sk-1234567890",
			Token:    "jwt-token-here",
		})
	})
	if err != nil {
		t.Fatalf("Failed to register route: %v", err)
	}

	// Make request
	req := httptest.NewRequest("GET", "/user", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}

	// Parse response
	var response UserResponse
	body, _ := io.ReadAll(rec.Body)
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// All fields should be preserved (no cleaning)
	if response.Password != "secret123" {
		t.Errorf("Password = %q, want %q (should NOT be cleaned)", response.Password, "secret123")
	}
	if response.APIKey != "sk-1234567890" {
		t.Errorf("APIKey = %q, want %q (should NOT be cleaned)", response.APIKey, "sk-1234567890")
	}
	if response.Token != "jwt-token-here" {
		t.Errorf("Token = %q, want %q (should NOT be cleaned)", response.Token, "jwt-token-here")
	}
}

func TestWithSensitiveFieldCleaning_OpinionatedHandler(t *testing.T) {
	router := NewRouter()

	type GetUserRequest struct {
		// Note: Path params binding in opinionated handlers requires BindRequest
		// For this test, we use a simple request type
	}

	// Route with opinionated handler and sensitive field cleaning
	err := router.GET("/users/456", func(ctx Context, req *GetUserRequest) (*UserResponse, error) {
		// In opinionated handlers, get path param from context
		id := ctx.Param("id")
		if id == "" {
			id = "456" // fallback for this test
		}
		return &UserResponse{
			ID:       id,
			Email:    "test@example.com",
			Password: "secret123",
			APIKey:   "sk-1234567890",
			Token:    "jwt-token-here",
		}, nil
	}, WithSensitiveFieldCleaning())
	if err != nil {
		t.Fatalf("Failed to register route: %v", err)
	}

	// Make request
	req := httptest.NewRequest("GET", "/users/456", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}

	// Parse response
	var response UserResponse
	body, _ := io.ReadAll(rec.Body)
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check that ID is set
	if response.ID != "456" {
		t.Errorf("ID = %q, want %q", response.ID, "456")
	}

	// Check that sensitive fields are cleaned
	if response.Password != "" {
		t.Errorf("Password = %q, want empty string", response.Password)
	}
	if response.APIKey != "[REDACTED]" {
		t.Errorf("APIKey = %q, want %q", response.APIKey, "[REDACTED]")
	}
	if response.Token != "***" {
		t.Errorf("Token = %q, want %q", response.Token, "***")
	}
}

func TestWithSensitiveFieldCleaning_NestedResponse(t *testing.T) {
	type Credentials struct {
		Username string `json:"username"`
		Password string `json:"password" sensitive:"true"`
	}

	type NestedUserResponse struct {
		ID    string      `json:"id"`
		Creds Credentials `json:"credentials"`
	}

	router := NewRouter()

	err := router.GET("/user", func(ctx Context) error {
		return ctx.JSON(200, &NestedUserResponse{
			ID: "123",
			Creds: Credentials{
				Username: "john",
				Password: "secret",
			},
		})
	}, WithSensitiveFieldCleaning())
	if err != nil {
		t.Fatalf("Failed to register route: %v", err)
	}

	req := httptest.NewRequest("GET", "/user", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}

	var response NestedUserResponse
	body, _ := io.ReadAll(rec.Body)
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.Creds.Username != "john" {
		t.Errorf("Creds.Username = %q, want %q", response.Creds.Username, "john")
	}
	if response.Creds.Password != "" {
		t.Errorf("Creds.Password = %q, want empty string", response.Creds.Password)
	}
}

func TestWithSensitiveFieldCleaning_SliceResponse(t *testing.T) {
	type TokenInfo struct {
		ID    string `json:"id"`
		Value string `json:"value" sensitive:"mask:****"`
	}

	router := NewRouter()

	err := router.GET("/tokens", func(ctx Context) error {
		return ctx.JSON(200, []TokenInfo{
			{ID: "1", Value: "secret1"},
			{ID: "2", Value: "secret2"},
		})
	}, WithSensitiveFieldCleaning())
	if err != nil {
		t.Fatalf("Failed to register route: %v", err)
	}

	req := httptest.NewRequest("GET", "/tokens", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}

	var response []TokenInfo
	body, _ := io.ReadAll(rec.Body)
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(response) != 2 {
		t.Fatalf("Expected 2 tokens, got %d", len(response))
	}

	for i, token := range response {
		if token.Value != "****" {
			t.Errorf("response[%d].Value = %q, want %q", i, token.Value, "****")
		}
	}
}

func TestWithSensitiveFieldCleaning_StatusBuilder(t *testing.T) {
	router := NewRouter()

	err := router.POST("/user", func(ctx Context) error {
		return ctx.Status(http.StatusCreated).JSON(&UserResponse{
			ID:       "123",
			Email:    "test@example.com",
			Password: "secret123",
			APIKey:   "sk-1234567890",
			Token:    "jwt-token-here",
		})
	}, WithSensitiveFieldCleaning())
	if err != nil {
		t.Fatalf("Failed to register route: %v", err)
	}

	req := httptest.NewRequest("POST", "/user", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected status %d, got %d", http.StatusCreated, rec.Code)
	}

	var response UserResponse
	body, _ := io.ReadAll(rec.Body)
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check that sensitive fields are cleaned even with Status() builder
	if response.Password != "" {
		t.Errorf("Password = %q, want empty string", response.Password)
	}
	if response.APIKey != "[REDACTED]" {
		t.Errorf("APIKey = %q, want %q", response.APIKey, "[REDACTED]")
	}
}

func TestRouteInfo_SensitiveFieldCleaning(t *testing.T) {
	router := NewRouter()

	// Route with sensitive field cleaning
	err := router.GET("/secure", func(ctx Context) error {
		return ctx.NoContent(200)
	}, WithName("secure"), WithSensitiveFieldCleaning())
	if err != nil {
		t.Fatalf("Failed to register route: %v", err)
	}

	// Route without sensitive field cleaning
	err = router.GET("/public", func(ctx Context) error {
		return ctx.NoContent(200)
	}, WithName("public"))
	if err != nil {
		t.Fatalf("Failed to register route: %v", err)
	}

	// Check RouteInfo
	routes := router.Routes()
	if len(routes) != 2 {
		t.Fatalf("Expected 2 routes, got %d", len(routes))
	}

	for _, route := range routes {
		switch route.Name {
		case "secure":
			if !route.SensitiveFieldCleaning {
				t.Error("Route 'secure' should have SensitiveFieldCleaning = true")
			}
		case "public":
			if route.SensitiveFieldCleaning {
				t.Error("Route 'public' should have SensitiveFieldCleaning = false")
			}
		}
	}
}
