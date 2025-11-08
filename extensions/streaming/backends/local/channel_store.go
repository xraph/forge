package local

import (
	"context"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// ChannelStore implements streaming.ChannelStore with in-memory storage.
type ChannelStore struct {
	mu            sync.RWMutex
	channels      map[string]*LocalChannel
	subscriptions map[string]map[string]*LocalSubscription // channelID -> connID -> subscription
}

// NewChannelStore creates a new local channel store.
func NewChannelStore() streaming.ChannelStore {
	return &ChannelStore{
		channels:      make(map[string]*LocalChannel),
		subscriptions: make(map[string]map[string]*LocalSubscription),
	}
}

func (s *ChannelStore) Create(ctx context.Context, channel streaming.Channel) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.channels[channel.GetID()]; exists {
		return streaming.ErrChannelAlreadyExists
	}

	localChannel, ok := channel.(*LocalChannel)
	if !ok {
		localChannel = &LocalChannel{
			id:           channel.GetID(),
			name:         channel.GetName(),
			created:      channel.GetCreated(),
			messageCount: channel.GetMessageCount(),
		}
	}

	s.channels[localChannel.id] = localChannel
	s.subscriptions[localChannel.id] = make(map[string]*LocalSubscription)

	return nil
}

func (s *ChannelStore) Get(ctx context.Context, channelID string) (streaming.Channel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	channel, exists := s.channels[channelID]
	if !exists {
		return nil, streaming.ErrChannelNotFound
	}

	return channel, nil
}

func (s *ChannelStore) Delete(ctx context.Context, channelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.channels[channelID]; !exists {
		return streaming.ErrChannelNotFound
	}

	delete(s.channels, channelID)
	delete(s.subscriptions, channelID)

	return nil
}

func (s *ChannelStore) List(ctx context.Context) ([]streaming.Channel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	channels := make([]streaming.Channel, 0, len(s.channels))
	for _, channel := range s.channels {
		channels = append(channels, channel)
	}

	return channels, nil
}

func (s *ChannelStore) Exists(ctx context.Context, channelID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.channels[channelID]

	return exists, nil
}

func (s *ChannelStore) AddSubscription(ctx context.Context, channelID string, sub streaming.Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.channels[channelID]; !exists {
		return streaming.ErrChannelNotFound
	}

	channelSubs, exists := s.subscriptions[channelID]
	if !exists {
		channelSubs = make(map[string]*LocalSubscription)
		s.subscriptions[channelID] = channelSubs
	}

	if _, exists := channelSubs[sub.GetConnID()]; exists {
		return streaming.ErrAlreadySubscribed
	}

	localSub, ok := sub.(*LocalSubscription)
	if !ok {
		localSub = &LocalSubscription{
			connID:       sub.GetConnID(),
			userID:       sub.GetUserID(),
			subscribedAt: sub.GetSubscribedAt(),
			filters:      sub.GetFilters(),
		}
	}

	channelSubs[sub.GetConnID()] = localSub

	return nil
}

func (s *ChannelStore) RemoveSubscription(ctx context.Context, channelID, connID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	channelSubs, exists := s.subscriptions[channelID]
	if !exists {
		return streaming.ErrChannelNotFound
	}

	if _, exists := channelSubs[connID]; !exists {
		return streaming.ErrNotSubscribed
	}

	delete(channelSubs, connID)

	return nil
}

func (s *ChannelStore) GetSubscriptions(ctx context.Context, channelID string) ([]streaming.Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	channelSubs, exists := s.subscriptions[channelID]
	if !exists {
		return nil, streaming.ErrChannelNotFound
	}

	subs := make([]streaming.Subscription, 0, len(channelSubs))
	for _, sub := range channelSubs {
		subs = append(subs, sub)
	}

	return subs, nil
}

func (s *ChannelStore) GetSubscriberCount(ctx context.Context, channelID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	channelSubs, exists := s.subscriptions[channelID]
	if !exists {
		return 0, streaming.ErrChannelNotFound
	}

	return len(channelSubs), nil
}

func (s *ChannelStore) IsSubscribed(ctx context.Context, channelID, connID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	channelSubs, exists := s.subscriptions[channelID]
	if !exists {
		return false, streaming.ErrChannelNotFound
	}

	_, exists = channelSubs[connID]

	return exists, nil
}

func (s *ChannelStore) Publish(ctx context.Context, channelID string, message *streaming.Message) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	channel, exists := s.channels[channelID]
	if !exists {
		return streaming.ErrChannelNotFound
	}

	atomic.AddInt64(&channel.messageCount, 1)

	return nil
}

func (s *ChannelStore) GetUserChannels(ctx context.Context, userID string) ([]streaming.Channel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	channels := make([]streaming.Channel, 0)

	for channelID, channelSubs := range s.subscriptions {
		for _, sub := range channelSubs {
			if sub.userID == userID {
				if channel, exists := s.channels[channelID]; exists {
					channels = append(channels, channel)

					break
				}
			}
		}
	}

	return channels, nil
}

func (s *ChannelStore) Connect(ctx context.Context) error {
	return nil // No-op for local
}

func (s *ChannelStore) Disconnect(ctx context.Context) error {
	return nil // No-op for local
}

func (s *ChannelStore) Ping(ctx context.Context) error {
	return nil // No-op for local
}

// LocalChannel implements streaming.Channel.
type LocalChannel struct {
	mu           sync.RWMutex
	id           string
	name         string
	created      time.Time
	messageCount int64
}

func NewLocalChannel(opts streaming.ChannelOptions) *LocalChannel {
	return &LocalChannel{
		id:           opts.ID,
		name:         opts.Name,
		created:      time.Now(),
		messageCount: 0,
	}
}

func (c *LocalChannel) GetID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.id
}

func (c *LocalChannel) GetName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.name
}

func (c *LocalChannel) GetCreated() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.created
}

func (c *LocalChannel) GetMessageCount() int64 {
	return atomic.LoadInt64(&c.messageCount)
}

func (c *LocalChannel) Subscribe(ctx context.Context, sub streaming.Subscription) error {
	return streaming.ErrInvalidChannel // Managed by ChannelStore
}

func (c *LocalChannel) Unsubscribe(ctx context.Context, connID string) error {
	return streaming.ErrInvalidChannel // Managed by ChannelStore
}

func (c *LocalChannel) GetSubscribers(ctx context.Context) ([]streaming.Subscription, error) {
	return nil, streaming.ErrInvalidChannel // Managed by ChannelStore
}

func (c *LocalChannel) GetSubscriberCount(ctx context.Context) (int, error) {
	return 0, streaming.ErrInvalidChannel // Managed by ChannelStore
}

func (c *LocalChannel) IsSubscribed(ctx context.Context, connID string) (bool, error) {
	return false, streaming.ErrInvalidChannel // Managed by ChannelStore
}

func (c *LocalChannel) Publish(ctx context.Context, message *streaming.Message) error {
	return streaming.ErrInvalidChannel // Managed by Manager
}

func (c *LocalChannel) Delete(ctx context.Context) error {
	return streaming.ErrInvalidChannel // Managed by ChannelStore
}

// LocalSubscription implements streaming.Subscription.
type LocalSubscription struct {
	mu           sync.RWMutex
	connID       string
	userID       string
	subscribedAt time.Time
	filters      map[string]any
}

func NewLocalSubscription(opts streaming.SubscriptionOptions) *LocalSubscription {
	return &LocalSubscription{
		connID:       opts.ConnID,
		userID:       opts.UserID,
		subscribedAt: time.Now(),
		filters:      opts.Filters,
	}
}

func (s *LocalSubscription) GetConnID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.connID
}

func (s *LocalSubscription) GetUserID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.userID
}

func (s *LocalSubscription) GetSubscribedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.subscribedAt
}

func (s *LocalSubscription) GetFilters() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	filters := make(map[string]any, len(s.filters))
	maps.Copy(filters, s.filters)

	return filters
}

func (s *LocalSubscription) SetFilters(filters map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.filters = filters
}

func (s *LocalSubscription) MatchesFilter(message *streaming.Message) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.filters) == 0 {
		return true // No filters, match all
	}

	// Check if message metadata matches filters
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
