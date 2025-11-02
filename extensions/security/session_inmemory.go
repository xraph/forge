package security

import (
	"context"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// InMemorySessionStore implements SessionStore using an in-memory map
// with automatic cleanup of expired sessions. Not recommended for production
// use in distributed environments.
type InMemorySessionStore struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	logger   forge.Logger
	metrics  forge.Metrics
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewInMemorySessionStore creates a new in-memory session store
func NewInMemorySessionStore(logger forge.Logger, metrics forge.Metrics) *InMemorySessionStore {
	return &InMemorySessionStore{
		sessions: make(map[string]*Session),
		logger:   logger,
		metrics:  metrics,
		stopCh:   make(chan struct{}),
	}
}

// Create creates a new session
func (s *InMemorySessionStore) Create(ctx context.Context, session *Session, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session.ExpiresAt = time.Now().Add(ttl)
	s.sessions[session.ID] = session

	if s.metrics != nil {
		s.metrics.Counter("security.sessions.created").Inc()
		s.metrics.Gauge("security.sessions.active").Set(float64(len(s.sessions)))
	}

	s.logger.Debug("session created",
		forge.F("session_id", session.ID),
		forge.F("user_id", session.UserID),
		forge.F("expires_at", session.ExpiresAt),
	)

	return nil
}

// Get retrieves a session by ID
func (s *InMemorySessionStore) Get(ctx context.Context, sessionID string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		if s.metrics != nil {
			s.metrics.Counter("security.sessions.not_found").Inc()
		}
		return nil, ErrSessionNotFound
	}

	if session.IsExpired() {
		if s.metrics != nil {
			s.metrics.Counter("security.sessions.expired").Inc()
		}
		return nil, ErrSessionExpired
	}

	if s.metrics != nil {
		s.metrics.Counter("security.sessions.retrieved").Inc()
	}

	return session, nil
}

// Update updates an existing session
func (s *InMemorySessionStore) Update(ctx context.Context, session *Session, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[session.ID]; !exists {
		return ErrSessionNotFound
	}

	session.ExpiresAt = time.Now().Add(ttl)
	s.sessions[session.ID] = session

	if s.metrics != nil {
		s.metrics.Counter("security.sessions.updated").Inc()
	}

	s.logger.Debug("session updated",
		forge.F("session_id", session.ID),
		forge.F("user_id", session.UserID),
	)

	return nil
}

// Delete removes a session by ID
func (s *InMemorySessionStore) Delete(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[sessionID]; !exists {
		return ErrSessionNotFound
	}

	delete(s.sessions, sessionID)

	if s.metrics != nil {
		s.metrics.Counter("security.sessions.deleted").Inc()
		s.metrics.Gauge("security.sessions.active").Set(float64(len(s.sessions)))
	}

	s.logger.Debug("session deleted",
		forge.F("session_id", sessionID),
	)

	return nil
}

// DeleteByUserID removes all sessions for a user
func (s *InMemorySessionStore) DeleteByUserID(ctx context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	deleted := 0
	for id, session := range s.sessions {
		if session.UserID == userID {
			delete(s.sessions, id)
			deleted++
		}
	}

	if s.metrics != nil {
		s.metrics.Counter("security.sessions.deleted").Add(float64(deleted))
		s.metrics.Gauge("security.sessions.active").Set(float64(len(s.sessions)))
	}

	s.logger.Debug("sessions deleted by user",
		forge.F("user_id", userID),
		forge.F("count", deleted),
	)

	return nil
}

// Touch updates the last accessed time and extends TTL
func (s *InMemorySessionStore) Touch(ctx context.Context, sessionID string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return ErrSessionNotFound
	}

	if session.IsExpired() {
		return ErrSessionExpired
	}

	session.Touch()
	session.ExpiresAt = time.Now().Add(ttl)
	s.sessions[sessionID] = session

	if s.metrics != nil {
		s.metrics.Counter("security.sessions.touched").Inc()
	}

	return nil
}

// Cleanup removes expired sessions
func (s *InMemorySessionStore) Cleanup(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	deleted := 0

	for id, session := range s.sessions {
		if now.After(session.ExpiresAt) {
			delete(s.sessions, id)
			deleted++
		}
	}

	if deleted > 0 {
		if s.metrics != nil {
			s.metrics.Counter("security.sessions.cleaned_up").Add(float64(deleted))
			s.metrics.Gauge("security.sessions.active").Set(float64(len(s.sessions)))
		}

		s.logger.Debug("cleaned up expired sessions",
			forge.F("count", deleted),
		)
	}

	return deleted, nil
}

// Count returns the total number of active sessions
func (s *InMemorySessionStore) Count(ctx context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return int64(len(s.sessions)), nil
}

// Connect establishes connection (no-op for in-memory)
func (s *InMemorySessionStore) Connect(ctx context.Context) error {
	// Start background cleanup goroutine
	s.wg.Add(1)
	go s.cleanupLoop()
	return nil
}

// Disconnect closes connection (no-op for in-memory)
func (s *InMemorySessionStore) Disconnect(ctx context.Context) error {
	close(s.stopCh)
	s.wg.Wait()
	return nil
}

// Ping checks if the store is accessible (always true for in-memory)
func (s *InMemorySessionStore) Ping(ctx context.Context) error {
	return nil
}

// cleanupLoop runs periodic cleanup of expired sessions
func (s *InMemorySessionStore) cleanupLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if _, err := s.Cleanup(context.Background()); err != nil {
				s.logger.Error("failed to cleanup expired sessions",
					forge.F("error", err),
				)
			}
		case <-s.stopCh:
			return
		}
	}
}

