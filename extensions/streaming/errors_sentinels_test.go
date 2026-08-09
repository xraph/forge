package streaming

import (
	"errors"
	"fmt"
	"testing"

	"github.com/xraph/forge/extensions/streaming/internal"
)

// TestFeatureDisabledSentinels pins D4: feature-flag rejections were untyped
// errors.New values, so clients had no way to branch on them.
func TestFeatureDisabledSentinels(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		internal error
		want     string
	}{
		{name: "rooms", err: ErrRoomsDisabled, internal: internal.ErrRoomsDisabled, want: "rooms are disabled"},
		{name: "channels", err: ErrChannelsDisabled, internal: internal.ErrChannelsDisabled, want: "channels are disabled"},
		{name: "history", err: ErrHistoryDisabled, internal: internal.ErrHistoryDisabled, want: "message history is disabled"},
		{name: "presence", err: ErrPresenceDisabled, internal: internal.ErrPresenceDisabled, want: "presence tracking is disabled"},
		{name: "typing", err: ErrTypingDisabled, internal: internal.ErrTypingDisabled, want: "typing indicators are disabled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Fatal("sentinel is nil")
			}

			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}

			// The public sentinel must be the same value the manager returns.
			if !errors.Is(tt.err, tt.internal) {
				t.Errorf("public sentinel is not the internal one (%v vs %v)", tt.err, tt.internal)
			}

			// Clients must be able to branch on a wrapped rejection.
			wrapped := fmt.Errorf("join room: %w", tt.err)
			if !errors.Is(wrapped, tt.err) {
				t.Errorf("errors.Is(wrapped, sentinel) = false, want true")
			}
		})
	}
}

func TestFeatureDisabledSentinelsAreDistinct(t *testing.T) {
	sentinels := map[string]error{
		"rooms":    ErrRoomsDisabled,
		"channels": ErrChannelsDisabled,
		"history":  ErrHistoryDisabled,
		"presence": ErrPresenceDisabled,
		"typing":   ErrTypingDisabled,
	}

	for nameA, errA := range sentinels {
		for nameB, errB := range sentinels {
			if nameA == nameB {
				continue
			}

			if errors.Is(errA, errB) {
				t.Errorf("errors.Is(%s, %s) = true, want false (sentinels must be distinguishable)", nameA, nameB)
			}
		}
	}
}
