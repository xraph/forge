package validation

import (
	"context"
	"testing"
	"time"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// mockConnection for testing
type mockConnection struct {
	id     string
	userID string
}

func (m *mockConnection) ID() string                          { return m.id }
func (m *mockConnection) GetUserID() string                   { return m.userID }
func (m *mockConnection) SetUserID(userID string)             {}
func (m *mockConnection) GetSessionID() string                { return "" }
func (m *mockConnection) SetSessionID(sessionID string)       {}
func (m *mockConnection) GetMetadata(key string) (any, bool)  { return nil, false }
func (m *mockConnection) SetMetadata(key string, value any)   {}
func (m *mockConnection) GetJoinedRooms() []string            { return nil }
func (m *mockConnection) AddRoom(roomID string)               {}
func (m *mockConnection) RemoveRoom(roomID string)            {}
func (m *mockConnection) IsInRoom(roomID string) bool         { return false }
func (m *mockConnection) GetSubscriptions() []string          { return nil }
func (m *mockConnection) AddSubscription(channelID string)    {}
func (m *mockConnection) RemoveSubscription(channelID string) {}
func (m *mockConnection) IsSubscribed(channelID string) bool  { return false }
func (m *mockConnection) Read() ([]byte, error)               { return nil, nil }
func (m *mockConnection) ReadJSON(v any) error                { return nil }
func (m *mockConnection) Write(data []byte) error             { return nil }
func (m *mockConnection) WriteJSON(v any) error               { return nil }
func (m *mockConnection) Close() error                        { return nil }
func (m *mockConnection) Context() context.Context            { return context.Background() }
func (m *mockConnection) RemoteAddr() string                  { return "127.0.0.1" }
func (m *mockConnection) LocalAddr() string                   { return "127.0.0.1" }
func (m *mockConnection) GetLastActivity() time.Time          { return time.Now() }
func (m *mockConnection) UpdateActivity()                     {}
func (m *mockConnection) IsClosed() bool                      { return false }
func (m *mockConnection) MarkClosed()                         {}

func TestContentValidator_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  ContentValidatorConfig
		message *streaming.Message
		wantErr bool
	}{
		{
			name: "valid message",
			config: ContentValidatorConfig{
				MaxContentLength: 1000,
				MinContentLength: 1,
				RequireUserID:    true,
				RequireType:      true,
			},
			message: &streaming.Message{
				ID:     "msg1",
				UserID: "user1",
				Type:   "text",
				Data:   "Hello, world!",
			},
			wantErr: false,
		},
		{
			name: "missing user ID",
			config: ContentValidatorConfig{
				RequireUserID: true,
			},
			message: &streaming.Message{
				ID:   "msg1",
				Type: "text",
				Data: "Hello",
			},
			wantErr: true,
		},
		{
			name: "content too short",
			config: ContentValidatorConfig{
				MinContentLength: 10,
			},
			message: &streaming.Message{
				ID:     "msg1",
				UserID: "user1",
				Type:   "text",
				Data:   "Hi",
			},
			wantErr: true,
		},
		{
			name: "content too long",
			config: ContentValidatorConfig{
				MaxContentLength: 5,
			},
			message: &streaming.Message{
				ID:     "msg1",
				UserID: "user1",
				Type:   "text",
				Data:   "This is too long",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewContentValidator(tt.config)
			conn := &mockConnection{id: "conn1", userID: "user1"}
			ctx := context.Background()

			err := validator.Validate(ctx, tt.message, conn)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestContentValidator_ValidateURLs(t *testing.T) {
	config := ContentValidatorConfig{
		ValidateURLs: true,
	}

	validator := NewContentValidator(config)

	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name:    "valid URL",
			content: "Check out https://example.com",
			wantErr: false,
		},
		{
			name:    "invalid URL",
			content: "Check out https://",
			wantErr: true,
		},
		{
			name:    "no URL",
			content: "Just a message",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateContent(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateContent() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestContentValidator_CustomValidators(t *testing.T) {
	customValidator := func(content any) error {
		if str, ok := content.(string); ok {
			if str == "forbidden" {
				return NewValidationError("content", "forbidden word", "CUSTOM_ERROR")
			}
		}
		return nil
	}

	config := ContentValidatorConfig{
		CustomValidators: map[string]ContentValidatorFunc{
			"custom": customValidator,
		},
	}

	validator := NewContentValidator(config)

	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name:    "passes custom validation",
			content: "allowed content",
			wantErr: false,
		},
		{
			name:    "fails custom validation",
			content: "forbidden",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateContent(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateContent() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func BenchmarkContentValidator_Validate(b *testing.B) {
	config := ContentValidatorConfig{
		MaxContentLength: 10000,
		MinContentLength: 1,
		RequireUserID:    true,
		RequireType:      true,
		ValidateURLs:     true,
	}

	validator := NewContentValidator(config)
	conn := &mockConnection{id: "conn1", userID: "user1"}
	ctx := context.Background()

	msg := &streaming.Message{
		ID:     "msg1",
		UserID: "user1",
		Type:   "text",
		Data:   "This is a test message with https://example.com",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.Validate(ctx, msg, conn)
	}
}
