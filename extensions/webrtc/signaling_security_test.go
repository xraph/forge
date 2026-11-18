package webrtc

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/forge/extensions/streaming"
)

func newSignalingTestExt(t *testing.T, opts ...ConfigOption) *Extension {
	t.Helper()

	streamingExt := streaming.NewExtension(streaming.WithLocalBackend())

	app := newMockApp()
	if err := streamingExt.Register(app); err != nil {
		t.Fatalf("register streaming: %v", err)
	}

	ext, err := New(streamingExt, DefaultConfig(), opts...)
	if err != nil {
		t.Fatalf("new webrtc: %v", err)
	}

	if err := ext.Register(app); err != nil {
		t.Fatalf("register webrtc: %v", err)
	}

	return ext
}

// Regression: the route did ctx.Get("user_id").(string) unchecked, which
// panics for an anonymous caller.
func TestSignalingUserID_RejectsAnonymous(t *testing.T) {
	for _, v := range []any{nil, 42, ""} {
		if _, ok := signalingUserID(v); ok {
			t.Errorf("signalingUserID(%#v) accepted an anonymous caller", v)
		}
	}

	if id, ok := signalingUserID("alice"); !ok || id != "alice" {
		t.Errorf("signalingUserID(alice) = %q, %v", id, ok)
	}
}

// Regression: joining by URL had no authorization at all.
func TestJoinFromSignaling_RequiresAuthorizer(t *testing.T) {
	ext := newSignalingTestExt(t)

	if _, _, err := ext.joinFromSignaling(context.Background(), "room-1", "alice"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("join without an authorizer: err = %v, want ErrUnauthorized", err)
	}

	if _, err := ext.GetCallRoom("room-1"); err == nil {
		t.Error("refused join still created the room")
	}
}

func TestJoinFromSignaling_DeniedUserCannotJoinOrCreate(t *testing.T) {
	ext := newSignalingTestExt(t, WithRoomAuthorizer(RoomAuthorizerFunc(func(_ context.Context, userID, _ string) (bool, error) {
		return userID == "alice", nil
	})))

	if _, _, err := ext.joinFromSignaling(context.Background(), "room-1", "mallory"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("denied user: err = %v, want ErrUnauthorized", err)
	}

	if _, err := ext.GetCallRoom("room-1"); err == nil {
		t.Error("denied user created the room")
	}

	authErr := errors.New("policy store down")
	failing := newSignalingTestExt(t, WithRoomAuthorizer(RoomAuthorizerFunc(func(context.Context, string, string) (bool, error) {
		return true, authErr
	})))

	if _, _, err := failing.joinFromSignaling(context.Background(), "room-1", "alice"); !errors.Is(err, authErr) {
		t.Fatalf("authorizer error: err = %v, want it surfaced", err)
	}
}

// Regression: auto-create called CreateCallRoom without opts.ID, so it
// always failed with "room ID is required" and no room was ever created
// from the signaling endpoint.
func TestJoinFromSignaling_AutoCreatesRoom(t *testing.T) {
	ext := newSignalingTestExt(t, WithRoomAuthorizer(AllowAllRooms))
	ctx := context.Background()

	room, peer, err := ext.joinFromSignaling(ctx, "room-1", "alice")
	if err != nil {
		t.Fatalf("first join: %v", err)
	}

	if peer == nil {
		t.Fatal("no peer returned")
	}

	if got, err := ext.GetCallRoom("room-1"); err != nil || got != room {
		t.Fatalf("room-1 not registered by the join: %v", err)
	}

	// The second caller finds the room the first one created.
	room2, _, err := ext.joinFromSignaling(ctx, "room-1", "bob")
	if err != nil {
		t.Fatalf("second join: %v", err)
	}

	if room2 != room {
		t.Error("second join got a different room")
	}

	if n := len(room.GetPeers()); n != 2 {
		t.Errorf("room has %d peers, want 2", n)
	}
}
