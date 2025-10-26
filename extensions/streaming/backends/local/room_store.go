package local

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// RoomStore implements streaming.RoomStore with in-memory storage.
type RoomStore struct {
	mu             sync.RWMutex
	rooms          map[string]*LocalRoom
	members        map[string]map[string]*LocalMember       // roomID -> userID -> member
	bans           map[string]map[string]*streaming.RoomBan // roomID -> userID -> ban
	invites        map[string]*streaming.Invite             // inviteCode -> invite
	roomInvites    map[string][]string                      // roomID -> []inviteCode
	moderationLogs map[string][]*streaming.ModerationEvent  // roomID -> events
}

// NewRoomStore creates a new local room store.
func NewRoomStore() streaming.RoomStore {
	return &RoomStore{
		rooms:          make(map[string]*LocalRoom),
		members:        make(map[string]map[string]*LocalMember),
		bans:           make(map[string]map[string]*streaming.RoomBan),
		invites:        make(map[string]*streaming.Invite),
		roomInvites:    make(map[string][]string),
		moderationLogs: make(map[string][]*streaming.ModerationEvent),
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
			// Initialize additional fields
			isPrivate:      false,
			maxMembers:     0, // 0 means unlimited
			isArchived:     false,
			isLocked:       false,
			slowMode:       0,
			pinnedMessages: make([]string, 0),
			tags:           make([]string, 0),
			category:       "",
			readMarkers:    make(map[string]string),
			mutedMembers:   make(map[string]time.Time),
		}
	}

	s.rooms[localRoom.id] = localRoom
	s.members[localRoom.id] = make(map[string]*LocalMember)
	s.bans[localRoom.id] = make(map[string]*streaming.RoomBan)
	s.roomInvites[localRoom.id] = make([]string, 0)
	s.moderationLogs[localRoom.id] = make([]*streaming.ModerationEvent, 0)

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
	for _, room := range rooms {
		if err := s.Create(ctx, room); err != nil {
			return err
		}
	}
	return nil
}

func (s *RoomStore) DeleteMany(ctx context.Context, roomIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, roomID := range roomIDs {
		if _, exists := s.rooms[roomID]; !exists {
			return streaming.ErrRoomNotFound
		}
		delete(s.rooms, roomID)
		delete(s.members, roomID)
		delete(s.bans, roomID)

		// Clean up invites
		if inviteCodes, exists := s.roomInvites[roomID]; exists {
			for _, code := range inviteCodes {
				delete(s.invites, code)
			}
			delete(s.roomInvites, roomID)
		}
		delete(s.moderationLogs, roomID)
	}
	return nil
}

func (s *RoomStore) GetUserRoomsByRole(ctx context.Context, userID, role string) ([]streaming.Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var rooms []streaming.Room
	for roomID, roomMembers := range s.members {
		if member, exists := roomMembers[userID]; exists && member.role == role {
			if room, exists := s.rooms[roomID]; exists {
				rooms = append(rooms, room)
			}
		}
	}
	return rooms, nil
}

func (s *RoomStore) GetCommonRooms(ctx context.Context, userID1, userID2 string) ([]streaming.Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var commonRooms []streaming.Room
	for roomID, roomMembers := range s.members {
		if _, exists1 := roomMembers[userID1]; exists1 {
			if _, exists2 := roomMembers[userID2]; exists2 {
				if room, exists := s.rooms[roomID]; exists {
					commonRooms = append(commonRooms, room)
				}
			}
		}
	}
	return commonRooms, nil
}

func (s *RoomStore) Search(ctx context.Context, query string, filters map[string]any) ([]streaming.Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query = strings.ToLower(query)
	var results []streaming.Room

	for _, room := range s.rooms {
		// Search in name and description
		if strings.Contains(strings.ToLower(room.name), query) ||
			strings.Contains(strings.ToLower(room.description), query) {

			// Apply filters
			if s.matchesFilters(room, filters) {
				results = append(results, room)
			}
		}
	}
	return results, nil
}

func (s *RoomStore) FindByTag(ctx context.Context, tag string) ([]streaming.Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []streaming.Room
	for _, room := range s.rooms {
		for _, roomTag := range room.tags {
			if roomTag == tag {
				results = append(results, room)
				break
			}
		}
	}
	return results, nil
}

func (s *RoomStore) FindByCategory(ctx context.Context, category string) ([]streaming.Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []streaming.Room
	for _, room := range s.rooms {
		if room.category == category {
			results = append(results, room)
		}
	}
	return results, nil
}

func (s *RoomStore) GetPublicRooms(ctx context.Context, limit int) ([]streaming.Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var publicRooms []streaming.Room
	for _, room := range s.rooms {
		if !room.isPrivate && !room.isArchived {
			publicRooms = append(publicRooms, room)
		}
	}

	// Sort by creation time (newest first)
	sort.Slice(publicRooms, func(i, j int) bool {
		return publicRooms[i].GetCreated().After(publicRooms[j].GetCreated())
	})

	if limit > 0 && len(publicRooms) > limit {
		publicRooms = publicRooms[:limit]
	}

	return publicRooms, nil
}

func (s *RoomStore) GetArchivedRooms(ctx context.Context, userID string) ([]streaming.Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var archivedRooms []streaming.Room
	for roomID, roomMembers := range s.members {
		if _, isMember := roomMembers[userID]; isMember {
			if room, exists := s.rooms[roomID]; exists && room.isArchived {
				archivedRooms = append(archivedRooms, room)
			}
		}
	}
	return archivedRooms, nil
}

func (s *RoomStore) GetRoomCount(ctx context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.rooms), nil
}

func (s *RoomStore) GetTotalMembers(ctx context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	totalMembers := 0
	for _, roomMembers := range s.members {
		totalMembers += len(roomMembers)
	}
	return totalMembers, nil
}

func (s *RoomStore) BanMember(ctx context.Context, roomID, userID string, ban streaming.RoomBan) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.rooms[roomID]; !exists {
		return streaming.ErrRoomNotFound
	}

	// Initialize bans map for room if needed
	if _, exists := s.bans[roomID]; !exists {
		s.bans[roomID] = make(map[string]*streaming.RoomBan)
	}

	// Remove member if they exist
	if roomMembers, exists := s.members[roomID]; exists {
		delete(roomMembers, userID)
	}

	// Add ban
	banCopy := ban
	s.bans[roomID][userID] = &banCopy

	// Log moderation event
	s.addModerationEvent(roomID, &streaming.ModerationEvent{
		ID:          fmt.Sprintf("ban_%s_%d", userID, time.Now().Unix()),
		Type:        streaming.ModerationEventBan,
		RoomID:      roomID,
		TargetID:    userID,
		ModeratorID: ban.BannedBy,
		Reason:      ban.Reason,
		Timestamp:   time.Now(),
	})

	return nil
}

func (s *RoomStore) UnbanMember(ctx context.Context, roomID, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.rooms[roomID]; !exists {
		return streaming.ErrRoomNotFound
	}

	if roomBans, exists := s.bans[roomID]; exists {
		if ban, exists := roomBans[userID]; exists {
			delete(roomBans, userID)

			// Log moderation event
			s.addModerationEvent(roomID, &streaming.ModerationEvent{
				ID:          fmt.Sprintf("unban_%s_%d", userID, time.Now().Unix()),
				Type:        streaming.ModerationEventUnban,
				RoomID:      roomID,
				TargetID:    userID,
				ModeratorID: ban.BannedBy, // Use original banner as moderator
				Timestamp:   time.Now(),
			})
		}
	}

	return nil
}

func (s *RoomStore) GetBans(ctx context.Context, roomID string) ([]streaming.RoomBan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, exists := s.rooms[roomID]; !exists {
		return nil, streaming.ErrRoomNotFound
	}

	var bans []streaming.RoomBan
	if roomBans, exists := s.bans[roomID]; exists {
		for _, ban := range roomBans {
			// Check if ban is still active
			if ban.ExpiresAt == nil || ban.ExpiresAt.After(time.Now()) {
				bans = append(bans, *ban)
			}
		}
	}
	return bans, nil
}

func (s *RoomStore) IsBanned(ctx context.Context, roomID, userID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, exists := s.rooms[roomID]; !exists {
		return false, streaming.ErrRoomNotFound
	}

	if roomBans, exists := s.bans[roomID]; exists {
		if ban, exists := roomBans[userID]; exists {
			// Check if ban is still active
			if ban.ExpiresAt == nil || ban.ExpiresAt.After(time.Now()) {
				return true, nil
			}
			// Ban expired, remove it
			delete(roomBans, userID)
		}
	}
	return false, nil
}

func (s *RoomStore) SaveInvite(ctx context.Context, roomID string, invite *streaming.Invite) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.rooms[roomID]; !exists {
		return streaming.ErrRoomNotFound
	}

	s.invites[invite.Code] = invite
	s.roomInvites[roomID] = append(s.roomInvites[roomID], invite.Code)

	return nil
}

func (s *RoomStore) GetInvite(ctx context.Context, inviteCode string) (*streaming.Invite, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	invite, exists := s.invites[inviteCode]
	if !exists {
		return nil, streaming.ErrInviteNotFound
	}

	// Check if invite is expired
	if invite.ExpiresAt != nil && invite.ExpiresAt.Before(time.Now()) {
		return nil, streaming.ErrInviteExpired
	}

	// Check if invite has reached max uses
	if invite.MaxUses > 0 && invite.UsedCount >= invite.MaxUses {
		return nil, streaming.ErrInviteExpired
	}

	return invite, nil
}

func (s *RoomStore) DeleteInvite(ctx context.Context, inviteCode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	invite, exists := s.invites[inviteCode]
	if !exists {
		return streaming.ErrInviteNotFound
	}

	// Remove from room invites list
	if inviteCodes, exists := s.roomInvites[invite.RoomID]; exists {
		for i, code := range inviteCodes {
			if code == inviteCode {
				s.roomInvites[invite.RoomID] = append(inviteCodes[:i], inviteCodes[i+1:]...)
				break
			}
		}
	}

	delete(s.invites, inviteCode)
	return nil
}

func (s *RoomStore) ListInvites(ctx context.Context, roomID string) ([]*streaming.Invite, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, exists := s.rooms[roomID]; !exists {
		return nil, streaming.ErrRoomNotFound
	}

	var invites []*streaming.Invite
	if inviteCodes, exists := s.roomInvites[roomID]; exists {
		for _, code := range inviteCodes {
			if invite, exists := s.invites[code]; exists {
				// Only include non-expired invites
				if invite.ExpiresAt == nil || invite.ExpiresAt.After(time.Now()) {
					if invite.MaxUses == 0 || invite.UsedCount < invite.MaxUses {
						invites = append(invites, invite)
					}
				}
			}
		}
	}
	return invites, nil
}

// Helper methods
func (s *RoomStore) matchesFilters(room *LocalRoom, filters map[string]any) bool {
	if filters == nil {
		return true
	}

	if isPrivate, ok := filters["private"].(bool); ok && room.isPrivate != isPrivate {
		return false
	}

	if isArchived, ok := filters["archived"].(bool); ok && room.isArchived != isArchived {
		return false
	}

	if category, ok := filters["category"].(string); ok && room.category != category {
		return false
	}

	if owner, ok := filters["owner"].(string); ok && room.owner != owner {
		return false
	}

	return true
}

func (s *RoomStore) addModerationEvent(roomID string, event *streaming.ModerationEvent) {
	if _, exists := s.moderationLogs[roomID]; !exists {
		s.moderationLogs[roomID] = make([]*streaming.ModerationEvent, 0)
	}
	s.moderationLogs[roomID] = append(s.moderationLogs[roomID], event)
}

// Room implements streaming.Room for local backend
type LocalRoom struct {
	mu          sync.RWMutex
	id          string
	name        string
	description string
	owner       string
	created     time.Time
	updated     time.Time
	metadata    map[string]any
	// Additional fields for extended functionality
	isPrivate      bool
	maxMembers     int
	isArchived     bool
	isLocked       bool
	lockReason     string
	slowMode       int // seconds
	pinnedMessages []string
	tags           []string
	category       string
	readMarkers    map[string]string    // userID -> messageID
	mutedMembers   map[string]time.Time // userID -> unmute time
}

func NewRoom(opts streaming.RoomOptions) *LocalRoom {
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
	return nil, streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) UpdateMemberRole(ctx context.Context, userID, newRole string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	// This would typically be handled by the RoomStore, but we can update local state
	r.updated = time.Now()
	return nil
}

func (r *LocalRoom) TransferOwnership(ctx context.Context, newOwnerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.owner = newOwnerID
	r.updated = time.Now()
	return nil
}

func (r *LocalRoom) BanMember(ctx context.Context, userID string, reason string, until *time.Time) error {
	return streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) UnbanMember(ctx context.Context, userID string) error {
	return streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) IsBanned(ctx context.Context, userID string) (bool, error) {
	return false, streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) GetBannedMembers(ctx context.Context) ([]streaming.RoomBan, error) {
	return nil, streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) MuteMember(ctx context.Context, userID string, duration time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mutedMembers[userID] = time.Now().Add(duration)
	return nil
}

func (r *LocalRoom) UnmuteMember(ctx context.Context, userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.mutedMembers, userID)
	return nil
}

func (r *LocalRoom) IsMuted(ctx context.Context, userID string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if muteUntil, exists := r.mutedMembers[userID]; exists {
		if muteUntil.After(time.Now()) {
			return true, nil
		}
		// Mute expired, clean it up
		delete(r.mutedMembers, userID)
	}
	return false, nil
}

func (r *LocalRoom) CreateInvite(ctx context.Context, opts streaming.InviteOptions) (*streaming.Invite, error) {
	return nil, streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) RevokeInvite(ctx context.Context, inviteCode string) error {
	return streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) ValidateInvite(ctx context.Context, inviteCode string) (bool, error) {
	return false, streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) GetInvites(ctx context.Context) ([]*streaming.Invite, error) {
	return nil, streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) JoinWithInvite(ctx context.Context, userID, inviteCode string) error {
	return streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) IsPrivate() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.isPrivate
}

func (r *LocalRoom) SetPrivate(ctx context.Context, private bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.isPrivate = private
	r.updated = time.Now()
	return nil
}

func (r *LocalRoom) GetMaxMembers() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.maxMembers
}

func (r *LocalRoom) SetMaxMembers(ctx context.Context, max int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.maxMembers = max
	r.updated = time.Now()
	return nil
}

func (r *LocalRoom) IsArchived() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.isArchived
}

func (r *LocalRoom) Archive(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.isArchived = true
	r.updated = time.Now()
	return nil
}

func (r *LocalRoom) Unarchive(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.isArchived = false
	r.updated = time.Now()
	return nil
}

func (r *LocalRoom) IsLocked() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.isLocked
}

func (r *LocalRoom) Lock(ctx context.Context, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.isLocked = true
	r.lockReason = reason
	r.updated = time.Now()
	return nil
}

func (r *LocalRoom) Unlock(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.isLocked = false
	r.lockReason = ""
	r.updated = time.Now()
	return nil
}

func (r *LocalRoom) GetModerationLog(ctx context.Context, limit int) ([]streaming.ModerationEvent, error) {
	return nil, streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *LocalRoom) SetSlowMode(ctx context.Context, intervalSeconds int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.slowMode = intervalSeconds
	r.updated = time.Now()
	return nil
}

func (r *LocalRoom) GetSlowMode(ctx context.Context) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.slowMode
}

func (r *LocalRoom) PinMessage(ctx context.Context, messageID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if already pinned
	for _, pinned := range r.pinnedMessages {
		if pinned == messageID {
			return nil // Already pinned
		}
	}

	r.pinnedMessages = append(r.pinnedMessages, messageID)
	r.updated = time.Now()
	return nil
}

func (r *LocalRoom) UnpinMessage(ctx context.Context, messageID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, pinned := range r.pinnedMessages {
		if pinned == messageID {
			r.pinnedMessages = append(r.pinnedMessages[:i], r.pinnedMessages[i+1:]...)
			r.updated = time.Now()
			break
		}
	}
	return nil
}

func (r *LocalRoom) GetPinnedMessages(ctx context.Context) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Return copy to prevent external modification
	pinned := make([]string, len(r.pinnedMessages))
	copy(pinned, r.pinnedMessages)
	return pinned, nil
}

func (r *LocalRoom) AddTag(ctx context.Context, tag string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if tag already exists
	for _, existingTag := range r.tags {
		if existingTag == tag {
			return nil // Already exists
		}
	}

	r.tags = append(r.tags, tag)
	r.updated = time.Now()
	return nil
}

func (r *LocalRoom) RemoveTag(ctx context.Context, tag string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, existingTag := range r.tags {
		if existingTag == tag {
			r.tags = append(r.tags[:i], r.tags[i+1:]...)
			r.updated = time.Now()
			break
		}
	}
	return nil
}

func (r *LocalRoom) GetTags() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Return copy to prevent external modification
	tags := make([]string, len(r.tags))
	copy(tags, r.tags)
	return tags
}

func (r *LocalRoom) SetCategory(ctx context.Context, category string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.category = category
	r.updated = time.Now()
	return nil
}

func (r *LocalRoom) GetCategory() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.category
}

func (r *LocalRoom) GetMessageCount(ctx context.Context) (int64, error) {
	return 0, streaming.ErrInvalidRoom // Would need MessageStore integration
}

func (r *LocalRoom) GetActiveMembers(ctx context.Context, since time.Duration) ([]streaming.Member, error) {
	return nil, streaming.ErrInvalidRoom // Managed by RoomStore with presence integration
}

func (r *LocalRoom) BroadcastExcept(ctx context.Context, message *streaming.Message, excludeUserIDs []string) error {
	return streaming.ErrInvalidRoom // Managed by Manager
}

func (r *LocalRoom) BroadcastToRole(ctx context.Context, message *streaming.Message, role string) error {
	return streaming.ErrInvalidRoom // Managed by Manager
}

func (r *LocalRoom) MarkAsRead(ctx context.Context, userID, messageID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.readMarkers[userID] = messageID
	return nil
}

func (r *LocalRoom) GetUnreadCount(ctx context.Context, userID string, since time.Time) (int, error) {
	return 0, streaming.ErrInvalidRoom // Would need MessageStore integration
}

func (r *LocalRoom) GetLastReadMessage(ctx context.Context, userID string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if messageID, exists := r.readMarkers[userID]; exists {
		return messageID, nil
	}
	return "", nil
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
