package redis

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
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
	if err == redis.Nil {
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
	if err == redis.Nil {
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
