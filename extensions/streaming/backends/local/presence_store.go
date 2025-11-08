package local

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// PresenceStore implements streaming.PresenceStore with in-memory storage.
type PresenceStore struct {
	mu       sync.RWMutex
	presence map[string]*streaming.UserPresence
	online   map[string]bool
	history  map[string][]*streaming.PresenceEvent      // userID -> events
	devices  map[string]map[string]streaming.DeviceInfo // userID -> deviceID -> device
}

// NewPresenceStore creates a new local presence store.
func NewPresenceStore() streaming.PresenceStore {
	return &PresenceStore{
		presence: make(map[string]*streaming.UserPresence),
		online:   make(map[string]bool),
		history:  make(map[string][]*streaming.PresenceEvent),
		devices:  make(map[string]map[string]streaming.DeviceInfo),
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

func (s *PresenceStore) SetMultiple(ctx context.Context, presences map[string]*streaming.UserPresence) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for userID, presence := range presences {
		if presence == nil {
			continue
		}

		// Create a copy to avoid shared state issues
		presenceCopy := &streaming.UserPresence{
			UserID:       presence.UserID,
			Status:       presence.Status,
			LastSeen:     presence.LastSeen,
			Connections:  append([]string(nil), presence.Connections...),
			CustomStatus: presence.CustomStatus,
			Metadata:     copyMap(presence.Metadata),
		}

		s.presence[userID] = presenceCopy

		// Save history event
		event := &streaming.PresenceEvent{
			Type:      "status_change",
			UserID:    userID,
			Status:    presence.Status,
			Timestamp: presence.LastSeen,
			Metadata:  copyMap(presence.Metadata),
		}
		s.history[userID] = append(s.history[userID], event)
	}

	return nil
}

func (s *PresenceStore) DeleteMultiple(ctx context.Context, userIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, userID := range userIDs {
		delete(s.presence, userID)
		delete(s.online, userID)

		// Save history event
		event := &streaming.PresenceEvent{
			Type:      "offline",
			UserID:    userID,
			Status:    streaming.StatusOffline,
			Timestamp: time.Now(),
		}
		s.history[userID] = append(s.history[userID], event)
	}

	return nil
}

func (s *PresenceStore) GetByStatus(ctx context.Context, status string) ([]*streaming.UserPresence, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*streaming.UserPresence

	for _, presence := range s.presence {
		if presence.Status == status {
			// Create a copy to avoid shared state issues
			presenceCopy := &streaming.UserPresence{
				UserID:       presence.UserID,
				Status:       presence.Status,
				LastSeen:     presence.LastSeen,
				Connections:  append([]string(nil), presence.Connections...),
				CustomStatus: presence.CustomStatus,
				Metadata:     copyMap(presence.Metadata),
			}
			result = append(result, presenceCopy)
		}
	}

	return result, nil
}

func (s *PresenceStore) GetRecent(ctx context.Context, status string, since time.Duration) ([]*streaming.UserPresence, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cutoff := time.Now().Add(-since)

	var result []*streaming.UserPresence

	for _, presence := range s.presence {
		if (status == "" || presence.Status == status) && presence.LastSeen.After(cutoff) {
			// Create a copy to avoid shared state issues
			presenceCopy := &streaming.UserPresence{
				UserID:       presence.UserID,
				Status:       presence.Status,
				LastSeen:     presence.LastSeen,
				Connections:  append([]string(nil), presence.Connections...),
				CustomStatus: presence.CustomStatus,
				Metadata:     copyMap(presence.Metadata),
			}
			result = append(result, presenceCopy)
		}
	}

	return result, nil
}

func (s *PresenceStore) GetWithFilters(ctx context.Context, filters streaming.PresenceFilters) ([]*streaming.UserPresence, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*streaming.UserPresence

	for _, presence := range s.presence {
		// Check status filter
		if len(filters.Status) > 0 {
			statusMatch := slices.Contains(filters.Status, presence.Status)

			if !statusMatch {
				continue
			}
		}

		// Check online filter
		if filters.Online {
			if _, isOnline := s.online[presence.UserID]; !isOnline {
				continue
			}
		}

		// Check activity filter
		if !filters.SinceActivity.IsZero() && presence.LastSeen.Before(filters.SinceActivity) {
			continue
		}

		// Create a copy to avoid shared state issues
		presenceCopy := &streaming.UserPresence{
			UserID:       presence.UserID,
			Status:       presence.Status,
			LastSeen:     presence.LastSeen,
			Connections:  append([]string(nil), presence.Connections...),
			CustomStatus: presence.CustomStatus,
			Metadata:     copyMap(presence.Metadata),
		}
		result = append(result, presenceCopy)
	}

	return result, nil
}

func (s *PresenceStore) SaveHistory(ctx context.Context, userID string, event *streaming.PresenceEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if event == nil {
		return streaming.ErrInvalidMessage
	}

	// Create a copy to avoid shared state issues
	eventCopy := &streaming.PresenceEvent{
		Type:      event.Type,
		UserID:    event.UserID,
		Status:    event.Status,
		Timestamp: event.Timestamp,
		Metadata:  copyMap(event.Metadata),
	}

	s.history[userID] = append(s.history[userID], eventCopy)

	return nil
}

func (s *PresenceStore) GetHistory(ctx context.Context, userID string, limit int) ([]*streaming.PresenceEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events, exists := s.history[userID]
	if !exists {
		return []*streaming.PresenceEvent{}, nil
	}

	// Sort by timestamp (most recent first)
	sortedEvents := make([]*streaming.PresenceEvent, len(events))
	copy(sortedEvents, events)

	// Simple sort by timestamp descending
	for i := range len(sortedEvents) - 1 {
		for j := i + 1; j < len(sortedEvents); j++ {
			if sortedEvents[i].Timestamp.Before(sortedEvents[j].Timestamp) {
				sortedEvents[i], sortedEvents[j] = sortedEvents[j], sortedEvents[i]
			}
		}
	}

	// Apply limit
	if limit > 0 && limit < len(sortedEvents) {
		sortedEvents = sortedEvents[:limit]
	}

	// Create copies to avoid shared state issues
	result := make([]*streaming.PresenceEvent, len(sortedEvents))
	for i, event := range sortedEvents {
		result[i] = &streaming.PresenceEvent{
			Type:      event.Type,
			UserID:    event.UserID,
			Status:    event.Status,
			Timestamp: event.Timestamp,
			Metadata:  copyMap(event.Metadata),
		}
	}

	return result, nil
}

func (s *PresenceStore) GetHistorySince(ctx context.Context, userID string, since time.Time) ([]*streaming.PresenceEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events, exists := s.history[userID]
	if !exists {
		return []*streaming.PresenceEvent{}, nil
	}

	var result []*streaming.PresenceEvent

	for _, event := range events {
		if event.Timestamp.After(since) {
			// Create a copy to avoid shared state issues
			eventCopy := &streaming.PresenceEvent{
				Type:      event.Type,
				UserID:    event.UserID,
				Status:    event.Status,
				Timestamp: event.Timestamp,
				Metadata:  copyMap(event.Metadata),
			}
			result = append(result, eventCopy)
		}
	}

	// Sort by timestamp (most recent first)
	for i := range len(result) - 1 {
		for j := i + 1; j < len(result); j++ {
			if result[i].Timestamp.Before(result[j].Timestamp) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result, nil
}

func (s *PresenceStore) SetDevice(ctx context.Context, userID, deviceID string, device streaming.DeviceInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.devices[userID] == nil {
		s.devices[userID] = make(map[string]streaming.DeviceInfo)
	}

	// Create a copy to avoid shared state issues
	deviceCopy := streaming.DeviceInfo{
		DeviceID: device.DeviceID,
		Type:     device.Type,
		OS:       device.OS,
		Browser:  device.Browser,
		LastSeen: device.LastSeen,
		IP:       device.IP,
		Active:   device.Active,
		Metadata: copyMap(device.Metadata),
	}

	s.devices[userID][deviceID] = deviceCopy

	return nil
}

func (s *PresenceStore) GetDevices(ctx context.Context, userID string) ([]streaming.DeviceInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	devices := s.devices[userID]
	if devices == nil {
		return []streaming.DeviceInfo{}, nil
	}

	// Create copies to avoid shared state issues
	result := make([]streaming.DeviceInfo, 0, len(devices))
	for _, device := range devices {
		result = append(result, streaming.DeviceInfo{
			DeviceID: device.DeviceID,
			Type:     device.Type,
			OS:       device.OS,
			Browser:  device.Browser,
			LastSeen: device.LastSeen,
			IP:       device.IP,
			Active:   device.Active,
			Metadata: copyMap(device.Metadata),
		})
	}

	return result, nil
}

func (s *PresenceStore) RemoveDevice(ctx context.Context, userID, deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.devices[userID] == nil {
		return nil // Device doesn't exist, nothing to remove
	}

	delete(s.devices[userID], deviceID)

	// Clean up empty map
	if len(s.devices[userID]) == 0 {
		delete(s.devices, userID)
	}

	return nil
}

func (s *PresenceStore) CountByStatus(ctx context.Context) (map[string]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	counts := make(map[string]int)
	for _, presence := range s.presence {
		counts[presence.Status]++
	}

	return counts, nil
}

func (s *PresenceStore) GetActiveCount(ctx context.Context, since time.Duration) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cutoff := time.Now().Add(-since)
	count := 0

	for _, presence := range s.presence {
		if presence.LastSeen.After(cutoff) {
			count++
		}
	}

	return count, nil
}

func copyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}

	copy := make(map[string]any, len(m))
	maps.Copy(copy, m)

	return copy
}
