package security

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"
)

var (
	// ErrSessionNotFound is returned when a session is not found.
	ErrSessionNotFound = errors.New("session not found")
	// ErrSessionExpired is returned when a session has expired.
	ErrSessionExpired = errors.New("session expired")
	// ErrInvalidSession is returned when session data is invalid.
	ErrInvalidSession = errors.New("invalid session")
)

// Session represents a user session with metadata and data.
type Session struct {
	// ID is the unique session identifier
	ID string `json:"id"`

	// UserID is the user identifier (optional)
	UserID string `json:"user_id,omitempty"`

	// Data holds arbitrary session data
	Data map[string]any `json:"data"`

	// CreatedAt is when the session was created
	CreatedAt time.Time `json:"created_at"`

	// ExpiresAt is when the session expires
	ExpiresAt time.Time `json:"expires_at"`

	// LastAccessedAt is when the session was last accessed
	LastAccessedAt time.Time `json:"last_accessed_at"`

	// IPAddress of the client that created the session
	IPAddress string `json:"ip_address,omitempty"`

	// UserAgent of the client that created the session
	UserAgent string `json:"user_agent,omitempty"`
}

// IsExpired returns true if the session has expired.
func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// IsValid returns true if the session is valid.
func (s *Session) IsValid() bool {
	return !s.IsExpired() && s.ID != ""
}

// Touch updates the last accessed timestamp.
func (s *Session) Touch() {
	s.LastAccessedAt = time.Now()
}

// GetID returns the session ID (implements shared.Session).
func (s *Session) GetID() string {
	return s.ID
}

// GetUserID returns the user ID (implements shared.Session).
func (s *Session) GetUserID() string {
	return s.UserID
}

// GetData retrieves a value from session data (implements shared.Session).
func (s *Session) GetData(key string) (any, bool) {
	if s.Data == nil {
		return nil, false
	}

	val, ok := s.Data[key]

	return val, ok
}

// SetData sets a value in session data (implements shared.Session).
func (s *Session) SetData(key string, value any) {
	if s.Data == nil {
		s.Data = make(map[string]any)
	}

	s.Data[key] = value
}

// DeleteData removes a value from session data (implements shared.Session).
func (s *Session) DeleteData(key string) {
	if s.Data != nil {
		delete(s.Data, key)
	}
}

// GetCreatedAt returns when the session was created (implements shared.Session).
func (s *Session) GetCreatedAt() time.Time {
	return s.CreatedAt
}

// GetExpiresAt returns when the session expires (implements shared.Session).
func (s *Session) GetExpiresAt() time.Time {
	return s.ExpiresAt
}

// GetLastAccessedAt returns when the session was last accessed (implements shared.Session).
func (s *Session) GetLastAccessedAt() time.Time {
	return s.LastAccessedAt
}

// SessionStore defines the interface for session storage backends.
type SessionStore interface {
	// Create creates a new session with the given TTL
	Create(ctx context.Context, session *Session, ttl time.Duration) error

	// Get retrieves a session by ID
	Get(ctx context.Context, sessionID string) (*Session, error)

	// Update updates an existing session
	Update(ctx context.Context, session *Session, ttl time.Duration) error

	// Delete removes a session by ID
	Delete(ctx context.Context, sessionID string) error

	// DeleteByUserID removes all sessions for a user
	DeleteByUserID(ctx context.Context, userID string) error

	// Touch updates the last accessed time and extends TTL
	Touch(ctx context.Context, sessionID string, ttl time.Duration) error

	// Cleanup removes expired sessions (for stores that don't auto-expire)
	Cleanup(ctx context.Context) (int, error)

	// Count returns the total number of active sessions
	Count(ctx context.Context) (int64, error)

	// Connect establishes connection to the backend
	Connect(ctx context.Context) error

	// Disconnect closes connection to the backend
	Disconnect(ctx context.Context) error

	// Ping checks if the backend is reachable
	Ping(ctx context.Context) error
}

// GenerateSessionID generates a cryptographically secure session ID.
func GenerateSessionID() (string, error) {
	// Generate 32 bytes (256 bits) of random data
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// Encode as base64 URL-safe
	return base64.URLEncoding.EncodeToString(b), nil
}

// NewSession creates a new session with the given TTL.
func NewSession(userID string, ttl time.Duration) (*Session, error) {
	sessionID, err := GenerateSessionID()
	if err != nil {
		return nil, err
	}

	now := time.Now()

	return &Session{
		ID:             sessionID,
		UserID:         userID,
		Data:           make(map[string]any),
		CreatedAt:      now,
		ExpiresAt:      now.Add(ttl),
		LastAccessedAt: now,
	}, nil
}
