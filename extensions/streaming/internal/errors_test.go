package internal

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorWrappersUnwrap(t *testing.T) {
	sentinel := errors.New("underlying")

	tests := []struct {
		name string
		err  error
	}{
		{name: "connection", err: NewConnectionError("c1", "send", sentinel)},
		{name: "room", err: NewRoomError("r1", "u1", "join", sentinel)},
		{name: "room without user", err: NewRoomError("r1", "", "join", sentinel)},
		{name: "channel", err: NewChannelError("ch1", "subscribe", sentinel)},
		{name: "message", err: NewMessageError("m1", "save", sentinel)},
		{name: "backend", err: NewBackendError("redis", "ping", sentinel)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(tt.err, sentinel) {
				t.Errorf("errors.Is(%v, sentinel) = false, want true", tt.err)
			}

			if errors.Unwrap(tt.err) != sentinel {
				t.Errorf("Unwrap = %v, want the underlying error", errors.Unwrap(tt.err))
			}
		})
	}
}

func TestErrorWrappersMessages(t *testing.T) {
	sentinel := errors.New("boom")

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "connection error names the connection and op",
			err:  NewConnectionError("c1", "send", sentinel),
			want: "connection c1: send: boom",
		},
		{
			name: "room error includes the user when set",
			err:  NewRoomError("r1", "u1", "join", sentinel),
			want: "room r1 (user u1): join: boom",
		},
		{
			name: "room error omits the user when empty",
			err:  NewRoomError("r1", "", "join", sentinel),
			want: "room r1: join: boom",
		},
		{
			name: "channel error",
			err:  NewChannelError("ch1", "subscribe", sentinel),
			want: "channel ch1: subscribe: boom",
		},
		{
			name: "message error",
			err:  NewMessageError("m1", "save", sentinel),
			want: "message m1: save: boom",
		},
		{
			name: "backend error",
			err:  NewBackendError("redis", "ping", sentinel),
			want: "backend redis: ping: boom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestErrorWrappersSupportErrorsAs(t *testing.T) {
	sentinel := errors.New("boom")

	t.Run("connection", func(t *testing.T) {
		wrapped := fmt.Errorf("outer: %w", NewConnectionError("c1", "send", sentinel))

		var target *ConnectionError
		if !errors.As(wrapped, &target) {
			t.Fatal("errors.As did not find a *ConnectionError")
		}

		if target.ConnID != "c1" || target.Op != "send" {
			t.Errorf("recovered %+v, want ConnID=c1 Op=send", target)
		}
	})

	t.Run("room", func(t *testing.T) {
		wrapped := fmt.Errorf("outer: %w", NewRoomError("r1", "u1", "join", sentinel))

		var target *RoomError
		if !errors.As(wrapped, &target) {
			t.Fatal("errors.As did not find a *RoomError")
		}

		if target.RoomID != "r1" || target.UserID != "u1" {
			t.Errorf("recovered %+v, want RoomID=r1 UserID=u1", target)
		}
	})

	t.Run("backend", func(t *testing.T) {
		wrapped := fmt.Errorf("outer: %w", NewBackendError("redis", "ping", sentinel))

		var target *BackendError
		if !errors.As(wrapped, &target) {
			t.Fatal("errors.As did not find a *BackendError")
		}

		if target.Backend != "redis" {
			t.Errorf("recovered %+v, want Backend=redis", target)
		}
	})
}

func TestSentinelErrorsAreDistinct(t *testing.T) {
	// Sentinels are compared with errors.Is across the extension, so two of them
	// must never be the same value — otherwise a caller branching on, say,
	// ErrRoomNotFound would also catch ErrChannelNotFound.
	sentinels := map[string]error{
		"ErrConnectionNotFound":     ErrConnectionNotFound,
		"ErrConnectionClosed":       ErrConnectionClosed,
		"ErrConnectionLimitReached": ErrConnectionLimitReached,
		"ErrInvalidConnection":      ErrInvalidConnection,
		"ErrRoomNotFound":           ErrRoomNotFound,
		"ErrRoomAlreadyExists":      ErrRoomAlreadyExists,
		"ErrRoomFull":               ErrRoomFull,
		"ErrNotRoomMember":          ErrNotRoomMember,
		"ErrAlreadyRoomMember":      ErrAlreadyRoomMember,
		"ErrRoomLimitReached":       ErrRoomLimitReached,
		"ErrChannelNotFound":        ErrChannelNotFound,
		"ErrChannelAlreadyExists":   ErrChannelAlreadyExists,
		"ErrNotSubscribed":          ErrNotSubscribed,
		"ErrAlreadySubscribed":      ErrAlreadySubscribed,
		"ErrChannelLimitReached":    ErrChannelLimitReached,
		"ErrPermissionDenied":       ErrPermissionDenied,
		"ErrMessageTooLarge":        ErrMessageTooLarge,
		"ErrMessageNotFound":        ErrMessageNotFound,
		"ErrPresenceNotFound":       ErrPresenceNotFound,
		"ErrInvalidStatus":          ErrInvalidStatus,
		"ErrInviteNotFound":         ErrInviteNotFound,
		"ErrInviteExpired":          ErrInviteExpired,
		"ErrBackendNotConnected":    ErrBackendNotConnected,
		"ErrInvalidConfig":          ErrInvalidConfig,
	}

	for nameA, errA := range sentinels {
		for nameB, errB := range sentinels {
			if nameA == nameB {
				continue
			}

			if errors.Is(errA, errB) {
				t.Errorf("%s and %s compare equal under errors.Is", nameA, nameB)
			}
		}
	}
}

func TestWrappedSentinelsRemainMatchable(t *testing.T) {
	// The pattern used throughout the extension: wrap a sentinel with context
	// and still let callers branch on it.
	err := NewRoomError("r1", "u1", "join", ErrRoomFull)

	if !errors.Is(err, ErrRoomFull) {
		t.Error("wrapped ErrRoomFull is not matchable with errors.Is")
	}

	if errors.Is(err, ErrRoomNotFound) {
		t.Error("wrapped ErrRoomFull incorrectly matches ErrRoomNotFound")
	}
}
