package streaming

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	streamauth "github.com/xraph/forge/extensions/streaming/auth"
	"github.com/xraph/forge/extensions/streaming/backends/local"
	redisbackend "github.com/xraph/forge/extensions/streaming/backends/redis"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// muteBackends returns a manager over each room store backend.
func muteBackends(t *testing.T) map[string]func(t *testing.T) (Manager, streaming.RoomStore) {
	t.Helper()

	build := func(rooms streaming.RoomStore) Manager {
		msgs := local.NewMessageStore()
		ra := streamauth.NewRoomAuthorizer(rooms)

		return NewManager(DefaultConfig(), rooms, local.NewChannelStore(), msgs,
			newFakePresenceTracker(), newFakeTypingTracker(), nil, nil, nil,
			WithRoomAuthorizer(ra),
			WithMessageAuthorizer(streamauth.NewMessageAuthorizer(ra, newAuthMessageStore(msgs))),
		)
	}

	return map[string]func(t *testing.T) (Manager, streaming.RoomStore){
		"local": func(t *testing.T) (Manager, streaming.RoomStore) {
			t.Helper()

			rooms := local.NewRoomStore()

			return build(rooms), rooms
		},
		"redis": func(t *testing.T) (Manager, streaming.RoomStore) {
			t.Helper()

			client := goredis.NewClient(&goredis.Options{Addr: miniredis.RunT(t).Addr()})
			t.Cleanup(func() { _ = client.Close() })

			rooms := redisbackend.NewRoomStore(client, "test:rooms")

			return build(rooms), rooms
		},
	}
}

// A muted member stays in the room and keeps receiving it, but the send gate
// refuses what they send until the mute is lifted or runs out. On the Redis
// backend MuteUser used to fail outright.
func TestMuteUser_RefusesSendsOnEveryBackend(t *testing.T) {
	for name, newManager := range muteBackends(t) {
		t.Run(name, func(t *testing.T) {
			m, rooms := newManager(t)
			ctx := context.Background()

			assertNoError(t, rooms.Create(ctx, local.NewRoom(streaming.RoomOptions{ID: "lobby", Name: "lobby", Owner: "o"})))

			alice := newTestConn("a1", "alice")
			bob := newTestConn("b1", "bob")

			for _, c := range []*testConn{alice, bob} {
				assertNoError(t, m.Register(c))
				assertNoError(t, m.JoinRoom(ctx, c.ID(), "lobby"))
			}

			send := func(c *testConn) error {
				_, err := m.ProcessInbound(ctx, &Message{Type: MessageTypeMessage, RoomID: "lobby", Data: "hi"}, c)

				return err
			}

			assertNoError(t, m.MuteUser(ctx, "alice", "lobby", time.Hour))
			assertErrorIs(t, send(alice), ErrUserMuted)
			assertNoError(t, send(bob))

			if !alice.IsInRoom("lobby") {
				t.Fatal("a mute must not take the user out of the room")
			}

			assertNoError(t, m.UnmuteUser(ctx, "alice", "lobby"))
			assertNoError(t, send(alice))

			// A mute with an end runs out on its own.
			assertNoError(t, m.MuteUser(ctx, "alice", "lobby", 100*time.Millisecond))
			assertErrorIs(t, send(alice), ErrUserMuted)

			time.Sleep(150 * time.Millisecond)

			assertNoError(t, send(alice))
		})
	}
}
