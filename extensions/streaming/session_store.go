package streaming

import (
	"context"
	"sync"
	"time"
)

// SessionSnapshot captures the state of a connection for resumption.
type SessionSnapshot struct {
	SessionID      string            `json:"session_id"`
	UserID         string            `json:"user_id"`
	Rooms          []string          `json:"rooms"`
	Channels       []string          `json:"channels"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	DisconnectedAt time.Time         `json:"disconnected_at"`
}

// SessionStore stores session snapshots for resumption.
type SessionStore interface {
	// Save stores a session snapshot with a TTL.
	Save(ctx context.Context, snapshot *SessionSnapshot, ttl time.Duration) error

	// Get retrieves a session snapshot by session ID.
	Get(ctx context.Context, sessionID string) (*SessionSnapshot, error)

	// Delete removes a session snapshot.
	Delete(ctx context.Context, sessionID string) error
}

// inMemorySessionStore implements SessionStore with in-memory storage.
type inMemorySessionStore struct {
	sessions map[string]*sessionWithExpiry
	mu       sync.RWMutex
}

type sessionWithExpiry struct {
	snapshot  *SessionSnapshot
	expiresAt time.Time
}

// NewInMemorySessionStore creates an in-memory session store.
func NewInMemorySessionStore() SessionStore {
	store := &inMemorySessionStore{
		sessions: make(map[string]*sessionWithExpiry),
	}
	go store.cleanupLoop()
	return store
}

func (s *inMemorySessionStore) Save(ctx context.Context, snapshot *SessionSnapshot, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[snapshot.SessionID] = &sessionWithExpiry{
		snapshot:  snapshot,
		expiresAt: time.Now().Add(ttl),
	}
	return nil
}

func (s *inMemorySessionStore) Get(ctx context.Context, sessionID string) (*SessionSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.sessions[sessionID]
	if !ok {
		return nil, ErrConnectionNotFound
	}

	if time.Now().After(entry.expiresAt) {
		return nil, ErrConnectionNotFound
	}

	return entry.snapshot, nil
}

func (s *inMemorySessionStore) Delete(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, sessionID)
	return nil
}

func (s *inMemorySessionStore) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for id, entry := range s.sessions {
			if now.After(entry.expiresAt) {
				delete(s.sessions, id)
			}
		}
		s.mu.Unlock()
	}
}
