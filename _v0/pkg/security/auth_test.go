package security

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/v0/pkg/logger"
	"github.com/xraph/forge/v0/pkg/metrics"
)

func TestNewAuthManager(t *testing.T) {
	tests := []struct {
		name    string
		config  AuthConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: AuthConfig{
				JWTSecret:     "test-secret-key",
				PasswordSalt:  "test-salt",
				JWTExpiration: 24 * time.Hour,
			},
			wantErr: false,
		},
		{
			name: "missing JWT secret",
			config: AuthConfig{
				PasswordSalt:  "test-salt",
				JWTExpiration: 24 * time.Hour,
			},
			wantErr: true,
		},
		{
			name: "missing password salt",
			config: AuthConfig{
				JWTSecret:     "test-secret-key",
				JWTExpiration: 24 * time.Hour,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, err := NewAuthManager(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewAuthManager() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && manager == nil {
				t.Error("NewAuthManager() returned nil manager")
			}
		})
	}
}

func TestAuthManager_ValidateToken(t *testing.T) {
	config := AuthConfig{
		JWTSecret:     "test-secret-key",
		PasswordSalt:  "test-salt",
		JWTExpiration: 24 * time.Hour,
		Logger:        logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:       metrics.NewMockMetricsCollector(),
	}

	manager, err := NewAuthManager(config)
	if err != nil {
		t.Fatalf("NewAuthManager() error = %v", err)
	}

	// Create a test user
	user := &User{
		ID:          "test-user",
		Username:    "testuser",
		Email:       "test@example.com",
		Roles:       []string{"user"},
		Permissions: []string{"read"},
		Attributes:  map[string]string{"department": "engineering"},
	}

	// Generate a token
	token, err := manager.generateJWT(user)
	if err != nil {
		t.Fatalf("generateJWT() error = %v", err)
	}

	// Validate the token
	claims, err := manager.ValidateToken(context.Background(), token)
	if err != nil {
		t.Errorf("ValidateToken() error = %v", err)
	}

	if claims.UserID != user.ID {
		t.Errorf("ValidateToken() UserID = %v, want %v", claims.UserID, user.ID)
	}
	if claims.Username != user.Username {
		t.Errorf("ValidateToken() Username = %v, want %v", claims.Username, user.Username)
	}
	if claims.Email != user.Email {
		t.Errorf("ValidateToken() Email = %v, want %v", claims.Email, user.Email)
	}
}

func TestAuthManager_HashPassword(t *testing.T) {
	config := AuthConfig{
		JWTSecret:    "test-secret-key",
		PasswordSalt: "test-salt",
		Logger:       logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:      metrics.NewMockMetricsCollector(),
	}

	manager, err := NewAuthManager(config)
	if err != nil {
		t.Fatalf("NewAuthManager() error = %v", err)
	}

	password := "test-password"
	hash, err := manager.HashPassword(password)
	if err != nil {
		t.Errorf("HashPassword() error = %v", err)
	}

	if hash == "" {
		t.Error("HashPassword() returned empty hash")
	}

	// Verify the password
	if !manager.VerifyPassword(password, hash) {
		t.Error("VerifyPassword() failed for correct password")
	}

	// Test with wrong password
	if manager.VerifyPassword("wrong-password", hash) {
		t.Error("VerifyPassword() succeeded for wrong password")
	}
}

func TestAuthManager_RefreshToken(t *testing.T) {
	config := AuthConfig{
		JWTSecret:     "test-secret-key",
		PasswordSalt:  "test-salt",
		JWTExpiration: 24 * time.Hour,
		Logger:        logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:       metrics.NewMockMetricsCollector(),
	}

	manager, err := NewAuthManager(config)
	if err != nil {
		t.Fatalf("NewAuthManager() error = %v", err)
	}

	// Create a test user
	user := &User{
		ID:          "test-user",
		Username:    "testuser",
		Email:       "test@example.com",
		Roles:       []string{"user"},
		Permissions: []string{"read"},
		Attributes:  map[string]string{"department": "engineering"},
	}

	// Generate a token
	token, err := manager.generateJWT(user)
	if err != nil {
		t.Fatalf("generateJWT() error = %v", err)
	}

	// Refresh the token
	result, err := manager.RefreshToken(context.Background(), token)
	if err != nil {
		t.Errorf("RefreshToken() error = %v", err)
	}

	if !result.Success {
		t.Error("RefreshToken() returned unsuccessful result")
	}

	if result.Token == "" {
		t.Error("RefreshToken() returned empty token")
	}

	if result.User == nil {
		t.Error("RefreshToken() returned nil user")
	}
}

func TestAuthManager_ListProviders(t *testing.T) {
	config := AuthConfig{
		JWTSecret:    "test-secret-key",
		PasswordSalt: "test-salt",
		Providers: []AuthProviderConfig{
			{
				Name:    "local",
				Type:    "local",
				Enabled: true,
			},
			{
				Name:    "oauth2",
				Type:    "oauth2",
				Enabled: true,
			},
		},
		Logger:  logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics: metrics.NewMockMetricsCollector(),
	}

	manager, err := NewAuthManager(config)
	if err != nil {
		t.Fatalf("NewAuthManager() error = %v", err)
	}

	providers := manager.ListProviders()
	if len(providers) != 2 {
		t.Errorf("ListProviders() returned %d providers, want 2", len(providers))
	}
}

func TestAuthManager_GetConfig(t *testing.T) {
	config := AuthConfig{
		JWTSecret:     "test-secret-key",
		PasswordSalt:  "test-salt",
		JWTExpiration: 24 * time.Hour,
		EnableMFA:     true,
		EnableSSO:     true,
		Logger:        logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:       metrics.NewMockMetricsCollector(),
	}

	manager, err := NewAuthManager(config)
	if err != nil {
		t.Fatalf("NewAuthManager() error = %v", err)
	}

	retrievedConfig := manager.GetConfig()
	if retrievedConfig.JWTSecret != config.JWTSecret {
		t.Error("GetConfig() JWTSecret mismatch")
	}
	if retrievedConfig.PasswordSalt != config.PasswordSalt {
		t.Error("GetConfig() PasswordSalt mismatch")
	}
	if retrievedConfig.JWTExpiration != config.JWTExpiration {
		t.Error("GetConfig() JWTExpiration mismatch")
	}
	if retrievedConfig.EnableMFA != config.EnableMFA {
		t.Error("GetConfig() EnableMFA mismatch")
	}
	if retrievedConfig.EnableSSO != config.EnableSSO {
		t.Error("GetConfig() EnableSSO mismatch")
	}
}

func TestAuthManager_UpdateConfig(t *testing.T) {
	config := AuthConfig{
		JWTSecret:     "test-secret-key",
		PasswordSalt:  "test-salt",
		JWTExpiration: 24 * time.Hour,
		Logger:        logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:       metrics.NewMockMetricsCollector(),
	}

	manager, err := NewAuthManager(config)
	if err != nil {
		t.Fatalf("NewAuthManager() error = %v", err)
	}

	newConfig := AuthConfig{
		JWTSecret:     "new-secret-key",
		PasswordSalt:  "new-salt",
		JWTExpiration: 48 * time.Hour,
		EnableMFA:     true,
		EnableSSO:     true,
	}

	err = manager.UpdateConfig(newConfig)
	if err != nil {
		t.Errorf("UpdateConfig() error = %v", err)
	}

	retrievedConfig := manager.GetConfig()
	if retrievedConfig.JWTSecret != newConfig.JWTSecret {
		t.Error("UpdateConfig() JWTSecret not updated")
	}
	if retrievedConfig.PasswordSalt != newConfig.PasswordSalt {
		t.Error("UpdateConfig() PasswordSalt not updated")
	}
	if retrievedConfig.JWTExpiration != newConfig.JWTExpiration {
		t.Error("UpdateConfig() JWTExpiration not updated")
	}
	if retrievedConfig.EnableMFA != newConfig.EnableMFA {
		t.Error("UpdateConfig() EnableMFA not updated")
	}
	if retrievedConfig.EnableSSO != newConfig.EnableSSO {
		t.Error("UpdateConfig() EnableSSO not updated")
	}
}

func TestAuthManager_ConcurrentAccess(t *testing.T) {
	config := AuthConfig{
		JWTSecret:    "test-secret-key",
		PasswordSalt: "test-salt",
		Logger:       logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:      metrics.NewMockMetricsCollector(),
	}

	manager, err := NewAuthManager(config)
	if err != nil {
		t.Fatalf("NewAuthManager() error = %v", err)
	}

	// Test concurrent access to providers
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			manager.ListProviders()
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestAuthManager_InvalidToken(t *testing.T) {
	config := AuthConfig{
		JWTSecret:    "test-secret-key",
		PasswordSalt: "test-salt",
		Logger:       logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:      metrics.NewMockMetricsCollector(),
	}

	manager, err := NewAuthManager(config)
	if err != nil {
		t.Fatalf("NewAuthManager() error = %v", err)
	}

	// Test with invalid token
	_, err = manager.ValidateToken(context.Background(), "invalid-token")
	if err == nil {
		t.Error("ValidateToken() should return error for invalid token")
	}

	// Test with empty token
	_, err = manager.ValidateToken(context.Background(), "")
	if err == nil {
		t.Error("ValidateToken() should return error for empty token")
	}
}

func TestAuthManager_ExpiredToken(t *testing.T) {
	config := AuthConfig{
		JWTSecret:     "test-secret-key",
		PasswordSalt:  "test-salt",
		JWTExpiration: -1 * time.Hour, // Expired token
		Logger:        logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:       metrics.NewMockMetricsCollector(),
	}

	manager, err := NewAuthManager(config)
	if err != nil {
		t.Fatalf("NewAuthManager() error = %v", err)
	}

	// Create a test user
	user := &User{
		ID:          "test-user",
		Username:    "testuser",
		Email:       "test@example.com",
		Roles:       []string{"user"},
		Permissions: []string{"read"},
		Attributes:  map[string]string{"department": "engineering"},
	}

	// Generate an expired token
	token, err := manager.generateJWT(user)
	if err != nil {
		t.Fatalf("generateJWT() error = %v", err)
	}

	// Validate the expired token
	_, err = manager.ValidateToken(context.Background(), token)
	if err == nil {
		t.Error("ValidateToken() should return error for expired token")
	}
}
