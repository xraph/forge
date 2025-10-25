package local

import (
	"context"
	"sync"
	"time"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// RoomStore implements streaming.RoomStore with in-memory storage.
type RoomStore struct {
	mu      sync.RWMutex
	rooms   map[string]*LocalRoom
	members map[string]map[string]*LocalMember // roomID -> userID -> member
}

// NewRoomStore creates a new local room store.
func NewRoomStore() streaming.RoomStore {
	return &RoomStore{
		rooms:   make(map[string]*LocalRoom),
		members: make(map[string]map[string]*LocalMember),
	}
}

func (s *RoomStore) Create(ctx context.Context, room streaming.Room) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.rooms[room.GetID()]; exists {
		return streaming.ErrRoomAlreadyExists
	}

	localRoom, ok := room.(*LocalRoom)
	if !ok {
		// Convert to local room
		localRoom = &LocalRoom{
			id:          room.GetID(),
			name:        room.GetName(),
			description: room.GetDescription(),
			owner:       room.GetOwner(),
			created:     room.GetCreated(),
			updated:     room.GetUpdated(),
			metadata:    room.GetMetadata(),
		}
	}

	s.rooms[localRoom.id] = localRoom
	s.members[localRoom.id] = make(map[string]*LocalMember)

	return nil
}

func (s *RoomStore) Get(ctx context.Context, roomID string) (streaming.Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	room, exists := s.rooms[roomID]
	if !exists {
		return nil, streaming.ErrRoomNotFound
	}

	return room, nil
}

func (s *RoomStore) Update(ctx context.Context, roomID string, updates map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	room, exists := s.rooms[roomID]
	if !exists {
		return streaming.ErrRoomNotFound
	}

	if name, ok := updates["name"].(string); ok {
		room.name = name
	}
	if desc, ok := updates["description"].(string); ok {
		room.description = desc
	}
	if metadata, ok := updates["metadata"].(map[string]any); ok {
		room.metadata = metadata
	}

	room.updated = time.Now()

	return nil
}

func (s *RoomStore) Delete(ctx context.Context, roomID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.rooms[roomID]; !exists {
		return streaming.ErrRoomNotFound
	}

	delete(s.rooms, roomID)
	delete(s.members, roomID)

	return nil
}

func (s *RoomStore) List(ctx context.Context, filters map[string]any) ([]streaming.Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rooms := make([]streaming.Room, 0, len(s.rooms))
	for _, room := range s.rooms {
		rooms = append(rooms, room)
	}

	return rooms, nil
}

func (s *RoomStore) Exists(ctx context.Context, roomID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.rooms[roomID]
	return exists, nil
}

func (s *RoomStore) AddMember(ctx context.Context, roomID string, member streaming.Member) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.rooms[roomID]; !exists {
		return streaming.ErrRoomNotFound
	}

	roomMembers, exists := s.members[roomID]
	if !exists {
		roomMembers = make(map[string]*LocalMember)
		s.members[roomID] = roomMembers
	}

	if _, exists := roomMembers[member.GetUserID()]; exists {
		return streaming.ErrAlreadyRoomMember
	}

	localMember, ok := member.(*LocalMember)
	if !ok {
		localMember = &LocalMember{
			userID:      member.GetUserID(),
			role:        member.GetRole(),
			joinedAt:    member.GetJoinedAt(),
			permissions: member.GetPermissions(),
			metadata:    member.GetMetadata(),
		}
	}

	roomMembers[member.GetUserID()] = localMember

	return nil
}

func (s *RoomStore) RemoveMember(ctx context.Context, roomID, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	roomMembers, exists := s.members[roomID]
	if !exists {
		return streaming.ErrRoomNotFound
	}

	if _, exists := roomMembers[userID]; !exists {
		return streaming.ErrNotRoomMember
	}

	delete(roomMembers, userID)

	return nil
}

func (s *RoomStore) GetMembers(ctx context.Context, roomID string) ([]streaming.Member, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	roomMembers, exists := s.members[roomID]
	if !exists {
		return nil, streaming.ErrRoomNotFound
	}

	members := make([]streaming.Member, 0, len(roomMembers))
	for _, member := range roomMembers {
		members = append(members, member)
	}

	return members, nil
}

func (s *RoomStore) GetMember(ctx context.Context, roomID, userID string) (streaming.Member, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	roomMembers, exists := s.members[roomID]
	if !exists {
		return nil, streaming.ErrRoomNotFound
	}

	member, exists := roomMembers[userID]
	if !exists {
		return nil, streaming.ErrNotRoomMember
	}

	return member, nil
}

func (s *RoomStore) IsMember(ctx context.Context, roomID, userID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	roomMembers, exists := s.members[roomID]
	if !exists {
		return false, streaming.ErrRoomNotFound
	}

	_, exists = roomMembers[userID]
	return exists, nil
}

func (s *RoomStore) MemberCount(ctx context.Context, roomID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	roomMembers, exists := s.members[roomID]
	if !exists {
		return 0, streaming.ErrRoomNotFound
	}

	return len(roomMembers), nil
}

func (s *RoomStore) GetUserRooms(ctx context.Context, userID string) ([]streaming.Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rooms := make([]streaming.Room, 0)
	for roomID, roomMembers := range s.members {
		if _, exists := roomMembers[userID]; exists {
			if room, exists := s.rooms[roomID]; exists {
				rooms = append(rooms, room)
			}
		}
	}

	return rooms, nil
}

func (s *RoomStore) Connect(ctx context.Context) error {
	return nil // No-op for local
}

func (s *RoomStore) Disconnect(ctx context.Context) error {
	return nil // No-op for local
}

func (s *RoomStore) Ping(ctx context.Context) error {
	return nil // No-op for local
}

func (s *RoomStore) CreateMany(ctx context.Context, rooms []streaming.Room) error {
	// TODO implement me
	panic("implement me")
}

func (s *RoomStore) DeleteMany(ctx context.Context, roomIDs []string) error {
	// TODO implement me
	panic("implement me")
}

func (s *RoomStore) GetUserRoomsByRole(ctx context.Context, userID, role string) ([]streaming.Room, error) {
	// TODO implement me
	panic("implement me")
}

func (s *RoomStore) GetCommonRooms(ctx context.Context, userID1, userID2 string) ([]streaming.Room, error) {
	// TODO implement me
	panic("implement me")
}

func (s *RoomStore) Search(ctx context.Context, query string, filters map[string]any) ([]streaming.Room, error) {
	// TODO implement me
	panic("implement me")
}

func (s *RoomStore) FindByTag(ctx context.Context, tag string) ([]streaming.Room, error) {
	// TODO implement me
	panic("implement me")
}

func (s *RoomStore) FindByCategory(ctx context.Context, category string) ([]streaming.Room, error) {
	// TODO implement me
	panic("implement me")
}

func (s *RoomStore) GetPublicRooms(ctx context.Context, limit int) ([]streaming.Room, error) {
	// TODO implement me
	panic("implement me")
}

func (s *RoomStore) GetArchivedRooms(ctx context.Context, userID string) ([]streaming.Room, error) {
	// TODO implement me
	panic("implement me")
}

func (s *RoomStore) GetRoomCount(ctx context.Context) (int, error) {
	// TODO implement me
	panic("implement me")
}

func (s *RoomStore) GetTotalMembers(ctx context.Context) (int, error) {
	// TODO implement me
	panic("implement me")
}

func (s *RoomStore) BanMember(ctx context.Context, roomID, userID string, ban streaming.RoomBan) error {
	// TODO implement me
	panic("implement me")
}

func (s *RoomStore) UnbanMember(ctx context.Context, roomID, userID string) error {
	// TODO implement me
	panic("implement me")
}

func (s *RoomStore) GetBans(ctx context.Context, roomID string) ([]streaming.RoomBan, error) {
	// TODO implement me
	panic("implement me")
}

func (s *RoomStore) IsBanned(ctx context.Context, roomID, userID string) (bool, error) {
	// TODO implement me
	panic("implement me")
}

func (s *RoomStore) SaveInvite(ctx context.Context, roomID string, invite *streaming.Invite) error {
	// TODO implement me
	panic("implement me")
}

func (s *RoomStore) GetInvite(ctx context.Context, inviteCode string) (*streaming.Invite, error) {
	// TODO implement me
	panic("implement me")
}

func (s *RoomStore) DeleteInvite(ctx context.Context, inviteCode string) error {
	// TODO implement me
	panic("implement me")
}

func (s *RoomStore) ListInvites(ctx context.Context, roomID string) ([]*streaming.Invite, error) {
	// TODO implement me
	panic("implement me")
}

// LocalRoom implements streaming.Room
type LocalRoom struct {
	mu          sync.RWMutex
	id          string
	name        string
	description string
	owner       string
	created     time.Time
	updated     time.Time
	metadata    map[string]any
}

func NewLocalRoom(opts streaming.RoomOptions) *LocalRoom {
	now := time.Now()
	return &LocalRoom{
		id:          opts.ID,
		name:        opts.Name,
		description: opts.Description,
		owner:       opts.Owner,
		created:     now,
		updated:     now,
		metadata:    opts.Metadata,
	}
}

func (r *LocalRoom) GetID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.id
}

func (r *LocalRoom) GetName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.name
}

func (r *LocalRoom) GetDescription() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.description
}

func (r *LocalRoom) GetOwner() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.owner
}

func (r *LocalRoom) GetCreated() time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.created
}

func (r *LocalRoom) GetUpdated() time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.updated
}

func (r *LocalRoom) GetMetadata() map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	// Return copy to prevent external modification
	metadata := make(map[string]any, len(r.metadata))
	for k, v := range r.metadata {
		metadata[k] = v
	}
	return metadata
}

func (r *LocalRoom) Join(ctx context.Context, userID, role string) error {
	return streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) Leave(ctx context.Context, userID string) error {
	return streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) IsMember(ctx context.Context, userID string) (bool, error) {
	return false, streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) GetMembers(ctx context.Context) ([]streaming.Member, error) {
	return nil, streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) GetMember(ctx context.Context, userID string) (streaming.Member, error) {
	return nil, streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) MemberCount(ctx context.Context) (int, error) {
	return 0, streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) HasPermission(ctx context.Context, userID, permission string) (bool, error) {
	return false, streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) GrantPermission(ctx context.Context, userID, permission string) error {
	return streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) RevokePermission(ctx context.Context, userID, permission string) error {
	return streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) Broadcast(ctx context.Context, message *streaming.Message) error {
	return streaming.ErrInvalidRoom // Managed by Manager
}

func (r *LocalRoom) Update(ctx context.Context, updates map[string]any) error {
	return streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) Delete(ctx context.Context) error {
	return streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) GetMembersByRole(ctx context.Context, role string) ([]streaming.Member, error) {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) UpdateMemberRole(ctx context.Context, userID, newRole string) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) TransferOwnership(ctx context.Context, newOwnerID string) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) BanMember(ctx context.Context, userID string, reason string, until *time.Time) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) UnbanMember(ctx context.Context, userID string) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) IsBanned(ctx context.Context, userID string) (bool, error) {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) GetBannedMembers(ctx context.Context) ([]streaming.RoomBan, error) {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) MuteMember(ctx context.Context, userID string, duration time.Duration) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) UnmuteMember(ctx context.Context, userID string) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) IsMuted(ctx context.Context, userID string) (bool, error) {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) CreateInvite(ctx context.Context, opts streaming.InviteOptions) (*streaming.Invite, error) {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) RevokeInvite(ctx context.Context, inviteCode string) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) ValidateInvite(ctx context.Context, inviteCode string) (bool, error) {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) GetInvites(ctx context.Context) ([]*streaming.Invite, error) {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) JoinWithInvite(ctx context.Context, userID, inviteCode string) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) IsPrivate() bool {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) SetPrivate(ctx context.Context, private bool) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) GetMaxMembers() int {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) SetMaxMembers(ctx context.Context, max int) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) IsArchived() bool {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) Archive(ctx context.Context) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) Unarchive(ctx context.Context) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) IsLocked() bool {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) Lock(ctx context.Context, reason string) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) Unlock(ctx context.Context) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) GetModerationLog(ctx context.Context, limit int) ([]streaming.ModerationEvent, error) {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) SetSlowMode(ctx context.Context, intervalSeconds int) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) GetSlowMode(ctx context.Context) int {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) PinMessage(ctx context.Context, messageID string) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) UnpinMessage(ctx context.Context, messageID string) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) GetPinnedMessages(ctx context.Context) ([]string, error) {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) AddTag(ctx context.Context, tag string) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) RemoveTag(ctx context.Context, tag string) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) GetTags() []string {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) SetCategory(ctx context.Context, category string) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) GetCategory() string {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) GetMessageCount(ctx context.Context) (int64, error) {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) GetActiveMembers(ctx context.Context, since time.Duration) ([]streaming.Member, error) {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) BroadcastExcept(ctx context.Context, message *streaming.Message, excludeUserIDs []string) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) BroadcastToRole(ctx context.Context, message *streaming.Message, role string) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) MarkAsRead(ctx context.Context, userID, messageID string) error {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) GetUnreadCount(ctx context.Context, userID string, since time.Time) (int, error) {
	// TODO implement me
	panic("implement me")
}

func (r *LocalRoom) GetLastReadMessage(ctx context.Context, userID string) (string, error) {
	// TODO implement me
	panic("implement me")
}

// LocalMember implements streaming.Member
type LocalMember struct {
	mu          sync.RWMutex
	userID      string
	role        string
	joinedAt    time.Time
	permissions []string
	metadata    map[string]any
}

func NewLocalMember(opts streaming.MemberOptions) *LocalMember {
	return &LocalMember{
		userID:      opts.UserID,
		role:        opts.Role,
		joinedAt:    time.Now(),
		permissions: opts.Permissions,
		metadata:    opts.Metadata,
	}
}

func (m *LocalMember) GetUserID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.userID
}

func (m *LocalMember) GetRole() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.role
}

func (m *LocalMember) GetJoinedAt() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.joinedAt
}

func (m *LocalMember) GetPermissions() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	perms := make([]string, len(m.permissions))
	copy(perms, m.permissions)
	return perms
}

func (m *LocalMember) SetRole(role string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.role = role
}

func (m *LocalMember) HasPermission(permission string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.permissions {
		if p == permission {
			return true
		}
	}
	return false
}

func (m *LocalMember) GrantPermission(permission string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.HasPermission(permission) {
		m.permissions = append(m.permissions, permission)
	}
}

func (m *LocalMember) RevokePermission(permission string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, p := range m.permissions {
		if p == permission {
			m.permissions = append(m.permissions[:i], m.permissions[i+1:]...)
			break
		}
	}
}

func (m *LocalMember) GetMetadata() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	metadata := make(map[string]any, len(m.metadata))
	for k, v := range m.metadata {
		metadata[k] = v
	}
	return metadata
}

func (m *LocalMember) SetMetadata(key string, value any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.metadata == nil {
		m.metadata = make(map[string]any)
	}
	m.metadata[key] = value
}
