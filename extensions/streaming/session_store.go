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

	// LastEventIDs is the position each channel had reached when the session
	// dropped, so a resumption can ask for the gap instead of resynchronising.
	LastEventIDs map[string]string `json:"last_event_ids,omitempty"`
}

// SessionStore stores session snapshots for resumption.
type SessionStore interface {
	// Save stores a session snapshot with a TTL.
	Save(ctx context.Context, snapshot *SessionSnapshot, ttl time.Duration) error

	// Get retrieves a session snapshot by session ID.
	Get(ctx context.Context, sessionID string) (*SessionSnapshot, error)

	// Delete removes a session snapshot.
	Delete(ctx context.Context, sessionID string) error

	// Close releases any background resources held by the store. It must be
	// safe to call more than once.
	Close() error
}

// clone returns a deep copy of the snapshot. Callers get their own Rooms,
// Channels, Metadata and LastEventIDs so two concurrent resumptions of the
// same session cannot observe or corrupt each other's state.
func (s *SessionSnapshot) clone() *SessionSnapshot {
	if s == nil {
		return nil
	}

	cp := *s

	if s.Rooms != nil {
		cp.Rooms = make([]string, len(s.Rooms))
		copy(cp.Rooms, s.Rooms)
	}

	if s.Channels != nil {
		cp.Channels = make([]string, len(s.Channels))
		copy(cp.Channels, s.Channels)
	}

	if s.Metadata != nil {
		cp.Metadata = make(map[string]string, len(s.Metadata))
		for k, v := range s.Metadata {
			cp.Metadata[k] = v
		}
	}

	if s.LastEventIDs != nil {
		cp.LastEventIDs = make(map[string]string, len(s.LastEventIDs))
		for k, v := range s.LastEventIDs {
			cp.LastEventIDs[k] = v
		}
	}

	return &cp
}

// inMemorySessionStore implements SessionStore with in-memory storage.
type inMemorySessionStore struct {
	sessions map[string]*sessionWithExpiry
	mu       sync.RWMutex

	stop      chan struct{}
	closeOnce sync.Once
}

type sessionWithExpiry struct {
	snapshot  *SessionSnapshot
	expiresAt time.Time
}

// NewInMemorySessionStore creates an in-memory session store.
func NewInMemorySessionStore() SessionStore {
	store := &inMemorySessionStore{
		sessions: make(map[string]*sessionWithExpiry),
		stop:     make(chan struct{}),
	}
	go store.cleanupLoop()
	return store
}

func (s *inMemorySessionStore) Save(ctx context.Context, snapshot *SessionSnapshot, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Copy on the way in as well as on the way out: a caller that keeps hold of
	// the snapshot it saved must not retain a handle into the store.
	s.sessions[snapshot.SessionID] = &sessionWithExpiry{
		snapshot:  snapshot.clone(),
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

	// Hand back a copy: returning the stored pointer lets one resuming
	// connection mutate the snapshot another one is still reading.
	return entry.snapshot.clone(), nil
}

func (s *inMemorySessionStore) Delete(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, sessionID)
	return nil
}

// Close stops the background expiry sweep. It is safe to call concurrently and
// more than once; calls after the first are no-ops. Stored sessions remain
// readable after Close, they simply stop being reaped.
func (s *inMemorySessionStore) Close() error {
	s.closeOnce.Do(func() {
		close(s.stop)
	})

	return nil
}

func (s *inMemorySessionStore) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.sweepExpired()
		}
	}
}

// sweepExpired drops every session past its expiry.
func (s *inMemorySessionStore) sweepExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, entry := range s.sessions {
		if now.After(entry.expiresAt) {
			delete(s.sessions, id)
		}
	}
}
