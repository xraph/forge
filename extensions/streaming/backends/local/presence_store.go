package local

import (
	"context"
	"sync"
	"time"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// PresenceStore implements streaming.PresenceStore with in-memory storage.
type PresenceStore struct {
	mu       sync.RWMutex
	presence map[string]*streaming.UserPresence
	online   map[string]bool
}

// NewPresenceStore creates a new local presence store.
func NewPresenceStore() streaming.PresenceStore {
	return &PresenceStore{
		presence: make(map[string]*streaming.UserPresence),
		online:   make(map[string]bool),
	}
}

func (s *PresenceStore) Set(ctx context.Context, userID string, presence *streaming.UserPresence) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.presence[userID] = presence
	if presence.Status == streaming.StatusOnline {
		s.online[userID] = true
	} else {
		delete(s.online, userID)
	}

	return nil
}

func (s *PresenceStore) Get(ctx context.Context, userID string) (*streaming.UserPresence, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	presence, exists := s.presence[userID]
	if !exists {
		return nil, streaming.ErrPresenceNotFound
	}

	// Return copy
	return &streaming.UserPresence{
		UserID:       presence.UserID,
		Status:       presence.Status,
		LastSeen:     presence.LastSeen,
		Connections:  append([]string{}, presence.Connections...),
		CustomStatus: presence.CustomStatus,
		Metadata:     copyMap(presence.Metadata),
	}, nil
}

func (s *PresenceStore) GetMultiple(ctx context.Context, userIDs []string) ([]*streaming.UserPresence, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	presences := make([]*streaming.UserPresence, 0, len(userIDs))
	for _, userID := range userIDs {
		if presence, exists := s.presence[userID]; exists {
			presences = append(presences, &streaming.UserPresence{
				UserID:       presence.UserID,
				Status:       presence.Status,
				LastSeen:     presence.LastSeen,
				Connections:  append([]string{}, presence.Connections...),
				CustomStatus: presence.CustomStatus,
				Metadata:     copyMap(presence.Metadata),
			})
		}
	}

	return presences, nil
}

func (s *PresenceStore) Delete(ctx context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.presence, userID)
	delete(s.online, userID)

	return nil
}

func (s *PresenceStore) GetOnline(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	users := make([]string, 0, len(s.online))
	for userID := range s.online {
		users = append(users, userID)
	}

	return users, nil
}

func (s *PresenceStore) SetOnline(ctx context.Context, userID string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.online[userID] = true

	// Update or create presence
	presence, exists := s.presence[userID]
	if !exists {
		presence = &streaming.UserPresence{
			UserID:      userID,
			Status:      streaming.StatusOnline,
			LastSeen:    time.Now(),
			Connections: []string{},
			Metadata:    make(map[string]any),
		}
		s.presence[userID] = presence
	} else {
		presence.Status = streaming.StatusOnline
		presence.LastSeen = time.Now()
	}

	return nil
}

func (s *PresenceStore) SetOffline(ctx context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.online, userID)

	if presence, exists := s.presence[userID]; exists {
		presence.Status = streaming.StatusOffline
		presence.LastSeen = time.Now()
	}

	return nil
}

func (s *PresenceStore) IsOnline(ctx context.Context, userID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.online[userID], nil
}

func (s *PresenceStore) UpdateActivity(ctx context.Context, userID string, timestamp time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if presence, exists := s.presence[userID]; exists {
		presence.LastSeen = timestamp
	}

	return nil
}

func (s *PresenceStore) GetLastActivity(ctx context.Context, userID string) (time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if presence, exists := s.presence[userID]; exists {
		return presence.LastSeen, nil
	}

	return time.Time{}, streaming.ErrPresenceNotFound
}

func (s *PresenceStore) CleanupExpired(ctx context.Context, olderThan time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	toDelete := make([]string, 0)

	for userID, presence := range s.presence {
		if presence.LastSeen.Before(cutoff) {
			toDelete = append(toDelete, userID)
		}
	}

	for _, userID := range toDelete {
		delete(s.presence, userID)
		delete(s.online, userID)
	}

	return nil
}

func (s *PresenceStore) Connect(ctx context.Context) error {
	return nil // No-op for local
}

func (s *PresenceStore) Disconnect(ctx context.Context) error {
	return nil // No-op for local
}

func (s *PresenceStore) Ping(ctx context.Context) error {
	return nil // No-op for local
}

func copyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	copy := make(map[string]any, len(m))
	for k, v := range m {
		copy[k] = v
	}
	return copy
}
