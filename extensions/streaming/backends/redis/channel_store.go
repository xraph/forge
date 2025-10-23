package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// ChannelStore implements streaming.ChannelStore with Redis backend.
type ChannelStore struct {
	client *redis.Client
	prefix string
}

// NewChannelStore creates a new Redis channel store.
func NewChannelStore(client *redis.Client, prefix string) streaming.ChannelStore {
	if prefix == "" {
		prefix = "streaming:channels"
	}
	return &ChannelStore{
		client: client,
		prefix: prefix,
	}
}

func (s *ChannelStore) Create(ctx context.Context, channel streaming.Channel) error {
	key := fmt.Sprintf("%s:%s", s.prefix, channel.GetID())

	data := map[string]interface{}{
		"id":            channel.GetID(),
		"name":          channel.GetName(),
		"created":       channel.GetCreated().Unix(),
		"message_count": channel.GetMessageCount(),
	}

	dataJSON, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// Check if exists
	exists, err := s.client.Exists(ctx, key).Result()
	if err != nil {
		return err
	}
	if exists > 0 {
		return streaming.ErrChannelAlreadyExists
	}

	// Store channel
	if err := s.client.Set(ctx, key, dataJSON, 0).Err(); err != nil {
		return err
	}

	// Add to channel list
	return s.client.SAdd(ctx, s.prefix+":list", channel.GetID()).Err()
}

func (s *ChannelStore) Get(ctx context.Context, channelID string) (streaming.Channel, error) {
	key := fmt.Sprintf("%s:%s", s.prefix, channelID)

	data, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, streaming.ErrChannelNotFound
	}
	if err != nil {
		return nil, err
	}

	var channelData map[string]interface{}
	if err := json.Unmarshal([]byte(data), &channelData); err != nil {
		return nil, err
	}

	return &redisChannel{
		store:        s,
		id:           channelData["id"].(string),
		name:         channelData["name"].(string),
		created:      time.Unix(int64(channelData["created"].(float64)), 0),
		messageCount: int64(channelData["message_count"].(float64)),
	}, nil
}

func (s *ChannelStore) Delete(ctx context.Context, channelID string) error {
	key := fmt.Sprintf("%s:%s", s.prefix, channelID)
	subsKey := fmt.Sprintf("%s:%s:subs", s.prefix, channelID)

	pipe := s.client.Pipeline()
	pipe.Del(ctx, key)
	pipe.Del(ctx, subsKey)
	pipe.SRem(ctx, s.prefix+":list", channelID)
	_, err := pipe.Exec(ctx)

	return err
}

func (s *ChannelStore) List(ctx context.Context) ([]streaming.Channel, error) {
	channelIDs, err := s.client.SMembers(ctx, s.prefix+":list").Result()
	if err != nil {
		return nil, err
	}

	channels := make([]streaming.Channel, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		channel, err := s.Get(ctx, channelID)
		if err != nil {
			continue
		}
		channels = append(channels, channel)
	}

	return channels, nil
}

func (s *ChannelStore) Exists(ctx context.Context, channelID string) (bool, error) {
	key := fmt.Sprintf("%s:%s", s.prefix, channelID)
	exists, err := s.client.Exists(ctx, key).Result()
	return exists > 0, err
}

func (s *ChannelStore) AddSubscription(ctx context.Context, channelID string, sub streaming.Subscription) error {
	subsKey := fmt.Sprintf("%s:%s:subs", s.prefix, channelID)

	// Check if channel exists
	exists, err := s.Exists(ctx, channelID)
	if err != nil {
		return err
	}
	if !exists {
		return streaming.ErrChannelNotFound
	}

	// Check if already subscribed
	isSub, err := s.client.HExists(ctx, subsKey, sub.GetConnID()).Result()
	if err != nil {
		return err
	}
	if isSub {
		return streaming.ErrAlreadySubscribed
	}

	// Serialize subscription
	subData := map[string]interface{}{
		"conn_id":       sub.GetConnID(),
		"user_id":       sub.GetUserID(),
		"subscribed_at": sub.GetSubscribedAt().Unix(),
		"filters":       sub.GetFilters(),
	}

	dataJSON, err := json.Marshal(subData)
	if err != nil {
		return err
	}

	return s.client.HSet(ctx, subsKey, sub.GetConnID(), dataJSON).Err()
}

func (s *ChannelStore) RemoveSubscription(ctx context.Context, channelID, connID string) error {
	subsKey := fmt.Sprintf("%s:%s:subs", s.prefix, channelID)

	exists, err := s.client.HExists(ctx, subsKey, connID).Result()
	if err != nil {
		return err
	}
	if !exists {
		return streaming.ErrNotSubscribed
	}

	return s.client.HDel(ctx, subsKey, connID).Err()
}

func (s *ChannelStore) GetSubscriptions(ctx context.Context, channelID string) ([]streaming.Subscription, error) {
	subsKey := fmt.Sprintf("%s:%s:subs", s.prefix, channelID)

	subData, err := s.client.HGetAll(ctx, subsKey).Result()
	if err != nil {
		return nil, err
	}

	subs := make([]streaming.Subscription, 0, len(subData))
	for _, data := range subData {
		var subInfo map[string]interface{}
		if err := json.Unmarshal([]byte(data), &subInfo); err != nil {
			continue
		}

		subs = append(subs, &redisSubscription{
			connID:       subInfo["conn_id"].(string),
			userID:       subInfo["user_id"].(string),
			subscribedAt: time.Unix(int64(subInfo["subscribed_at"].(float64)), 0),
			filters:      getMap(subInfo, "filters"),
		})
	}

	return subs, nil
}

func (s *ChannelStore) GetSubscriberCount(ctx context.Context, channelID string) (int, error) {
	subsKey := fmt.Sprintf("%s:%s:subs", s.prefix, channelID)
	count, err := s.client.HLen(ctx, subsKey).Result()
	return int(count), err
}

func (s *ChannelStore) IsSubscribed(ctx context.Context, channelID, connID string) (bool, error) {
	subsKey := fmt.Sprintf("%s:%s:subs", s.prefix, channelID)
	return s.client.HExists(ctx, subsKey, connID).Result()
}

func (s *ChannelStore) Publish(ctx context.Context, channelID string, message *streaming.Message) error {
	// Increment message count
	key := fmt.Sprintf("%s:%s", s.prefix, channelID)

	// Get current data
	channel, err := s.Get(ctx, channelID)
	if err != nil {
		return err
	}

	// Update message count
	data := map[string]interface{}{
		"id":            channel.GetID(),
		"name":          channel.GetName(),
		"created":       channel.GetCreated().Unix(),
		"message_count": channel.GetMessageCount() + 1,
	}

	dataJSON, err := json.Marshal(data)
	if err != nil {
		return err
	}

	return s.client.Set(ctx, key, dataJSON, 0).Err()
}

func (s *ChannelStore) GetUserChannels(ctx context.Context, userID string) ([]streaming.Channel, error) {
	// This requires scanning all channels - could be optimized with an index
	channelIDs, err := s.client.SMembers(ctx, s.prefix+":list").Result()
	if err != nil {
		return nil, err
	}

	channels := make([]streaming.Channel, 0)
	for _, channelID := range channelIDs {
		subsKey := fmt.Sprintf("%s:%s:subs", s.prefix, channelID)

		// Get all subscriptions and check if any belong to this user
		subs, err := s.client.HGetAll(ctx, subsKey).Result()
		if err != nil {
			continue
		}

		for _, subData := range subs {
			var subInfo map[string]interface{}
			if err := json.Unmarshal([]byte(subData), &subInfo); err != nil {
				continue
			}

			if subInfo["user_id"].(string) == userID {
				channel, err := s.Get(ctx, channelID)
				if err == nil {
					channels = append(channels, channel)
				}
				break
			}
		}
	}

	return channels, nil
}

func (s *ChannelStore) Connect(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *ChannelStore) Disconnect(ctx context.Context) error {
	return nil
}

func (s *ChannelStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

// redisChannel implements streaming.Channel
type redisChannel struct {
	store        *ChannelStore
	id           string
	name         string
	created      time.Time
	messageCount int64
}

func (c *redisChannel) GetID() string          { return c.id }
func (c *redisChannel) GetName() string        { return c.name }
func (c *redisChannel) GetCreated() time.Time  { return c.created }
func (c *redisChannel) GetMessageCount() int64 { return c.messageCount }

func (c *redisChannel) Subscribe(ctx context.Context, sub streaming.Subscription) error {
	return streaming.ErrInvalidChannel
}

func (c *redisChannel) Unsubscribe(ctx context.Context, connID string) error {
	return streaming.ErrInvalidChannel
}

func (c *redisChannel) GetSubscribers(ctx context.Context) ([]streaming.Subscription, error) {
	return nil, streaming.ErrInvalidChannel
}

func (c *redisChannel) GetSubscriberCount(ctx context.Context) (int, error) {
	return 0, streaming.ErrInvalidChannel
}

func (c *redisChannel) IsSubscribed(ctx context.Context, connID string) (bool, error) {
	return false, streaming.ErrInvalidChannel
}

func (c *redisChannel) Publish(ctx context.Context, message *streaming.Message) error {
	return streaming.ErrInvalidChannel
}

func (c *redisChannel) Delete(ctx context.Context) error {
	return streaming.ErrInvalidChannel
}

// redisSubscription implements streaming.Subscription
type redisSubscription struct {
	connID       string
	userID       string
	subscribedAt time.Time
	filters      map[string]any
}

func (s *redisSubscription) GetConnID() string                 { return s.connID }
func (s *redisSubscription) GetUserID() string                 { return s.userID }
func (s *redisSubscription) GetSubscribedAt() time.Time        { return s.subscribedAt }
func (s *redisSubscription) GetFilters() map[string]any        { return s.filters }
func (s *redisSubscription) SetFilters(filters map[string]any) { s.filters = filters }

func (s *redisSubscription) MatchesFilter(message *streaming.Message) bool {
	if len(s.filters) == 0 {
		return true
	}

	for key, filterValue := range s.filters {
		if message.Metadata == nil {
			return false
		}
		msgValue, exists := message.Metadata[key]
		if !exists || msgValue != filterValue {
			return false
		}
	}

	return true
}
