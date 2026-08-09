package streaming

import (
	"slices"
	"sync"
	"time"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// basicMember is a backend-neutral Member the manager can construct when a
// connection joins a room.
//
// The manager must not build a *local.LocalMember: that would tie the join path
// to one backend, and the Redis store would then be handed a type it only reads
// through the interface anyway. Both stores accept any streaming.Member and copy
// it through the getters (see backends/local/room_store.go:174 and
// backends/redis/room_store.go:204), so the neutral type is what belongs here.
type basicMember struct {
	mu          sync.RWMutex
	userID      string
	role        string
	joinedAt    time.Time
	permissions []string
	metadata    map[string]any
}

// NewMember creates a Member suitable for handing to any RoomStore.
func NewMember(opts streaming.MemberOptions) streaming.Member {
	role := opts.Role
	if role == "" {
		role = streaming.RoleMember
	}

	metadata := opts.Metadata
	if metadata == nil {
		metadata = make(map[string]any)
	}

	return &basicMember{
		userID:      opts.UserID,
		role:        role,
		joinedAt:    time.Now(),
		permissions: slices.Clone(opts.Permissions),
		metadata:    metadata,
	}
}

func (m *basicMember) GetUserID() string { return m.userID }

func (m *basicMember) GetRole() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.role
}

func (m *basicMember) GetJoinedAt() time.Time { return m.joinedAt }

func (m *basicMember) GetPermissions() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return slices.Clone(m.permissions)
}

func (m *basicMember) SetRole(role string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.role = role
}

func (m *basicMember) HasPermission(permission string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return slices.Contains(m.permissions, permission)
}

func (m *basicMember) GrantPermission(permission string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !slices.Contains(m.permissions, permission) {
		m.permissions = append(m.permissions, permission)
	}
}

func (m *basicMember) RevokePermission(permission string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.permissions = slices.DeleteFunc(m.permissions, func(p string) bool {
		return p == permission
	})
}

func (m *basicMember) GetMetadata() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cp := make(map[string]any, len(m.metadata))
	for k, v := range m.metadata {
		cp[k] = v
	}

	return cp
}

func (m *basicMember) SetMetadata(key string, value any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.metadata[key] = value
}

// basicSubscription is the Subscription counterpart to basicMember: a
// backend-neutral value the manager can hand to any ChannelStore.
type basicSubscription struct {
	mu           sync.RWMutex
	connID       string
	userID       string
	subscribedAt time.Time
	filters      map[string]any
}

// NewSubscription creates a Subscription suitable for handing to any ChannelStore.
func NewSubscription(opts streaming.SubscriptionOptions) streaming.Subscription {
	filters := opts.Filters
	if filters == nil {
		filters = make(map[string]any)
	}

	return &basicSubscription{
		connID:       opts.ConnID,
		userID:       opts.UserID,
		subscribedAt: time.Now(),
		filters:      filters,
	}
}

func (s *basicSubscription) GetConnID() string { return s.connID }

func (s *basicSubscription) GetUserID() string { return s.userID }

func (s *basicSubscription) GetSubscribedAt() time.Time { return s.subscribedAt }

func (s *basicSubscription) GetFilters() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cp := make(map[string]any, len(s.filters))
	for k, v := range s.filters {
		cp[k] = v
	}

	return cp
}

func (s *basicSubscription) SetFilters(filters map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.filters = filters
}

// MatchesFilter reports whether a message satisfies every filter on this
// subscription. An empty filter set matches everything.
//
// Matches the semantics of the local backend's implementation
// (backends/local/channel_store.go:367): filters are compared against message
// metadata, and a missing key is a non-match rather than a pass.
func (s *basicSubscription) MatchesFilter(message *streaming.Message) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.filters) == 0 {
		return true
	}

	for key, want := range s.filters {
		if message.Metadata == nil {
			return false
		}

		got, exists := message.Metadata[key]
		if !exists || got != want {
			return false
		}
	}

	return true
}
