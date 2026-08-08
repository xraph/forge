package streaming

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/streaming/coordinator"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// Shared fakes and harness for the streaming root package tests.
//
// Design notes:
//
//   - The store/tracker/coordinator fakes embed their interface rather than
//     implementing all ~35-90 methods. Only the methods the manager actually
//     exercises are defined; anything else panics with a nil-interface
//     dereference, which is a loud and correct failure ("the code under test
//     called a method this fake does not model"). It also keeps the fakes
//     compiling when the production interfaces grow.
//
//   - The connection fake models forge.Connection (the transport half) and is
//     wrapped by the real NewConnection, so tests exercise the production
//     enhancedConn rather than a third re-implementation of EnhancedConnection.
//     This follows the mockConnection pattern in filters/filter_test.go and
//     validation/content_test.go, but at the transport seam so writes are
//     capturable.

// errFake is returned by fakes configured to fail.
var errFake = errors.New("fake failure")

// --- Connection fake -------------------------------------------------------

// recordingConn implements forge.Connection and captures everything written to
// it. Wrap it with NewConnection to get a full streaming.Connection.
type recordingConn struct {
	// Embedded so that additions to forge.Connection do not break the fake.
	// Every method the streaming code actually calls is defined explicitly
	// below and shadows the embedded interface.
	forge.Connection

	id string

	mu         sync.Mutex
	jsonWrites []any
	rawWrites  [][]byte
	closed     bool

	// writeErr, when set, is returned by Write/WriteJSON/WriteBinary.
	writeErr error
}

func (c *recordingConn) ID() string { return c.id }

func (c *recordingConn) Read() ([]byte, error) { return nil, nil }

func (c *recordingConn) ReadJSON(v any) error { return nil }

func (c *recordingConn) Write(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.writeErr != nil {
		return c.writeErr
	}

	cp := make([]byte, len(data))
	copy(cp, data)
	c.rawWrites = append(c.rawWrites, cp)

	return nil
}

// WriteBinary is defined even though forge.Connection may not require it, so
// the fake satisfies the interface whether or not the method is part of it.
func (c *recordingConn) WriteBinary(data []byte) error { return c.Write(data) }

func (c *recordingConn) WriteJSON(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.writeErr != nil {
		return c.writeErr
	}

	c.jsonWrites = append(c.jsonWrites, v)

	return nil
}

func (c *recordingConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true

	return nil
}

func (c *recordingConn) Context() context.Context { return context.Background() }

func (c *recordingConn) RemoteAddr() string { return "127.0.0.1:1234" }

func (c *recordingConn) LocalAddr() string { return "127.0.0.1:8080" }

// jsonWriteCount returns how many messages were delivered via WriteJSON.
func (c *recordingConn) jsonWriteCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.jsonWrites)
}

// rawWriteCount returns how many messages were delivered via Write.
func (c *recordingConn) rawWriteCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.rawWrites)
}

// totalWrites returns the combined JSON + raw delivery count.
func (c *recordingConn) totalWrites() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.jsonWrites) + len(c.rawWrites)
}

// lastJSON returns the most recent WriteJSON payload decoded as a Message.
//
// enhancedConn serialises on the caller's goroutine and replays the bytes
// through the transport as a json.RawMessage, so what reaches the transport is
// encoded JSON rather than the original *Message.
func (c *recordingConn) lastJSON(t *testing.T) *Message {
	t.Helper()

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.jsonWrites) == 0 {
		t.Fatalf("conn %s: no JSON writes recorded", c.id)
	}

	last := c.jsonWrites[len(c.jsonWrites)-1]

	var raw []byte

	switch v := last.(type) {
	case json.RawMessage:
		raw = v
	case []byte:
		raw = v
	case *Message:
		return v
	default:
		t.Fatalf("conn %s: last JSON write is %T, want JSON bytes or *Message", c.id, last)
	}

	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("conn %s: last JSON write is not decodable: %v (%s)", c.id, err, raw)
	}

	return &msg
}

// lastRaw returns the most recent Write payload.
func (c *recordingConn) lastRaw(t *testing.T) []byte {
	t.Helper()

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.rawWrites) == 0 {
		t.Fatalf("conn %s: no raw writes recorded", c.id)
	}

	return c.rawWrites[len(c.rawWrites)-1]
}

// testConn pairs an enhanced connection with the recorder underneath it.
type testConn struct {
	Connection

	rec *recordingConn
}

// newTestConn builds a real enhancedConn over a recording transport.
func newTestConn(id, userID string) *testConn {
	rec := &recordingConn{id: id}
	conn := NewConnection(rec)
	conn.SetUserID(userID)

	return &testConn{Connection: conn, rec: rec}
}

// --- Member fake -----------------------------------------------------------

type fakeMember struct {
	userID string
	role   string
}

func (m *fakeMember) GetUserID() string                    { return m.userID }
func (m *fakeMember) GetRole() string                      { return m.role }
func (m *fakeMember) GetJoinedAt() time.Time               { return time.Time{} }
func (m *fakeMember) GetPermissions() []string             { return nil }
func (m *fakeMember) SetRole(role string)                  { m.role = role }
func (m *fakeMember) HasPermission(permission string) bool { return false }
func (m *fakeMember) GrantPermission(permission string)    {}
func (m *fakeMember) RevokePermission(permission string)   {}
func (m *fakeMember) GetMetadata() map[string]any          { return nil }
func (m *fakeMember) SetMetadata(key string, value any)    {}

// --- Room fake -------------------------------------------------------------

// fakeRoom implements the parts of streaming.Room the manager touches.
type fakeRoom struct {
	streaming.Room

	id      string
	name    string
	owner   string
	private bool
	created time.Time
}

func (r *fakeRoom) GetID() string               { return r.id }
func (r *fakeRoom) GetName() string             { return r.name }
func (r *fakeRoom) GetDescription() string      { return "" }
func (r *fakeRoom) GetOwner() string            { return r.owner }
func (r *fakeRoom) GetCreated() time.Time       { return r.created }
func (r *fakeRoom) GetUpdated() time.Time       { return r.created }
func (r *fakeRoom) GetMetadata() map[string]any { return nil }
func (r *fakeRoom) IsPrivate() bool             { return r.private }
func (r *fakeRoom) IsArchived() bool            { return false }

func newFakeRoom(id string) *fakeRoom {
	return &fakeRoom{id: id, name: id, owner: "owner", created: time.Now()}
}

// --- Channel fake ----------------------------------------------------------

type fakeChannel struct {
	streaming.Channel

	id   string
	name string
}

func (c *fakeChannel) GetID() string          { return c.id }
func (c *fakeChannel) GetName() string        { return c.name }
func (c *fakeChannel) GetCreated() time.Time  { return time.Time{} }
func (c *fakeChannel) GetMessageCount() int64 { return 0 }

func newFakeChannel(id string) *fakeChannel {
	return &fakeChannel{id: id, name: id}
}

// --- RoomStore fake --------------------------------------------------------

type fakeRoomStore struct {
	streaming.RoomStore

	mu        sync.Mutex
	rooms     map[string]streaming.Room
	members   map[string][]streaming.Member // roomID -> members
	userRooms map[string][]string           // userID -> roomIDs

	createErr       error
	getErr          error
	deleteErr       error
	getUserRoomsErr error

	addMemberCalls    []addMemberCall
	removeMemberCalls []removeMemberCall
	connectCalls      int
	disconnectCalls   int
}

type addMemberCall struct {
	roomID string
	userID string
}

type removeMemberCall struct {
	roomID string
	userID string
}

func newFakeRoomStore() *fakeRoomStore {
	return &fakeRoomStore{
		rooms:     make(map[string]streaming.Room),
		members:   make(map[string][]streaming.Member),
		userRooms: make(map[string][]string),
	}
}

func (s *fakeRoomStore) Create(ctx context.Context, room streaming.Room) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.createErr != nil {
		return s.createErr
	}

	s.rooms[room.GetID()] = room

	return nil
}

func (s *fakeRoomStore) Get(ctx context.Context, roomID string) (streaming.Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.getErr != nil {
		return nil, s.getErr
	}

	room, ok := s.rooms[roomID]
	if !ok {
		return nil, ErrRoomNotFound
	}

	return room, nil
}

func (s *fakeRoomStore) Update(ctx context.Context, roomID string, updates map[string]any) error {
	return nil
}

func (s *fakeRoomStore) Delete(ctx context.Context, roomID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deleteErr != nil {
		return s.deleteErr
	}

	delete(s.rooms, roomID)

	return nil
}

func (s *fakeRoomStore) List(ctx context.Context, filters map[string]any) ([]streaming.Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]streaming.Room, 0, len(s.rooms))
	for _, r := range s.rooms {
		out = append(out, r)
	}

	return out, nil
}

func (s *fakeRoomStore) Exists(ctx context.Context, roomID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.rooms[roomID]

	return ok, nil
}

func (s *fakeRoomStore) AddMember(ctx context.Context, roomID string, member streaming.Member) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.addMemberCalls = append(s.addMemberCalls, addMemberCall{roomID: roomID, userID: member.GetUserID()})
	s.members[roomID] = append(s.members[roomID], member)
	s.userRooms[member.GetUserID()] = append(s.userRooms[member.GetUserID()], roomID)

	return nil
}

func (s *fakeRoomStore) RemoveMember(ctx context.Context, roomID, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.removeMemberCalls = append(s.removeMemberCalls, removeMemberCall{roomID: roomID, userID: userID})

	kept := s.members[roomID][:0]

	for _, m := range s.members[roomID] {
		if m.GetUserID() != userID {
			kept = append(kept, m)
		}
	}

	s.members[roomID] = kept
	s.userRooms[userID] = removeFromSlice(s.userRooms[userID], roomID)

	return nil
}

func (s *fakeRoomStore) GetMembers(ctx context.Context, roomID string) ([]streaming.Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]streaming.Member, len(s.members[roomID]))
	copy(out, s.members[roomID])

	return out, nil
}

func (s *fakeRoomStore) IsMember(ctx context.Context, roomID, userID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, m := range s.members[roomID] {
		if m.GetUserID() == userID {
			return true, nil
		}
	}

	return false, nil
}

func (s *fakeRoomStore) MemberCount(ctx context.Context, roomID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.members[roomID]), nil
}

func (s *fakeRoomStore) GetUserRooms(ctx context.Context, userID string) ([]streaming.Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.getUserRoomsErr != nil {
		return nil, s.getUserRoomsErr
	}

	ids := s.userRooms[userID]
	out := make([]streaming.Room, 0, len(ids))

	for _, id := range ids {
		if r, ok := s.rooms[id]; ok {
			out = append(out, r)
		} else {
			out = append(out, newFakeRoom(id))
		}
	}

	return out, nil
}

func (s *fakeRoomStore) GetRoomCount(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.rooms), nil
}

func (s *fakeRoomStore) Connect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connectCalls++

	return nil
}

func (s *fakeRoomStore) Disconnect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.disconnectCalls++

	return nil
}

func (s *fakeRoomStore) Ping(ctx context.Context) error { return nil }

// seedUserRooms pre-populates the user->rooms index without going through
// AddMember, so tests can drive the MaxRoomsPerUser check directly.
func (s *fakeRoomStore) seedUserRooms(userID string, roomIDs ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.userRooms[userID] = append(s.userRooms[userID], roomIDs...)
}

// addMemberCallCount reports how many times AddMember was invoked.
func (s *fakeRoomStore) addMemberCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.addMemberCalls)
}

// --- ChannelStore fake -----------------------------------------------------

type fakeChannelStore struct {
	streaming.ChannelStore

	mu           sync.Mutex
	channels     map[string]streaming.Channel
	userChannels map[string][]string
	subs         map[string][]streaming.Subscription

	createErr error
	getErr    error
	addSubErr error

	connectCalls    int
	disconnectCalls int
}

func newFakeChannelStore() *fakeChannelStore {
	return &fakeChannelStore{
		channels:     make(map[string]streaming.Channel),
		userChannels: make(map[string][]string),
		subs:         make(map[string][]streaming.Subscription),
	}
}

func (s *fakeChannelStore) Create(ctx context.Context, channel streaming.Channel) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.createErr != nil {
		return s.createErr
	}

	s.channels[channel.GetID()] = channel

	return nil
}

func (s *fakeChannelStore) Get(ctx context.Context, channelID string) (streaming.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.getErr != nil {
		return nil, s.getErr
	}

	ch, ok := s.channels[channelID]
	if !ok {
		return nil, ErrChannelNotFound
	}

	return ch, nil
}

func (s *fakeChannelStore) Delete(ctx context.Context, channelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.channels, channelID)

	return nil
}

func (s *fakeChannelStore) List(ctx context.Context) ([]streaming.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]streaming.Channel, 0, len(s.channels))
	for _, c := range s.channels {
		out = append(out, c)
	}

	return out, nil
}

func (s *fakeChannelStore) Exists(ctx context.Context, channelID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.channels[channelID]

	return ok, nil
}

func (s *fakeChannelStore) AddSubscription(ctx context.Context, channelID string, sub streaming.Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.addSubErr != nil {
		return s.addSubErr
	}

	s.subs[channelID] = append(s.subs[channelID], sub)

	userID := sub.GetUserID()
	for _, existing := range s.userChannels[userID] {
		if existing == channelID {
			return nil
		}
	}

	s.userChannels[userID] = append(s.userChannels[userID], channelID)

	return nil
}

func (s *fakeChannelStore) RemoveSubscription(ctx context.Context, channelID, connID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	kept := s.subs[channelID][:0]

	var removedUser string

	for _, sub := range s.subs[channelID] {
		if sub.GetConnID() == connID {
			removedUser = sub.GetUserID()

			continue
		}

		kept = append(kept, sub)
	}

	s.subs[channelID] = kept

	if removedUser != "" {
		s.userChannels[removedUser] = removeFromSlice(s.userChannels[removedUser], channelID)
	}

	return nil
}

func (s *fakeChannelStore) GetSubscriptions(ctx context.Context, channelID string) ([]streaming.Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]streaming.Subscription, len(s.subs[channelID]))
	copy(out, s.subs[channelID])

	return out, nil
}

func (s *fakeChannelStore) GetSubscriberCount(ctx context.Context, channelID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.subs[channelID]), nil
}

func (s *fakeChannelStore) IsSubscribed(ctx context.Context, channelID, connID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, sub := range s.subs[channelID] {
		if sub.GetConnID() == connID {
			return true, nil
		}
	}

	return false, nil
}

func (s *fakeChannelStore) GetUserChannels(ctx context.Context, userID string) ([]streaming.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ids := s.userChannels[userID]
	out := make([]streaming.Channel, 0, len(ids))

	for _, id := range ids {
		out = append(out, newFakeChannel(id))
	}

	return out, nil
}

func (s *fakeChannelStore) Connect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connectCalls++

	return nil
}

func (s *fakeChannelStore) Disconnect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.disconnectCalls++

	return nil
}

func (s *fakeChannelStore) Ping(ctx context.Context) error { return nil }

// seedUserChannels pre-populates the user->channels index so tests can drive
// the MaxChannelsPerUser check directly.
func (s *fakeChannelStore) seedUserChannels(userID string, channelIDs ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.userChannels[userID] = append(s.userChannels[userID], channelIDs...)
}

// --- MessageStore fake -----------------------------------------------------

type fakeMessageStore struct {
	roomSeq map[string]int64

	streaming.MessageStore

	mu       sync.Mutex
	saved    []*Message
	saveErr  error
	history  []*Message
	getByID  map[string]*Message
	deleted  []string
	connects int
}

func newFakeMessageStore() *fakeMessageStore {
	return &fakeMessageStore{getByID: make(map[string]*Message)}
}

func (s *fakeMessageStore) Save(ctx context.Context, message *Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.saveErr != nil {
		return s.saveErr
	}

	// Assign a per-room sequence when the caller did not, mirroring the real
	// stores. Replay is meaningless against messages that have none.
	if message.RoomID != "" && message.Sequence == 0 {
		if s.roomSeq == nil {
			s.roomSeq = make(map[string]int64)
		}

		s.roomSeq[message.RoomID]++
		message.Sequence = s.roomSeq[message.RoomID]
	}

	s.saved = append(s.saved, message)

	return nil
}

// GetSince returns a room's messages after a sequence, oldest first.
func (s *fakeMessageStore) GetSince(
	ctx context.Context,
	roomID string,
	afterSequence int64,
	limit int,
) ([]*Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []*Message

	for _, msg := range s.saved {
		if msg.RoomID == roomID && msg.Sequence > afterSequence {
			out = append(out, msg)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}

func (s *fakeMessageStore) Get(ctx context.Context, messageID string) (*Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	msg, ok := s.getByID[messageID]
	if !ok {
		return nil, ErrMessageNotFound
	}

	return msg, nil
}

func (s *fakeMessageStore) Delete(ctx context.Context, messageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.deleted = append(s.deleted, messageID)

	return nil
}

func (s *fakeMessageStore) GetHistory(ctx context.Context, roomID string, query streaming.HistoryQuery) ([]*Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]*Message, len(s.history))
	copy(out, s.history)

	return out, nil
}

func (s *fakeMessageStore) Connect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connects++

	return nil
}

func (s *fakeMessageStore) Disconnect(ctx context.Context) error { return nil }

func (s *fakeMessageStore) Ping(ctx context.Context) error { return nil }

// savedCount reports how many messages were persisted.
func (s *fakeMessageStore) savedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.saved)
}

// --- PresenceTracker fake --------------------------------------------------

type fakePresenceTracker struct {
	streaming.PresenceTracker

	mu        sync.Mutex
	presences map[string]*UserPresence
	online    []string
	inRoom    map[string][]string

	setCalls      []presenceCall
	activityCalls []string
	started       bool
	stopped       bool
	setErr        error
}

type presenceCall struct {
	userID string
	status string
}

func newFakePresenceTracker() *fakePresenceTracker {
	return &fakePresenceTracker{
		presences: make(map[string]*UserPresence),
		inRoom:    make(map[string][]string),
	}
}

func (p *fakePresenceTracker) SetPresence(ctx context.Context, userID, status string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.setErr != nil {
		return p.setErr
	}

	p.setCalls = append(p.setCalls, presenceCall{userID: userID, status: status})
	p.presences[userID] = &UserPresence{UserID: userID, Status: status, LastSeen: time.Now()}

	return nil
}

func (p *fakePresenceTracker) GetPresence(ctx context.Context, userID string) (*UserPresence, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	pr, ok := p.presences[userID]
	if !ok {
		return nil, ErrPresenceNotFound
	}

	return pr, nil
}

func (p *fakePresenceTracker) GetPresences(ctx context.Context, userIDs []string) ([]*UserPresence, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]*UserPresence, 0, len(userIDs))

	for _, id := range userIDs {
		if pr, ok := p.presences[id]; ok {
			out = append(out, pr)
		}
	}

	return out, nil
}

func (p *fakePresenceTracker) TrackActivity(ctx context.Context, userID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.activityCalls = append(p.activityCalls, userID)

	return nil
}

func (p *fakePresenceTracker) GetOnlineUsers(ctx context.Context) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]string, len(p.online))
	copy(out, p.online)

	return out, nil
}

func (p *fakePresenceTracker) GetOnlineUsersInRoom(ctx context.Context, roomID string) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]string, len(p.inRoom[roomID]))
	copy(out, p.inRoom[roomID])

	return out, nil
}

func (p *fakePresenceTracker) SetCustomStatus(ctx context.Context, userID, customStatus string) error {
	return nil
}

func (p *fakePresenceTracker) BroadcastPresence(ctx context.Context, roomID, userID, status string) error {
	return nil
}

func (p *fakePresenceTracker) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.started = true

	return nil
}

func (p *fakePresenceTracker) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.stopped = true

	return nil
}

// setPresenceCalls returns a copy of the recorded SetPresence calls.
func (p *fakePresenceTracker) setPresenceCalls() []presenceCall {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]presenceCall, len(p.setCalls))
	copy(out, p.setCalls)

	return out
}

// --- TypingTracker fake ----------------------------------------------------

type fakeTypingTracker struct {
	streaming.TypingTracker

	mu       sync.Mutex
	starts   []typingCall
	stops    []typingCall
	byRoom   map[string][]string
	started  bool
	stopped  bool
	startErr error
}

type typingCall struct {
	userID string
	roomID string
}

func newFakeTypingTracker() *fakeTypingTracker {
	return &fakeTypingTracker{byRoom: make(map[string][]string)}
}

func (tt *fakeTypingTracker) StartTyping(ctx context.Context, userID, roomID string) error {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	if tt.startErr != nil {
		return tt.startErr
	}

	tt.starts = append(tt.starts, typingCall{userID: userID, roomID: roomID})
	tt.byRoom[roomID] = append(tt.byRoom[roomID], userID)

	return nil
}

func (tt *fakeTypingTracker) StopTyping(ctx context.Context, userID, roomID string) error {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	tt.stops = append(tt.stops, typingCall{userID: userID, roomID: roomID})
	tt.byRoom[roomID] = removeFromSlice(tt.byRoom[roomID], userID)

	return nil
}

func (tt *fakeTypingTracker) GetTypingUsers(ctx context.Context, roomID string) ([]string, error) {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	out := make([]string, len(tt.byRoom[roomID]))
	copy(out, tt.byRoom[roomID])

	return out, nil
}

func (tt *fakeTypingTracker) IsTyping(ctx context.Context, userID, roomID string) (bool, error) {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	for _, u := range tt.byRoom[roomID] {
		if u == userID {
			return true, nil
		}
	}

	return false, nil
}

func (tt *fakeTypingTracker) Start(ctx context.Context) error {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	tt.started = true

	return nil
}

func (tt *fakeTypingTracker) Stop(ctx context.Context) error {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	tt.stopped = true

	return nil
}

// startTypingCalls returns a copy of the recorded StartTyping calls.
func (tt *fakeTypingTracker) startTypingCalls() []typingCall {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	out := make([]typingCall, len(tt.starts))
	copy(out, tt.starts)

	return out
}

// stopTypingCalls returns a copy of the recorded StopTyping calls.
func (tt *fakeTypingTracker) stopTypingCalls() []typingCall {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	out := make([]typingCall, len(tt.stops))
	copy(out, tt.stops)

	return out
}

// --- Coordinator fake ------------------------------------------------------

// fakeCoordinator implements coordinator.StreamCoordinator and records relays.
type fakeCoordinator struct {
	mu sync.Mutex

	globalBroadcasts []*Message
	roomBroadcasts   []coordTarget
	userBroadcasts   []coordTarget
	nodeBroadcasts   []coordTarget

	handler       coordinator.MessageHandler
	registered    []string
	unregistered  []string
	started       bool
	stopped       bool
	broadcastErr  error
	userNodes     map[string][]string
	roomNodes     map[string][]string
	trackedUsers  []coordTarget
	untrackedUser []coordTarget
}

type coordTarget struct {
	id  string
	msg *Message
}

func newFakeCoordinator() *fakeCoordinator {
	return &fakeCoordinator{
		userNodes: make(map[string][]string),
		roomNodes: make(map[string][]string),
	}
}

func (c *fakeCoordinator) BroadcastToNode(ctx context.Context, nodeID string, msg *Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.nodeBroadcasts = append(c.nodeBroadcasts, coordTarget{id: nodeID, msg: msg})

	return c.broadcastErr
}

func (c *fakeCoordinator) BroadcastToUser(ctx context.Context, userID string, msg *Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.userBroadcasts = append(c.userBroadcasts, coordTarget{id: userID, msg: msg})

	return c.broadcastErr
}

func (c *fakeCoordinator) BroadcastToRoom(ctx context.Context, roomID string, msg *Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.roomBroadcasts = append(c.roomBroadcasts, coordTarget{id: roomID, msg: msg})

	return c.broadcastErr
}

func (c *fakeCoordinator) BroadcastGlobal(ctx context.Context, msg *Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.globalBroadcasts = append(c.globalBroadcasts, msg)

	return c.broadcastErr
}

func (c *fakeCoordinator) SyncPresence(ctx context.Context, presence *UserPresence) error { return nil }

func (c *fakeCoordinator) SyncRoomState(ctx context.Context, roomID string, state *coordinator.RoomState) error {
	return nil
}

func (c *fakeCoordinator) GetUserNodes(ctx context.Context, userID string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.userNodes[userID], nil
}

func (c *fakeCoordinator) GetRoomNodes(ctx context.Context, roomID string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.roomNodes[roomID], nil
}

func (c *fakeCoordinator) RegisterNode(ctx context.Context, nodeID string, metadata map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.registered = append(c.registered, nodeID)

	return nil
}

func (c *fakeCoordinator) UnregisterNode(ctx context.Context, nodeID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.unregistered = append(c.unregistered, nodeID)

	return nil
}

func (c *fakeCoordinator) Subscribe(ctx context.Context, handler coordinator.MessageHandler) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.handler = handler

	return nil
}

func (c *fakeCoordinator) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.started = true

	return nil
}

func (c *fakeCoordinator) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stopped = true

	return nil
}

// TrackUserNode / UntrackUserNode are looked up by the manager via an inline
// interface assertion, so they are part of the contract the fake must model.
func (c *fakeCoordinator) TrackUserNode(ctx context.Context, userID, nodeID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.trackedUsers = append(c.trackedUsers, coordTarget{id: userID})

	return nil
}

func (c *fakeCoordinator) UntrackUserNode(ctx context.Context, userID, nodeID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.untrackedUser = append(c.untrackedUser, coordTarget{id: userID})

	return nil
}

// globalBroadcastCount reports how many global relays were issued.
func (c *fakeCoordinator) globalBroadcastCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.globalBroadcasts)
}

// roomBroadcastTargets returns the room IDs relayed to other nodes.
func (c *fakeCoordinator) roomBroadcastTargets() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]string, 0, len(c.roomBroadcasts))
	for _, t := range c.roomBroadcasts {
		out = append(out, t.id)
	}

	return out
}

// userBroadcastTargets returns the user IDs relayed to other nodes.
func (c *fakeCoordinator) userBroadcastTargets() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]string, 0, len(c.userBroadcasts))
	for _, t := range c.userBroadcasts {
		out = append(out, t.id)
	}

	return out
}

// --- Manager harness -------------------------------------------------------

// harness bundles a manager with the fakes wired underneath it.
type harness struct {
	mgr      Manager
	impl     *manager
	rooms    *fakeRoomStore
	channels *fakeChannelStore
	messages *fakeMessageStore
	presence *fakePresenceTracker
	typing   *fakeTypingTracker
}

// testConfig returns a Config suitable for unit tests: all features on,
// small-but-non-trivial limits, no distributed backend.
func testConfig() Config {
	cfg := DefaultConfig()
	cfg.Backend = "local"
	cfg.EnableDistributed = false

	return cfg
}

// newTestManager builds a manager over the fakes. Pass ManagerOptions to add a
// coordinator, hooks, codecs, session store, and so on.
func newTestManager(t *testing.T, cfg Config, opts ...ManagerOption) *harness {
	t.Helper()

	h := &harness{
		rooms:    newFakeRoomStore(),
		channels: newFakeChannelStore(),
		messages: newFakeMessageStore(),
		presence: newFakePresenceTracker(),
		typing:   newFakeTypingTracker(),
	}

	h.mgr = NewManager(
		cfg,
		h.rooms,
		h.channels,
		h.messages,
		h.presence,
		h.typing,
		nil, // distributed backend
		nil, // logger
		nil, // metrics
		opts...,
	)

	impl, ok := h.mgr.(*manager)
	if !ok {
		t.Fatalf("NewManager returned %T, want *manager", h.mgr)
	}

	h.impl = impl

	return h
}

// register is a convenience wrapper that fails the test on registration error.
func (h *harness) register(t *testing.T, conns ...*testConn) {
	t.Helper()

	for _, c := range conns {
		if err := h.mgr.Register(c); err != nil {
			t.Fatalf("Register(%s): unexpected error: %v", c.ID(), err)
		}
	}
}

// join puts a connection in a room through the manager.
//
// Tests must not call conn.AddRoom directly: BroadcastToRoom resolves
// recipients from the manager's room index, which only JoinRoom populates, so a
// connection that joined behind the manager's back receives nothing.
func (h *harness) join(t *testing.T, conn *testConn, roomIDs ...string) {
	t.Helper()

	for _, roomID := range roomIDs {
		if err := h.mgr.JoinRoom(context.Background(), conn.ID(), roomID); err != nil {
			t.Fatalf("JoinRoom(%s, %s): unexpected error: %v", conn.ID(), roomID, err)
		}
	}
}

// subscribe subscribes a connection to channels through the manager, for the
// same reason join exists.
func (h *harness) subscribe(t *testing.T, conn *testConn, channelIDs ...string) {
	t.Helper()

	for _, channelID := range channelIDs {
		if err := h.mgr.Subscribe(context.Background(), conn.ID(), channelID, nil); err != nil {
			t.Fatalf("Subscribe(%s, %s): unexpected error: %v", conn.ID(), channelID, err)
		}
	}
}

// waitQuiet blocks until the connection's send queue has drained, so a test can
// read write counts without racing the writer goroutine.
func (c *testConn) waitQuiet(t *testing.T) {
	t.Helper()

	stats, ok := any(c.Connection).(interface{ SendQueueStats() SendQueueStats })
	if !ok {
		// No queue on this connection: writes are already synchronous.
		return
	}

	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		s := stats.SendQueueStats()
		if s.Depth == 0 && s.Written+s.Dropped >= s.Enqueued {
			return
		}

		time.Sleep(time.Millisecond)
	}

	s := stats.SendQueueStats()
	t.Fatalf("conn %s: send queue did not drain within 2s (%+v)", c.ID(), s)
}

// --- Assertions ------------------------------------------------------------

func assertNoError(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertErrorIs(t *testing.T, err, target error) {
	t.Helper()

	if !errors.Is(err, target) {
		t.Fatalf("got error %v, want %v", err, target)
	}
}

// awaitCount waits for a counter to settle on want, then reports it.
//
// The shared primitive behind every delivery assertion. Delivery is
// asynchronous — see assertWrites — so any test reading a write counter
// straight after a send is racing the connection's writer goroutine.
func awaitCount(t *testing.T, label string, count func() int, want int) {
	t.Helper()

	const (
		timeout   = 2 * time.Second
		settleFor = 20 * time.Millisecond
		pollEvery = time.Millisecond
	)

	deadline := time.Now().Add(timeout)

	var got int

	for time.Now().Before(deadline) {
		got = count()

		if got == want {
			// Hold briefly: a test expecting one frame must also fail on two,
			// and returning the moment the count matched would never see it.
			time.Sleep(settleFor)

			if got = count(); got == want {
				return
			}

			break
		}

		if got > want {
			break
		}

		time.Sleep(pollEvery)
	}

	t.Errorf("%s = %d, want %d", label, got, want)
}

// assertWrites waits for a connection to have received exactly want frames.
//
// Delivery is asynchronous: a connection hands frames to a bounded send queue
// drained by its own writer goroutine, so a write has been *accepted* by the
// time a broadcast returns but has not necessarily reached the socket. Asserting
// synchronously would fail whenever the writer had not been scheduled yet — a
// flake that appears under load and vanishes under a debugger.
//
// Polls to the expected count and then holds briefly to catch overshoot: a test
// expecting one frame should also fail if two arrive, and returning the instant
// the count matched would never see the second.
func assertWrites(t *testing.T, conn *testConn, want int) {
	t.Helper()

	const (
		timeout   = 2 * time.Second
		settleFor = 20 * time.Millisecond
		pollEvery = time.Millisecond
	)

	deadline := time.Now().Add(timeout)

	var got int

	for time.Now().Before(deadline) {
		if got = conn.rec.totalWrites(); got == want {
			time.Sleep(settleFor)

			if got = conn.rec.totalWrites(); got == want {
				return
			}

			break
		}

		if got > want {
			break
		}

		time.Sleep(pollEvery)
	}

	t.Errorf("conn %s: got %d writes, want %d", conn.ID(), got, want)
}
