package security

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/metrics"
)

func TestInMemorySessionStore_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	log := logger.NewTestLogger()
	met := metrics.NewNoOpMetrics()

	store := NewInMemorySessionStore(log, met)

	// Create session
	session, err := NewSession("user123", 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	if err := store.Create(ctx, session, 1*time.Hour); err != nil {
		t.Fatalf("failed to store session: %v", err)
	}

	// Get session
	retrieved, err := store.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}

	if retrieved.ID != session.ID {
		t.Errorf("expected ID %s, got %s", session.ID, retrieved.ID)
	}

	if retrieved.UserID != session.UserID {
		t.Errorf("expected user ID %s, got %s", session.UserID, retrieved.UserID)
	}
}

func TestInMemorySessionStore_GetNotFound(t *testing.T) {
	ctx := context.Background()
	log := logger.NewTestLogger()
	met := metrics.NewNoOpMetrics()

	store := NewInMemorySessionStore(log, met)

	_, err := store.Get(ctx, "nonexistent")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestInMemorySessionStore_GetExpired(t *testing.T) {
	ctx := context.Background()
	log := logger.NewTestLogger()
	met := metrics.NewNoOpMetrics()

	store := NewInMemorySessionStore(log, met)

	// Create expired session
	session, err := NewSession("user123", 1*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	if err := store.Create(ctx, session, 1*time.Millisecond); err != nil {
		t.Fatalf("failed to store session: %v", err)
	}

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	_, err = store.Get(ctx, session.ID)
	if !errors.Is(err, ErrSessionExpired) {
		t.Errorf("expected ErrSessionExpired, got %v", err)
	}
}

func TestInMemorySessionStore_Update(t *testing.T) {
	ctx := context.Background()
	log := logger.NewTestLogger()
	met := metrics.NewNoOpMetrics()

	store := NewInMemorySessionStore(log, met)

	// Create session
	session, err := NewSession("user123", 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	if err := store.Create(ctx, session, 1*time.Hour); err != nil {
		t.Fatalf("failed to store session: %v", err)
	}

	// Update session data
	session.Data["key"] = "value"
	if err := store.Update(ctx, session, 2*time.Hour); err != nil {
		t.Fatalf("failed to update session: %v", err)
	}

	// Get updated session
	retrieved, err := store.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}

	if retrieved.Data["key"] != "value" {
		t.Errorf("expected data key=value, got %v", retrieved.Data["key"])
	}
}

func TestInMemorySessionStore_Delete(t *testing.T) {
	ctx := context.Background()
	log := logger.NewTestLogger()
	met := metrics.NewNoOpMetrics()

	store := NewInMemorySessionStore(log, met)

	// Create session
	session, err := NewSession("user123", 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	if err := store.Create(ctx, session, 1*time.Hour); err != nil {
		t.Fatalf("failed to store session: %v", err)
	}

	// Delete session
	if err := store.Delete(ctx, session.ID); err != nil {
		t.Fatalf("failed to delete session: %v", err)
	}

	// Verify deletion
	_, err = store.Get(ctx, session.ID)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound after delete, got %v", err)
	}
}

func TestInMemorySessionStore_DeleteByUserID(t *testing.T) {
	ctx := context.Background()
	log := logger.NewTestLogger()
	met := metrics.NewNoOpMetrics()

	store := NewInMemorySessionStore(log, met)

	// Create multiple sessions for the same user
	userID := "user123"
	sessions := make([]*Session, 3)

	for i := range 3 {
		session, err := NewSession(userID, 1*time.Hour)
		if err != nil {
			t.Fatalf("failed to create session %d: %v", i, err)
		}

		sessions[i] = session

		if err := store.Create(ctx, session, 1*time.Hour); err != nil {
			t.Fatalf("failed to store session %d: %v", i, err)
		}
	}

	// Create session for different user
	otherSession, err := NewSession("user456", 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to create other session: %v", err)
	}

	if err := store.Create(ctx, otherSession, 1*time.Hour); err != nil {
		t.Fatalf("failed to store other session: %v", err)
	}

	// Delete all sessions for user123
	if err := store.DeleteByUserID(ctx, userID); err != nil {
		t.Fatalf("failed to delete sessions by user ID: %v", err)
	}

	// Verify user123 sessions are deleted
	for i, session := range sessions {
		_, err := store.Get(ctx, session.ID)
		if !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("expected session %d to be deleted, got error %v", i, err)
		}
	}

	// Verify other user's session still exists
	_, err = store.Get(ctx, otherSession.ID)
	if err != nil {
		t.Errorf("other user's session should not be deleted, got error %v", err)
	}
}

func TestInMemorySessionStore_Touch(t *testing.T) {
	ctx := context.Background()
	log := logger.NewTestLogger()
	met := metrics.NewNoOpMetrics()

	store := NewInMemorySessionStore(log, met)

	// Create session
	session, err := NewSession("user123", 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	if err := store.Create(ctx, session, 1*time.Hour); err != nil {
		t.Fatalf("failed to store session: %v", err)
	}

	oldExpiry := session.ExpiresAt
	oldLastAccessed := session.LastAccessedAt

	time.Sleep(10 * time.Millisecond)

	// Touch session
	if err := store.Touch(ctx, session.ID, 2*time.Hour); err != nil {
		t.Fatalf("failed to touch session: %v", err)
	}

	// Get session and verify expiry was extended and last accessed was updated
	retrieved, err := store.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}

	if !retrieved.ExpiresAt.After(oldExpiry) {
		t.Error("Touch() did not extend session expiry")
	}

	if !retrieved.LastAccessedAt.After(oldLastAccessed) {
		t.Error("Touch() did not update last accessed time")
	}
}

func TestInMemorySessionStore_Cleanup(t *testing.T) {
	ctx := context.Background()
	log := logger.NewTestLogger()
	met := metrics.NewNoOpMetrics()

	store := NewInMemorySessionStore(log, met)

	// Create expired sessions
	for i := range 3 {
		session, err := NewSession("user"+string(rune(i)), 1*time.Millisecond)
		if err != nil {
			t.Fatalf("failed to create session %d: %v", i, err)
		}

		if err := store.Create(ctx, session, 1*time.Millisecond); err != nil {
			t.Fatalf("failed to store session %d: %v", i, err)
		}
	}

	// Create valid session
	validSession, err := NewSession("valid", 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to create valid session: %v", err)
	}

	if err := store.Create(ctx, validSession, 1*time.Hour); err != nil {
		t.Fatalf("failed to store valid session: %v", err)
	}

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	// Cleanup
	deleted, err := store.Cleanup(ctx)
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	if deleted != 3 {
		t.Errorf("expected 3 sessions deleted, got %d", deleted)
	}

	// Verify valid session still exists
	_, err = store.Get(ctx, validSession.ID)
	if err != nil {
		t.Errorf("valid session should not be deleted, got error %v", err)
	}
}

func TestInMemorySessionStore_Count(t *testing.T) {
	ctx := context.Background()
	log := logger.NewTestLogger()
	met := metrics.NewNoOpMetrics()

	store := NewInMemorySessionStore(log, met)

	// Initial count should be 0
	count, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("failed to count sessions: %v", err)
	}

	if count != 0 {
		t.Errorf("expected 0 sessions, got %d", count)
	}

	// Create sessions
	for i := range 5 {
		session, err := NewSession("user"+string(rune(i)), 1*time.Hour)
		if err != nil {
			t.Fatalf("failed to create session %d: %v", i, err)
		}

		if err := store.Create(ctx, session, 1*time.Hour); err != nil {
			t.Fatalf("failed to store session %d: %v", i, err)
		}
	}

	// Count should be 5
	count, err = store.Count(ctx)
	if err != nil {
		t.Fatalf("failed to count sessions: %v", err)
	}

	if count != 5 {
		t.Errorf("expected 5 sessions, got %d", count)
	}
}
