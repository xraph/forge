package streaming

import (
	"github.com/xraph/forge/pkg/common"
	streamingcore "github.com/xraph/forge/pkg/streaming/core"
)

// UserPresence represents a user's presence information
type UserPresence = streamingcore.UserPresence

// PresenceCallback is called when presence changes
type PresenceCallback = streamingcore.PresenceCallback

// PresenceEvent represents presence change events
type PresenceEvent = streamingcore.PresenceEvent

const (
	PresenceEventJoin   = streamingcore.PresenceEventJoin
	PresenceEventLeave  = streamingcore.PresenceEventLeave
	PresenceEventUpdate = streamingcore.PresenceEventUpdate
	PresenceEventExpire = streamingcore.PresenceEventExpire
)

// PresenceTracker manages user presence information
type PresenceTracker = streamingcore.PresenceTracker

// PresenceStats represents presence tracker statistics
type PresenceStats = streamingcore.PresenceStats

// PresenceConfig contains configuration for presence tracking
type PresenceConfig = streamingcore.PresenceConfig

// DefaultPresenceConfig returns default presence configuration
func DefaultPresenceConfig() PresenceConfig {
	return streamingcore.DefaultPresenceConfig()
}

// LocalPresenceTracker implements in-memory presence tracking
type LocalPresenceTracker = streamingcore.LocalPresenceTracker

// NewLocalPresenceTracker creates a new local presence tracker
func NewLocalPresenceTracker(config PresenceConfig, logger common.Logger, metrics common.Metrics) PresenceTracker {
	return streamingcore.NewLocalPresenceTracker(config, logger, metrics)
}

// UserPresenceFromJSON FromJSON creates a UserPresence from JSON
func UserPresenceFromJSON(data []byte) (*UserPresence, error) {
	return streamingcore.UserPresenceFromJSON(data)
}
