package redis

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
	"github.com/xraph/forge/internal/errors"
)

// PresenceStore implements streaming.PresenceStore with Redis backend.
type PresenceStore struct {
	client *redis.Client
	prefix string
}

// NewPresenceStore creates a new Redis presence store.
func NewPresenceStore(client *redis.Client, prefix string) streaming.PresenceStore {
	if prefix == "" {
		prefix = "streaming:presence"
	}

	return &PresenceStore{
		client: client,
		prefix: prefix,
	}
}

func (s *PresenceStore) Set(ctx context.Context, userID string, presence *streaming.UserPresence) error {
	key := s.prefix + ":" + userID

	dataJSON, err := json.Marshal(presence)
	if err != nil {
		return err
	}

	return s.client.Set(ctx, key, dataJSON, 0).Err()
}

func (s *PresenceStore) Get(ctx context.Context, userID string) (*streaming.UserPresence, error) {
	key := s.prefix + ":" + userID

	data, err := s.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return nil, streaming.ErrPresenceNotFound
	}

	if err != nil {
		return nil, err
	}

	var presence streaming.UserPresence
	if err := json.Unmarshal([]byte(data), &presence); err != nil {
		return nil, err
	}

	return &presence, nil
}

func (s *PresenceStore) GetMultiple(ctx context.Context, userIDs []string) ([]*streaming.UserPresence, error) {
	if len(userIDs) == 0 {
		return []*streaming.UserPresence{}, nil
	}

	// Build keys
	keys := make([]string, len(userIDs))
	for i, userID := range userIDs {
		keys[i] = s.prefix + ":" + userID
	}

	// Batch get
	results, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}

	presences := make([]*streaming.UserPresence, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}

		dataStr, ok := result.(string)
		if !ok {
			continue
		}

		var presence streaming.UserPresence
		if err := json.Unmarshal([]byte(dataStr), &presence); err != nil {
			continue
		}

		presences = append(presences, &presence)
	}

	return presences, nil
}

func (s *PresenceStore) Delete(ctx context.Context, userID string) error {
	key := s.prefix + ":" + userID
	onlineKey := s.prefix + ":online"

	pipe := s.client.Pipeline()
	pipe.Del(ctx, key)
	pipe.ZRem(ctx, onlineKey, userID)
	_, err := pipe.Exec(ctx)

	return err
}

func (s *PresenceStore) GetOnline(ctx context.Context) ([]string, error) {
	onlineKey := s.prefix + ":online"

	// Get all online users (sorted set by last activity)
	users, err := s.client.ZRange(ctx, onlineKey, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	return users, nil
}

func (s *PresenceStore) SetOnline(ctx context.Context, userID string, ttl time.Duration) error {
	onlineKey := s.prefix + ":online"

	// Add to online set with score as expiration time
	expiresAt := time.Now().Add(ttl).Unix()

	return s.client.ZAdd(ctx, onlineKey, redis.Z{
		Score:  float64(expiresAt),
		Member: userID,
	}).Err()
}

func (s *PresenceStore) SetOffline(ctx context.Context, userID string) error {
	onlineKey := s.prefix + ":online"

	return s.client.ZRem(ctx, onlineKey, userID).Err()
}

func (s *PresenceStore) IsOnline(ctx context.Context, userID string) (bool, error) {
	onlineKey := s.prefix + ":online"

	score, err := s.client.ZScore(ctx, onlineKey, userID).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	// Check if not expired
	return time.Now().Unix() < int64(score), nil
}

func (s *PresenceStore) UpdateActivity(ctx context.Context, userID string, timestamp time.Time) error {
	presence, err := s.Get(ctx, userID)
	if err != nil {
		return err
	}

	presence.LastSeen = timestamp

	return s.Set(ctx, userID, presence)
}

func (s *PresenceStore) GetLastActivity(ctx context.Context, userID string) (time.Time, error) {
	presence, err := s.Get(ctx, userID)
	if err != nil {
		return time.Time{}, err
	}

	return presence.LastSeen, nil
}

func (s *PresenceStore) CleanupExpired(ctx context.Context, olderThan time.Duration) error {
	onlineKey := s.prefix + ":online"

	// Remove expired users from online set
	now := time.Now().Unix()

	return s.client.ZRemRangeByScore(ctx, onlineKey, "-inf", string(rune(now))).Err()
}

func (s *PresenceStore) Connect(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *PresenceStore) Disconnect(ctx context.Context) error {
	return nil
}

func (s *PresenceStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

// SetMultiple sets multiple presence records in a single operation.
func (s *PresenceStore) SetMultiple(ctx context.Context, presences map[string]*streaming.UserPresence) error {
	if len(presences) == 0 {
		return nil
	}

	pipe := s.client.Pipeline()
	for userID, presence := range presences {
		key := s.prefix + ":" + userID

		dataJSON, err := json.Marshal(presence)
		if err != nil {
			return err
		}

		pipe.Set(ctx, key, dataJSON, 0)
	}

	_, err := pipe.Exec(ctx)

	return err
}

// DeleteMultiple deletes multiple presence records in a single operation.
func (s *PresenceStore) DeleteMultiple(ctx context.Context, userIDs []string) error {
	if len(userIDs) == 0 {
		return nil
	}

	keys := make([]string, len(userIDs))
	for i, userID := range userIDs {
		keys[i] = s.prefix + ":" + userID
	}

	pipe := s.client.Pipeline()
	pipe.Del(ctx, keys...)

	// Also remove from online set
	onlineKey := s.prefix + ":online"
	for _, userID := range userIDs {
		pipe.ZRem(ctx, onlineKey, userID)
	}

	_, err := pipe.Exec(ctx)

	return err
}

// GetByStatus returns all presence records with the specified status.
func (s *PresenceStore) GetByStatus(ctx context.Context, status string) ([]*streaming.UserPresence, error) {
	// Get all presence keys
	pattern := s.prefix + ":*"

	keys, err := s.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, err
	}

	if len(keys) == 0 {
		return []*streaming.UserPresence{}, nil
	}

	// Get all presence records
	results, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}

	var presences []*streaming.UserPresence

	for _, result := range results {
		if result == nil {
			continue
		}

		dataStr, ok := result.(string)
		if !ok {
			continue
		}

		var presence streaming.UserPresence
		if err := json.Unmarshal([]byte(dataStr), &presence); err != nil {
			continue
		}

		if presence.Status == status {
			presences = append(presences, &presence)
		}
	}

	return presences, nil
}

// GetRecent returns presence records with the specified status updated since the given duration.
func (s *PresenceStore) GetRecent(ctx context.Context, status string, since time.Duration) ([]*streaming.UserPresence, error) {
	cutoff := time.Now().Add(-since)

	// Get all presence keys
	pattern := s.prefix + ":*"

	keys, err := s.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, err
	}

	if len(keys) == 0 {
		return []*streaming.UserPresence{}, nil
	}

	// Get all presence records
	results, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}

	var presences []*streaming.UserPresence

	for _, result := range results {
		if result == nil {
			continue
		}

		dataStr, ok := result.(string)
		if !ok {
			continue
		}

		var presence streaming.UserPresence
		if err := json.Unmarshal([]byte(dataStr), &presence); err != nil {
			continue
		}

		if presence.Status == status && presence.LastSeen.After(cutoff) {
			presences = append(presences, &presence)
		}
	}

	return presences, nil
}

// GetWithFilters returns presence records matching the specified filters.
func (s *PresenceStore) GetWithFilters(ctx context.Context, filters streaming.PresenceFilters) ([]*streaming.UserPresence, error) {
	// Get all presence keys
	pattern := s.prefix + ":*"

	keys, err := s.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, err
	}

	if len(keys) == 0 {
		return []*streaming.UserPresence{}, nil
	}

	// Get all presence records
	results, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}

	var presences []*streaming.UserPresence

	for _, result := range results {
		if result == nil {
			continue
		}

		dataStr, ok := result.(string)
		if !ok {
			continue
		}

		var presence streaming.UserPresence
		if err := json.Unmarshal([]byte(dataStr), &presence); err != nil {
			continue
		}

		// Apply filters
		if len(filters.Status) > 0 {
			found := false

			for _, status := range filters.Status {
				if presence.Status == status {
					found = true

					break
				}
			}

			if !found {
				continue
			}
		}

		if !filters.SinceActivity.IsZero() && presence.LastSeen.Before(filters.SinceActivity) {
			continue
		}

		presences = append(presences, &presence)
	}

	return presences, nil
}

// SaveHistory saves a presence event to history.
func (s *PresenceStore) SaveHistory(ctx context.Context, userID string, event *streaming.PresenceEvent) error {
	historyKey := s.prefix + ":history:" + userID

	eventJSON, err := json.Marshal(event)
	if err != nil {
		return err
	}

	// Add to sorted set with timestamp as score
	score := float64(event.Timestamp.Unix())

	return s.client.ZAdd(ctx, historyKey, redis.Z{
		Score:  score,
		Member: string(eventJSON),
	}).Err()
}

// GetHistory returns presence history for a user.
func (s *PresenceStore) GetHistory(ctx context.Context, userID string, limit int) ([]*streaming.PresenceEvent, error) {
	historyKey := s.prefix + ":history:" + userID

	// Get latest events (highest scores first)
	results, err := s.client.ZRevRange(ctx, historyKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}

	events := make([]*streaming.PresenceEvent, 0, len(results))
	for _, result := range results {
		var event streaming.PresenceEvent
		if err := json.Unmarshal([]byte(result), &event); err != nil {
			continue
		}

		events = append(events, &event)
	}

	return events, nil
}

// GetHistorySince returns presence history for a user since the specified time.
func (s *PresenceStore) GetHistorySince(ctx context.Context, userID string, since time.Time) ([]*streaming.PresenceEvent, error) {
	historyKey := s.prefix + ":history:" + userID

	// Get events since timestamp
	minScore := float64(since.Unix())

	results, err := s.client.ZRangeByScore(ctx, historyKey, &redis.ZRangeBy{
		Min: string(rune(int(minScore))),
		Max: "+inf",
	}).Result()
	if err != nil {
		return nil, err
	}

	events := make([]*streaming.PresenceEvent, 0, len(results))
	for _, result := range results {
		var event streaming.PresenceEvent
		if err := json.Unmarshal([]byte(result), &event); err != nil {
			continue
		}

		events = append(events, &event)
	}

	return events, nil
}

// SetDevice sets device information for a user.
func (s *PresenceStore) SetDevice(ctx context.Context, userID, deviceID string, device streaming.DeviceInfo) error {
	deviceKey := s.prefix + ":devices:" + userID + ":" + deviceID

	deviceJSON, err := json.Marshal(device)
	if err != nil {
		return err
	}

	return s.client.Set(ctx, deviceKey, deviceJSON, 0).Err()
}

// GetDevices returns all devices for a user.
func (s *PresenceStore) GetDevices(ctx context.Context, userID string) ([]streaming.DeviceInfo, error) {
	pattern := s.prefix + ":devices:" + userID + ":*"

	keys, err := s.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, err
	}

	if len(keys) == 0 {
		return []streaming.DeviceInfo{}, nil
	}

	results, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}

	devices := make([]streaming.DeviceInfo, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}

		dataStr, ok := result.(string)
		if !ok {
			continue
		}

		var device streaming.DeviceInfo
		if err := json.Unmarshal([]byte(dataStr), &device); err != nil {
			continue
		}

		devices = append(devices, device)
	}

	return devices, nil
}

// RemoveDevice removes a device for a user.
func (s *PresenceStore) RemoveDevice(ctx context.Context, userID, deviceID string) error {
	deviceKey := s.prefix + ":devices:" + userID + ":" + deviceID

	return s.client.Del(ctx, deviceKey).Err()
}

// CountByStatus returns count of users by status.
func (s *PresenceStore) CountByStatus(ctx context.Context) (map[string]int, error) {
	// Get all presence keys
	pattern := s.prefix + ":*"

	keys, err := s.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, err
	}

	if len(keys) == 0 {
		return map[string]int{}, nil
	}

	// Filter out non-presence keys (history, devices, etc.)
	var presenceKeys []string

	for _, key := range keys {
		if !strings.Contains(key, ":history:") && !strings.Contains(key, ":devices:") && !strings.Contains(key, ":online") {
			presenceKeys = append(presenceKeys, key)
		}
	}

	if len(presenceKeys) == 0 {
		return map[string]int{}, nil
	}

	// Get all presence records
	results, err := s.client.MGet(ctx, presenceKeys...).Result()
	if err != nil {
		return nil, err
	}

	counts := make(map[string]int)

	for _, result := range results {
		if result == nil {
			continue
		}

		dataStr, ok := result.(string)
		if !ok {
			continue
		}

		var presence streaming.UserPresence
		if err := json.Unmarshal([]byte(dataStr), &presence); err != nil {
			continue
		}

		counts[presence.Status]++
	}

	return counts, nil
}

// GetActiveCount returns count of users active since the specified duration.
func (s *PresenceStore) GetActiveCount(ctx context.Context, since time.Duration) (int, error) {
	cutoff := time.Now().Add(-since)

	// Get all presence keys
	pattern := s.prefix + ":*"

	keys, err := s.client.Keys(ctx, pattern).Result()
	if err != nil {
		return 0, err
	}

	if len(keys) == 0 {
		return 0, nil
	}

	// Filter out non-presence keys
	var presenceKeys []string

	for _, key := range keys {
		if !strings.Contains(key, ":history:") && !strings.Contains(key, ":devices:") && !strings.Contains(key, ":online") {
			presenceKeys = append(presenceKeys, key)
		}
	}

	if len(presenceKeys) == 0 {
		return 0, nil
	}

	// Get all presence records
	results, err := s.client.MGet(ctx, presenceKeys...).Result()
	if err != nil {
		return 0, err
	}

	count := 0

	for _, result := range results {
		if result == nil {
			continue
		}

		dataStr, ok := result.(string)
		if !ok {
			continue
		}

		var presence streaming.UserPresence
		if err := json.Unmarshal([]byte(dataStr), &presence); err != nil {
			continue
		}

		if presence.LastSeen.After(cutoff) {
			count++
		}
	}

	return count, nil
}
