package router

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventID_RoundTrip(t *testing.T) {
	s := formatEventID("7f3a9c1e", 42)
	assert.Equal(t, "7f3a9c1e-42", s)

	id, ok := parseEventID(s)
	require.True(t, ok)
	assert.Equal(t, "7f3a9c1e", id.Epoch)
	assert.Equal(t, uint64(42), id.Seq)
}

// Epochs are UUIDs, which contain dashes, so the seq must be split off the
// right-hand end rather than the first dash found.
func TestEventID_EpochContainingDashes(t *testing.T) {
	epoch := "3f2504e0-4f89-11d3-9a0c-0305e82c3301"

	id, ok := parseEventID(formatEventID(epoch, 7))
	require.True(t, ok)
	assert.Equal(t, epoch, id.Epoch)
	assert.Equal(t, uint64(7), id.Seq)
}

// A malformed id must never parse into a plausible-looking position: every one
// of these resolves to "cannot resume" rather than to seq 0.
func TestEventID_Malformed(t *testing.T) {
	for _, s := range []string{
		"",
		"noseparator",
		"epoch-",
		"-42",
		"epoch-notanumber",
		"epoch-99999999999999999999999",
	} {
		t.Run(s, func(t *testing.T) {
			_, ok := parseEventID(s)
			assert.False(t, ok)
		})
	}
}

// A dash-terminated epoch still round trips. The codec's whole job is to invert
// formatEventID, so no input formatEventID can produce may be rejected.
func TestEventID_DashTerminatedEpochRoundTrips(t *testing.T) {
	id, ok := parseEventID(formatEventID("abc-", 1))
	require.True(t, ok)
	assert.Equal(t, "abc-", id.Epoch)
	assert.Equal(t, uint64(1), id.Seq)
}
