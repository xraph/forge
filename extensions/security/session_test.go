package security

import (
	"testing"
	"time"
)

func TestGenerateSessionID(t *testing.T) {
	// Generate multiple session IDs
	ids := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id, err := GenerateSessionID()
		if err != nil {
			t.Fatalf("failed to generate session ID: %v", err)
		}

		if id == "" {
			t.Fatal("generated empty session ID")
		}

		if ids[id] {
			t.Fatalf("duplicate session ID generated: %s", id)
		}
		ids[id] = true
	}

	if len(ids) != 1000 {
		t.Fatalf("expected 1000 unique IDs, got %d", len(ids))
	}
}

func TestNewSession(t *testing.T) {
	userID := "user123"
	ttl := 1 * time.Hour

	session, err := NewSession(userID, ttl)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	if session.ID == "" {
		t.Error("session ID is empty")
	}

	if session.UserID != userID {
		t.Errorf("expected user ID %s, got %s", userID, session.UserID)
	}

	if session.Data == nil {
		t.Error("session data is nil")
	}

	if session.CreatedAt.IsZero() {
		t.Error("created at is zero")
	}

	if session.LastAccessedAt.IsZero() {
		t.Error("last accessed at is zero")
	}

	if session.ExpiresAt.IsZero() {
		t.Error("expires at is zero")
	}

	expectedExpiry := session.CreatedAt.Add(ttl)
	if !session.ExpiresAt.Equal(expectedExpiry) {
		t.Errorf("expected expiry %v, got %v", expectedExpiry, session.ExpiresAt)
	}
}

func TestSessionIsExpired(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{
			name:      "not expired",
			expiresAt: time.Now().Add(1 * time.Hour),
			want:      false,
		},
		{
			name:      "expired",
			expiresAt: time.Now().Add(-1 * time.Hour),
			want:      true,
		},
		{
			name:      "just expired",
			expiresAt: time.Now().Add(-1 * time.Millisecond),
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := &Session{
				ID:        "test-id",
				ExpiresAt: tt.expiresAt,
			}

			if got := session.IsExpired(); got != tt.want {
				t.Errorf("IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSessionIsValid(t *testing.T) {
	tests := []struct {
		name    string
		session *Session
		want    bool
	}{
		{
			name: "valid session",
			session: &Session{
				ID:        "test-id",
				ExpiresAt: time.Now().Add(1 * time.Hour),
			},
			want: true,
		},
		{
			name: "expired session",
			session: &Session{
				ID:        "test-id",
				ExpiresAt: time.Now().Add(-1 * time.Hour),
			},
			want: false,
		},
		{
			name: "empty ID",
			session: &Session{
				ID:        "",
				ExpiresAt: time.Now().Add(1 * time.Hour),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.session.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSessionTouch(t *testing.T) {
	session := &Session{
		ID:             "test-id",
		LastAccessedAt: time.Now().Add(-1 * time.Hour),
	}

	oldAccessTime := session.LastAccessedAt
	time.Sleep(10 * time.Millisecond)

	session.Touch()

	if !session.LastAccessedAt.After(oldAccessTime) {
		t.Error("Touch() did not update last accessed time")
	}
}
