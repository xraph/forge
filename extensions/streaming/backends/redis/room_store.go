package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
	"github.com/xraph/forge/internal/errors"
)

// RoomStore implements streaming.RoomStore with Redis backend.
type RoomStore struct {
	client *redis.Client
	prefix string
}

// NewRoomStore creates a new Redis room store.
func NewRoomStore(client *redis.Client, prefix string) streaming.RoomStore {
	if prefix == "" {
		prefix = "streaming:rooms"
	}

	return &RoomStore{
		client: client,
		prefix: prefix,
	}
}

func (s *RoomStore) Create(ctx context.Context, room streaming.Room) error {
	key := fmt.Sprintf("%s:%s", s.prefix, room.GetID())

	// Serialize room data
	data := map[string]interface{}{
		"id":          room.GetID(),
		"name":        room.GetName(),
		"description": room.GetDescription(),
		"owner":       room.GetOwner(),
		"created":     room.GetCreated().Unix(),
		"updated":     room.GetUpdated().Unix(),
		"metadata":    room.GetMetadata(),
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
		return streaming.ErrRoomAlreadyExists
	}

	// Store room
	if err := s.client.Set(ctx, key, dataJSON, 0).Err(); err != nil {
		return err
	}

	// Add to room list
	return s.client.SAdd(ctx, s.prefix+":list", room.GetID()).Err()
}

func (s *RoomStore) Get(ctx context.Context, roomID string) (streaming.Room, error) {
	key := fmt.Sprintf("%s:%s", s.prefix, roomID)

	data, err := s.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return nil, streaming.ErrRoomNotFound
	}

	if err != nil {
		return nil, err
	}

	var roomData map[string]interface{}
	if err := json.Unmarshal([]byte(data), &roomData); err != nil {
		return nil, err
	}

	return &redisRoom{
		store:       s,
		id:          roomData["id"].(string),
		name:        roomData["name"].(string),
		description: getString(roomData, "description"),
		owner:       roomData["owner"].(string),
		created:     time.Unix(int64(roomData["created"].(float64)), 0),
		updated:     time.Unix(int64(roomData["updated"].(float64)), 0),
		metadata:    getMap(roomData, "metadata"),
	}, nil
}

func (s *RoomStore) Update(ctx context.Context, roomID string, updates map[string]any) error {
	key := fmt.Sprintf("%s:%s", s.prefix, roomID)

	// Get existing room
	room, err := s.Get(ctx, roomID)
	if err != nil {
		return err
	}

	// Update fields
	data := map[string]interface{}{
		"id":          room.GetID(),
		"name":        room.GetName(),
		"description": room.GetDescription(),
		"owner":       room.GetOwner(),
		"created":     room.GetCreated().Unix(),
		"updated":     time.Now().Unix(),
		"metadata":    room.GetMetadata(),
	}

	if name, ok := updates["name"].(string); ok {
		data["name"] = name
	}

	if desc, ok := updates["description"].(string); ok {
		data["description"] = desc
	}

	if metadata, ok := updates["metadata"].(map[string]any); ok {
		data["metadata"] = metadata
	}

	dataJSON, err := json.Marshal(data)
	if err != nil {
		return err
	}

	return s.client.Set(ctx, key, dataJSON, 0).Err()
}

func (s *RoomStore) Delete(ctx context.Context, roomID string) error {
	key := fmt.Sprintf("%s:%s", s.prefix, roomID)
	membersKey := fmt.Sprintf("%s:%s:members", s.prefix, roomID)

	// Delete room, members, and from list
	pipe := s.client.Pipeline()
	pipe.Del(ctx, key)
	pipe.Del(ctx, membersKey)
	pipe.SRem(ctx, s.prefix+":list", roomID)
	_, err := pipe.Exec(ctx)

	return err
}

func (s *RoomStore) List(ctx context.Context, filters map[string]any) ([]streaming.Room, error) {
	roomIDs, err := s.client.SMembers(ctx, s.prefix+":list").Result()
	if err != nil {
		return nil, err
	}

	rooms := make([]streaming.Room, 0, len(roomIDs))
	for _, roomID := range roomIDs {
		room, err := s.Get(ctx, roomID)
		if err != nil {
			continue // Skip invalid rooms
		}

		rooms = append(rooms, room)
	}

	return rooms, nil
}

func (s *RoomStore) Exists(ctx context.Context, roomID string) (bool, error) {
	key := fmt.Sprintf("%s:%s", s.prefix, roomID)
	exists, err := s.client.Exists(ctx, key).Result()

	return exists > 0, err
}

func (s *RoomStore) AddMember(ctx context.Context, roomID string, member streaming.Member) error {
	membersKey := fmt.Sprintf("%s:%s:members", s.prefix, roomID)

	// Check if room exists
	exists, err := s.Exists(ctx, roomID)
	if err != nil {
		return err
	}

	if !exists {
		return streaming.ErrRoomNotFound
	}

	// Check if already member
	isMember, err := s.client.HExists(ctx, membersKey, member.GetUserID()).Result()
	if err != nil {
		return err
	}

	if isMember {
		return streaming.ErrAlreadyRoomMember
	}

	// Serialize member
	memberData := map[string]interface{}{
		"user_id":     member.GetUserID(),
		"role":        member.GetRole(),
		"joined_at":   member.GetJoinedAt().Unix(),
		"permissions": member.GetPermissions(),
		"metadata":    member.GetMetadata(),
	}

	dataJSON, err := json.Marshal(memberData)
	if err != nil {
		return err
	}

	return s.client.HSet(ctx, membersKey, member.GetUserID(), dataJSON).Err()
}

func (s *RoomStore) RemoveMember(ctx context.Context, roomID, userID string) error {
	membersKey := fmt.Sprintf("%s:%s:members", s.prefix, roomID)

	// Check if member exists
	exists, err := s.client.HExists(ctx, membersKey, userID).Result()
	if err != nil {
		return err
	}

	if !exists {
		return streaming.ErrNotRoomMember
	}

	return s.client.HDel(ctx, membersKey, userID).Err()
}

func (s *RoomStore) GetMembers(ctx context.Context, roomID string) ([]streaming.Member, error) {
	membersKey := fmt.Sprintf("%s:%s:members", s.prefix, roomID)

	memberData, err := s.client.HGetAll(ctx, membersKey).Result()
	if err != nil {
		return nil, err
	}

	members := make([]streaming.Member, 0, len(memberData))
	for _, data := range memberData {
		var memberInfo map[string]interface{}
		if err := json.Unmarshal([]byte(data), &memberInfo); err != nil {
			continue
		}

		members = append(members, &redisMember{
			userID:      memberInfo["user_id"].(string),
			role:        memberInfo["role"].(string),
			joinedAt:    time.Unix(int64(memberInfo["joined_at"].(float64)), 0),
			permissions: getStringSlice(memberInfo, "permissions"),
			metadata:    getMap(memberInfo, "metadata"),
		})
	}

	return members, nil
}

func (s *RoomStore) GetMember(ctx context.Context, roomID, userID string) (streaming.Member, error) {
	membersKey := fmt.Sprintf("%s:%s:members", s.prefix, roomID)

	data, err := s.client.HGet(ctx, membersKey, userID).Result()
	if errors.Is(err, redis.Nil) {
		return nil, streaming.ErrNotRoomMember
	}

	if err != nil {
		return nil, err
	}

	var memberInfo map[string]interface{}
	if err := json.Unmarshal([]byte(data), &memberInfo); err != nil {
		return nil, err
	}

	return &redisMember{
		userID:      memberInfo["user_id"].(string),
		role:        memberInfo["role"].(string),
		joinedAt:    time.Unix(int64(memberInfo["joined_at"].(float64)), 0),
		permissions: getStringSlice(memberInfo, "permissions"),
		metadata:    getMap(memberInfo, "metadata"),
	}, nil
}

func (s *RoomStore) IsMember(ctx context.Context, roomID, userID string) (bool, error) {
	membersKey := fmt.Sprintf("%s:%s:members", s.prefix, roomID)

	return s.client.HExists(ctx, membersKey, userID).Result()
}

func (s *RoomStore) MemberCount(ctx context.Context, roomID string) (int, error) {
	membersKey := fmt.Sprintf("%s:%s:members", s.prefix, roomID)
	count, err := s.client.HLen(ctx, membersKey).Result()

	return int(count), err
}

func (s *RoomStore) GetUserRooms(ctx context.Context, userID string) ([]streaming.Room, error) {
	// This requires scanning all rooms - could be optimized with an index
	roomIDs, err := s.client.SMembers(ctx, s.prefix+":list").Result()
	if err != nil {
		return nil, err
	}

	rooms := make([]streaming.Room, 0)

	for _, roomID := range roomIDs {
		isMember, err := s.IsMember(ctx, roomID, userID)
		if err != nil || !isMember {
			continue
		}

		room, err := s.Get(ctx, roomID)
		if err != nil {
			continue
		}

		rooms = append(rooms, room)
	}

	return rooms, nil
}

func (s *RoomStore) Connect(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *RoomStore) Disconnect(ctx context.Context) error {
	return nil // Redis client manages its own connections
}

func (s *RoomStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

// CreateMany creates multiple rooms in a batch.
func (s *RoomStore) CreateMany(ctx context.Context, rooms []streaming.Room) error {
	pipe := s.client.Pipeline()

	for _, room := range rooms {
		key := fmt.Sprintf("%s:%s", s.prefix, room.GetID())

		data := map[string]interface{}{
			"id":          room.GetID(),
			"name":        room.GetName(),
			"description": room.GetDescription(),
			"owner":       room.GetOwner(),
			"created":     room.GetCreated().Unix(),
			"updated":     room.GetUpdated().Unix(),
			"metadata":    room.GetMetadata(),
		}

		dataJSON, err := json.Marshal(data)
		if err != nil {
			return err
		}

		pipe.Set(ctx, key, dataJSON, 0)
		pipe.SAdd(ctx, s.prefix+":list", room.GetID())
	}

	_, err := pipe.Exec(ctx)

	return err
}

// DeleteMany deletes multiple rooms by their IDs.
func (s *RoomStore) DeleteMany(ctx context.Context, roomIDs []string) error {
	pipe := s.client.Pipeline()

	for _, roomID := range roomIDs {
		key := fmt.Sprintf("%s:%s", s.prefix, roomID)
		membersKey := fmt.Sprintf("%s:%s:members", s.prefix, roomID)
		bansKey := fmt.Sprintf("%s:%s:bans", s.prefix, roomID)
		invitesKey := fmt.Sprintf("%s:%s:invites", s.prefix, roomID)

		pipe.Del(ctx, key, membersKey, bansKey, invitesKey)
		pipe.SRem(ctx, s.prefix+":list", roomID)
	}

	_, err := pipe.Exec(ctx)

	return err
}

// GetUserRoomsByRole gets all rooms where a user has a specific role.
func (s *RoomStore) GetUserRoomsByRole(ctx context.Context, userID, role string) ([]streaming.Room, error) {
	roomIDs, err := s.client.SMembers(ctx, s.prefix+":list").Result()
	if err != nil {
		return nil, err
	}

	rooms := make([]streaming.Room, 0)

	for _, roomID := range roomIDs {
		member, err := s.GetMember(ctx, roomID, userID)
		if err != nil || member.GetRole() != role {
			continue
		}

		room, err := s.Get(ctx, roomID)
		if err != nil {
			continue
		}

		rooms = append(rooms, room)
	}

	return rooms, nil
}

// GetCommonRooms gets rooms that both users are members of.
func (s *RoomStore) GetCommonRooms(ctx context.Context, userID1, userID2 string) ([]streaming.Room, error) {
	roomIDs, err := s.client.SMembers(ctx, s.prefix+":list").Result()
	if err != nil {
		return nil, err
	}

	rooms := make([]streaming.Room, 0)

	for _, roomID := range roomIDs {
		isMember1, err1 := s.IsMember(ctx, roomID, userID1)
		isMember2, err2 := s.IsMember(ctx, roomID, userID2)

		if err1 != nil || err2 != nil || !isMember1 || !isMember2 {
			continue
		}

		room, err := s.Get(ctx, roomID)
		if err != nil {
			continue
		}

		rooms = append(rooms, room)
	}

	return rooms, nil
}

// Search searches for rooms by query and filters.
func (s *RoomStore) Search(ctx context.Context, query string, filters map[string]any) ([]streaming.Room, error) {
	// Simple implementation - could be enhanced with proper search indexing
	roomIDs, err := s.client.SMembers(ctx, s.prefix+":list").Result()
	if err != nil {
		return nil, err
	}

	rooms := make([]streaming.Room, 0)

	for _, roomID := range roomIDs {
		room, err := s.Get(ctx, roomID)
		if err != nil {
			continue
		}

		// Simple text matching
		if query != "" {
			if !contains(room.GetName(), query) && !contains(room.GetDescription(), query) {
				continue
			}
		}

		// Apply filters
		if !matchesFilters(room, filters) {
			continue
		}

		rooms = append(rooms, room)
	}

	return rooms, nil
}

// FindByTag finds rooms with a specific tag.
func (s *RoomStore) FindByTag(ctx context.Context, tag string) ([]streaming.Room, error) {
	// This would require tag indexing in a real implementation
	return s.Search(ctx, "", map[string]any{"tag": tag})
}

// FindByCategory finds rooms in a specific category.
func (s *RoomStore) FindByCategory(ctx context.Context, category string) ([]streaming.Room, error) {
	return s.Search(ctx, "", map[string]any{"category": category})
}

// GetPublicRooms gets public rooms up to the specified limit.
func (s *RoomStore) GetPublicRooms(ctx context.Context, limit int) ([]streaming.Room, error) {
	return s.Search(ctx, "", map[string]any{"private": false, "limit": limit})
}

// GetArchivedRooms gets archived rooms for a user.
func (s *RoomStore) GetArchivedRooms(ctx context.Context, userID string) ([]streaming.Room, error) {
	return s.Search(ctx, "", map[string]any{"archived": true, "member": userID})
}

// GetRoomCount gets the total number of rooms.
func (s *RoomStore) GetRoomCount(ctx context.Context) (int, error) {
	count, err := s.client.SCard(ctx, s.prefix+":list").Result()

	return int(count), err
}

// GetTotalMembers gets the total number of members across all rooms.
func (s *RoomStore) GetTotalMembers(ctx context.Context) (int, error) {
	roomIDs, err := s.client.SMembers(ctx, s.prefix+":list").Result()
	if err != nil {
		return 0, err
	}

	total := 0

	for _, roomID := range roomIDs {
		count, err := s.MemberCount(ctx, roomID)
		if err != nil {
			continue
		}

		total += count
	}

	return total, nil
}

// BanMember bans a member from a room.
func (s *RoomStore) BanMember(ctx context.Context, roomID, userID string, ban streaming.RoomBan) error {
	bansKey := fmt.Sprintf("%s:%s:bans", s.prefix, roomID)

	banData, err := json.Marshal(ban)
	if err != nil {
		return err
	}

	return s.client.HSet(ctx, bansKey, userID, banData).Err()
}

// UnbanMember removes a ban from a member.
func (s *RoomStore) UnbanMember(ctx context.Context, roomID, userID string) error {
	bansKey := fmt.Sprintf("%s:%s:bans", s.prefix, roomID)

	return s.client.HDel(ctx, bansKey, userID).Err()
}

// GetBans gets all bans for a room.
func (s *RoomStore) GetBans(ctx context.Context, roomID string) ([]streaming.RoomBan, error) {
	bansKey := fmt.Sprintf("%s:%s:bans", s.prefix, roomID)

	bansData, err := s.client.HGetAll(ctx, bansKey).Result()
	if err != nil {
		return nil, err
	}

	bans := make([]streaming.RoomBan, 0, len(bansData))
	for _, banJSON := range bansData {
		var ban streaming.RoomBan
		if err := json.Unmarshal([]byte(banJSON), &ban); err != nil {
			continue
		}

		bans = append(bans, ban)
	}

	return bans, nil
}

// IsBanned checks if a user is banned from a room.
func (s *RoomStore) IsBanned(ctx context.Context, roomID, userID string) (bool, error) {
	bansKey := fmt.Sprintf("%s:%s:bans", s.prefix, roomID)
	exists, err := s.client.HExists(ctx, bansKey, userID).Result()

	return exists, err
}

// SaveInvite saves an invite for a room.
func (s *RoomStore) SaveInvite(ctx context.Context, roomID string, invite *streaming.Invite) error {
	invitesKey := fmt.Sprintf("%s:%s:invites", s.prefix, roomID)
	inviteKey := fmt.Sprintf("%s:invite:%s", s.prefix, invite.Code)

	inviteData, err := json.Marshal(invite)
	if err != nil {
		return err
	}

	pipe := s.client.Pipeline()
	pipe.HSet(ctx, invitesKey, invite.Code, inviteData)
	pipe.Set(ctx, inviteKey, inviteData, 0)

	_, err = pipe.Exec(ctx)

	return err
}

// GetInvite gets an invite by its code.
func (s *RoomStore) GetInvite(ctx context.Context, inviteCode string) (*streaming.Invite, error) {
	inviteKey := fmt.Sprintf("%s:invite:%s", s.prefix, inviteCode)

	inviteData, err := s.client.Get(ctx, inviteKey).Result()
	if errors.Is(err, redis.Nil) {
		return nil, streaming.ErrInviteNotFound
	}

	if err != nil {
		return nil, err
	}

	var invite streaming.Invite
	if err := json.Unmarshal([]byte(inviteData), &invite); err != nil {
		return nil, err
	}

	return &invite, nil
}

// DeleteInvite deletes an invite by its code.
func (s *RoomStore) DeleteInvite(ctx context.Context, inviteCode string) error {
	inviteKey := fmt.Sprintf("%s:invite:%s", s.prefix, inviteCode)

	// Get the invite to find the room ID
	invite, err := s.GetInvite(ctx, inviteCode)
	if err != nil {
		return err
	}

	invitesKey := fmt.Sprintf("%s:%s:invites", s.prefix, invite.RoomID)

	pipe := s.client.Pipeline()
	pipe.HDel(ctx, invitesKey, inviteCode)
	pipe.Del(ctx, inviteKey)

	_, err = pipe.Exec(ctx)

	return err
}

// ListInvites lists all invites for a room.
func (s *RoomStore) ListInvites(ctx context.Context, roomID string) ([]*streaming.Invite, error) {
	invitesKey := fmt.Sprintf("%s:%s:invites", s.prefix, roomID)

	invitesData, err := s.client.HGetAll(ctx, invitesKey).Result()
	if err != nil {
		return nil, err
	}

	invites := make([]*streaming.Invite, 0, len(invitesData))
	for _, inviteJSON := range invitesData {
		var invite streaming.Invite
		if err := json.Unmarshal([]byte(inviteJSON), &invite); err != nil {
			continue
		}

		invites = append(invites, &invite)
	}

	return invites, nil
}

// redisRoom implements streaming.Room.
type redisRoom struct {
	store       *RoomStore
	id          string
	name        string
	description string
	owner       string
	created     time.Time
	updated     time.Time
	metadata    map[string]any
}

func (r *redisRoom) GetID() string               { return r.id }
func (r *redisRoom) GetName() string             { return r.name }
func (r *redisRoom) GetDescription() string      { return r.description }
func (r *redisRoom) GetOwner() string            { return r.owner }
func (r *redisRoom) GetCreated() time.Time       { return r.created }
func (r *redisRoom) GetUpdated() time.Time       { return r.updated }
func (r *redisRoom) GetMetadata() map[string]any { return r.metadata }

func (r *redisRoom) Join(ctx context.Context, userID, role string) error {
	return streaming.ErrInvalidRoom // Managed by RoomStore
}

func (r *redisRoom) Leave(ctx context.Context, userID string) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) IsMember(ctx context.Context, userID string) (bool, error) {
	return false, streaming.ErrInvalidRoom
}

func (r *redisRoom) GetMembers(ctx context.Context) ([]streaming.Member, error) {
	return nil, streaming.ErrInvalidRoom
}

func (r *redisRoom) GetMember(ctx context.Context, userID string) (streaming.Member, error) {
	return nil, streaming.ErrInvalidRoom
}

func (r *redisRoom) MemberCount(ctx context.Context) (int, error) {
	return 0, streaming.ErrInvalidRoom
}

func (r *redisRoom) HasPermission(ctx context.Context, userID, permission string) (bool, error) {
	return false, streaming.ErrInvalidRoom
}

func (r *redisRoom) GrantPermission(ctx context.Context, userID, permission string) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) RevokePermission(ctx context.Context, userID, permission string) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) Broadcast(ctx context.Context, message *streaming.Message) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) Update(ctx context.Context, updates map[string]any) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) Delete(ctx context.Context) error {
	return streaming.ErrInvalidRoom
}

// Advanced Membership Management.
func (r *redisRoom) GetMembersByRole(ctx context.Context, role string) ([]streaming.Member, error) {
	return nil, streaming.ErrInvalidRoom
}

func (r *redisRoom) UpdateMemberRole(ctx context.Context, userID, newRole string) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) TransferOwnership(ctx context.Context, newOwnerID string) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) BanMember(ctx context.Context, userID string, reason string, until *time.Time) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) UnbanMember(ctx context.Context, userID string) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) IsBanned(ctx context.Context, userID string) (bool, error) {
	return false, streaming.ErrInvalidRoom
}

func (r *redisRoom) GetBannedMembers(ctx context.Context) ([]streaming.RoomBan, error) {
	return nil, streaming.ErrInvalidRoom
}

func (r *redisRoom) MuteMember(ctx context.Context, userID string, duration time.Duration) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) UnmuteMember(ctx context.Context, userID string) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) IsMuted(ctx context.Context, userID string) (bool, error) {
	return false, streaming.ErrInvalidRoom
}

// Invitations.
func (r *redisRoom) CreateInvite(ctx context.Context, opts streaming.InviteOptions) (*streaming.Invite, error) {
	return nil, streaming.ErrInvalidRoom
}

func (r *redisRoom) RevokeInvite(ctx context.Context, inviteCode string) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) ValidateInvite(ctx context.Context, inviteCode string) (bool, error) {
	return false, streaming.ErrInvalidRoom
}

func (r *redisRoom) GetInvites(ctx context.Context) ([]*streaming.Invite, error) {
	return nil, streaming.ErrInvalidRoom
}

func (r *redisRoom) JoinWithInvite(ctx context.Context, userID, inviteCode string) error {
	return streaming.ErrInvalidRoom
}

// Room Settings.
func (r *redisRoom) IsPrivate() bool {
	return false // Default implementation
}

func (r *redisRoom) SetPrivate(ctx context.Context, private bool) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) GetMaxMembers() int {
	return 0 // Default implementation
}

func (r *redisRoom) SetMaxMembers(ctx context.Context, max int) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) IsArchived() bool {
	return false // Default implementation
}

func (r *redisRoom) Archive(ctx context.Context) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) Unarchive(ctx context.Context) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) IsLocked() bool {
	return false // Default implementation
}

func (r *redisRoom) Lock(ctx context.Context, reason string) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) Unlock(ctx context.Context) error {
	return streaming.ErrInvalidRoom
}

// Moderation.
func (r *redisRoom) GetModerationLog(ctx context.Context, limit int) ([]streaming.ModerationEvent, error) {
	return nil, streaming.ErrInvalidRoom
}

func (r *redisRoom) SetSlowMode(ctx context.Context, intervalSeconds int) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) GetSlowMode(ctx context.Context) int {
	return 0 // Default implementation
}

// Pinned Messages.
func (r *redisRoom) PinMessage(ctx context.Context, messageID string) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) UnpinMessage(ctx context.Context, messageID string) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) GetPinnedMessages(ctx context.Context) ([]string, error) {
	return nil, streaming.ErrInvalidRoom
}

// Categories/Tags.
func (r *redisRoom) AddTag(ctx context.Context, tag string) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) RemoveTag(ctx context.Context, tag string) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) GetTags() []string {
	return []string{} // Default implementation
}

func (r *redisRoom) SetCategory(ctx context.Context, category string) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) GetCategory() string {
	return "" // Default implementation
}

// Statistics.
func (r *redisRoom) GetMessageCount(ctx context.Context) (int64, error) {
	return 0, streaming.ErrInvalidRoom
}

func (r *redisRoom) GetActiveMembers(ctx context.Context, since time.Duration) ([]streaming.Member, error) {
	return nil, streaming.ErrInvalidRoom
}

// Messaging.
func (r *redisRoom) BroadcastExcept(ctx context.Context, message *streaming.Message, excludeUserIDs []string) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) BroadcastToRole(ctx context.Context, message *streaming.Message, role string) error {
	return streaming.ErrInvalidRoom
}

// Read Receipts.
func (r *redisRoom) MarkAsRead(ctx context.Context, userID, messageID string) error {
	return streaming.ErrInvalidRoom
}

func (r *redisRoom) GetUnreadCount(ctx context.Context, userID string, since time.Time) (int, error) {
	return 0, streaming.ErrInvalidRoom
}

func (r *redisRoom) GetLastReadMessage(ctx context.Context, userID string) (string, error) {
	return "", streaming.ErrInvalidRoom
}

// redisMember implements streaming.Member.
type redisMember struct {
	userID      string
	role        string
	joinedAt    time.Time
	permissions []string
	metadata    map[string]any
}

func (m *redisMember) GetUserID() string           { return m.userID }
func (m *redisMember) GetRole() string             { return m.role }
func (m *redisMember) GetJoinedAt() time.Time      { return m.joinedAt }
func (m *redisMember) GetPermissions() []string    { return m.permissions }
func (m *redisMember) SetRole(role string)         { m.role = role }
func (m *redisMember) GetMetadata() map[string]any { return m.metadata }
func (m *redisMember) SetMetadata(key string, val any) {
	if m.metadata == nil {
		m.metadata = make(map[string]any)
	}

	m.metadata[key] = val
}

func (m *redisMember) HasPermission(permission string) bool {
	for _, p := range m.permissions {
		if p == permission {
			return true
		}
	}

	return false
}

func (m *redisMember) GrantPermission(permission string) {
	if !m.HasPermission(permission) {
		m.permissions = append(m.permissions, permission)
	}
}

func (m *redisMember) RevokePermission(permission string) {
	for i, p := range m.permissions {
		if p == permission {
			m.permissions = append(m.permissions[:i], m.permissions[i+1:]...)

			break
		}
	}
}

// Helper functions

func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}

	return ""
}

func getMap(m map[string]interface{}, key string) map[string]any {
	if val, ok := m[key]; ok {
		if mapVal, ok := val.(map[string]interface{}); ok {
			return mapVal
		}
	}

	return make(map[string]any)
}

func getStringSlice(m map[string]interface{}, key string) []string {
	if val, ok := m[key]; ok {
		if slice, ok := val.([]interface{}); ok {
			result := make([]string, 0, len(slice))
			for _, item := range slice {
				if str, ok := item.(string); ok {
					result = append(result, str)
				}
			}

			return result
		}
	}

	return []string{}
}

// contains checks if a string contains a substring (case-insensitive).
func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// matchesFilters checks if a room matches the given filters.
func matchesFilters(room streaming.Room, filters map[string]any) bool {
	if filters == nil {
		return true
	}

	// Apply basic filters
	if private, ok := filters["private"].(bool); ok {
		if room.IsPrivate() != private {
			return false
		}
	}

	if archived, ok := filters["archived"].(bool); ok {
		if room.IsArchived() != archived {
			return false
		}
	}

	if category, ok := filters["category"].(string); ok {
		if room.GetCategory() != category {
			return false
		}
	}

	if tag, ok := filters["tag"].(string); ok {
		found := false

		for _, roomTag := range room.GetTags() {
			if roomTag == tag {
				found = true

				break
			}
		}

		if !found {
			return false
		}
	}

	return true
}
