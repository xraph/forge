package streaming

import "testing"

// TestSessionSnapshot_CarriesLastEventIDs pins the resume position: a snapshot
// records where each channel got to, so a resumption can resume rather than
// merely reconnect. Channels and DisconnectedAt alone say a session existed,
// not what it had seen. clone() must deep-copy LastEventIDs the same way it
// already deep-copies Rooms, Channels and Metadata, or two concurrent
// resumptions of one session would share (and corrupt) one map.
func TestSessionSnapshot_CarriesLastEventIDs(t *testing.T) {
	snapshot := &SessionSnapshot{
		SessionID:    "s1",
		Channels:     []string{"orders"},
		LastEventIDs: map[string]string{"orders": "epoch-42"},
	}

	clone := snapshot.clone()
	clone.LastEventIDs["orders"] = "epoch-99"

	if got := snapshot.LastEventIDs["orders"]; got != "epoch-42" {
		t.Errorf("clone must not share the map with its original: snapshot.LastEventIDs[%q] = %q, want %q", "orders", got, "epoch-42")
	}
}
