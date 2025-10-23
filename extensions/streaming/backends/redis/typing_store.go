package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// TypingStore implements streaming.TypingStore with Redis backend.
type TypingStore struct {
	client *redis.Client
	prefix string
}

// NewTypingStore creates a new Redis typing store.
func NewTypingStore(client *redis.Client, prefix string) streaming.TypingStore {
	if prefix == "" {
		prefix = "streaming:typing"
	}
	return &TypingStore{
		client: client,
		prefix: prefix,
	}
}

func (s *TypingStore) SetTyping(ctx context.Context, userID, roomID string, expiresAt time.Time) error {
	key := fmt.Sprintf("%s:%s", s.prefix, roomID)

	// Add to sorted set with expiration time as score
	return s.client.ZAdd(ctx, key, redis.Z{
		Score:  float64(expiresAt.Unix()),
		Member: userID,
	}).Err()
}

func (s *TypingStore) RemoveTyping(ctx context.Context, userID, roomID string) error {
	key := fmt.Sprintf("%s:%s", s.prefix, roomID)
	return s.client.ZRem(ctx, key, userID).Err()
}

func (s *TypingStore) GetTypingUsers(ctx context.Context, roomID string) ([]string, error) {
	key := fmt.Sprintf("%s:%s", s.prefix, roomID)

	// Get all users with expiration time > now
	now := time.Now().Unix()
	users, err := s.client.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min: fmt.Sprintf("%d", now),
		Max: "+inf",
	}).Result()

	if err != nil {
		return nil, err
	}

	return users, nil
}

func (s *TypingStore) IsTyping(ctx context.Context, userID, roomID string) (bool, error) {
	key := fmt.Sprintf("%s:%s", s.prefix, roomID)

	score, err := s.client.ZScore(ctx, key, userID).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	// Check if not expired
	return time.Now().Unix() < int64(score), nil
}

func (s *TypingStore) CleanupExpired(ctx context.Context) error {
	// Use SCAN to find all typing keys
	var cursor uint64
	now := time.Now().Unix()

	for {
		keys, nextCursor, err := s.client.Scan(ctx, cursor, s.prefix+":*", 100).Result()
		if err != nil {
			return err
		}

		// Remove expired entries from each key
		for _, key := range keys {
			if err := s.client.ZRemRangeByScore(ctx, key, "-inf", string(rune(now))).Err(); err != nil {
				continue // Skip errors, continue cleanup
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return nil
}

func (s *TypingStore) Connect(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *TypingStore) Disconnect(ctx context.Context) error {
	return nil
}

func (s *TypingStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}
