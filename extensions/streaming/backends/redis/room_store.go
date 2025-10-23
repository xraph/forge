package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
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
	if err == redis.Nil {
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
	if err == redis.Nil {
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

// redisRoom implements streaming.Room
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

// redisMember implements streaming.Member
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
