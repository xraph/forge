package filters

import (
	"context"
	"slices"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// MetadataFilterConfig configures metadata-based filtering.
type MetadataFilterConfig struct {
	// Filter by room/channel
	FilterByRoom    bool
	FilterByChannel bool

	// Filter by message type
	FilterByType bool
	AllowedTypes []string
	BlockedTypes []string

	// Custom conditions
	CustomConditions map[string]FilterCondition
}

// FilterCondition defines a custom filter condition.
type FilterCondition func(ctx context.Context, msg *streaming.Message, recipient streaming.EnhancedConnection) bool

// metadataFilter filters based on message metadata.
type metadataFilter struct {
	config MetadataFilterConfig
}

// NewMetadataFilter creates a metadata filter.
func NewMetadataFilter(config MetadataFilterConfig) MessageFilter {
	return &metadataFilter{
		config: config,
	}
}

func (mf *metadataFilter) Name() string {
	return "metadata_filter"
}

func (mf *metadataFilter) Priority() int {
	return 20 // After content filter
}

func (mf *metadataFilter) Filter(ctx context.Context, msg *streaming.Message, recipient streaming.EnhancedConnection) (*streaming.Message, error) {
	// Filter by room
	if mf.config.FilterByRoom && msg.RoomID != "" {
		// Check if recipient is in room
		if !recipient.IsInRoom(msg.RoomID) {
			return nil, nil //nolint:nilnil // MessageFilter contract: nil, nil blocks the message (recipient not in room)
		}
	}

	// Filter by channel
	if mf.config.FilterByChannel && msg.ChannelID != "" {
		// Check if recipient is subscribed to channel
		if !recipient.IsSubscribed(msg.ChannelID) {
			return nil, nil //nolint:nilnil // MessageFilter contract: nil, nil blocks the message (recipient not subscribed)
		}
	}

	// Filter by message type
	if mf.config.FilterByType {
		// Check blocked types
		if slices.Contains(mf.config.BlockedTypes, msg.Type) {
			return nil, nil //nolint:nilnil // MessageFilter contract: nil, nil blocks the message (blocked type)
		}

		// Check allowed types
		if len(mf.config.AllowedTypes) > 0 {
			allowed := slices.Contains(mf.config.AllowedTypes, msg.Type)

			if !allowed {
				return nil, nil //nolint:nilnil // MessageFilter contract: nil, nil blocks the message (type not allowed)
			}
		}
	}

	// Apply custom conditions
	for name, condition := range mf.config.CustomConditions {
		if !condition(ctx, msg, recipient) {
			// Add debug info to context
			if msg.Metadata == nil {
				msg.Metadata = make(map[string]any)
			}

			msg.Metadata["filtered_by"] = name

			return nil, nil //nolint:nilnil // MessageFilter contract: nil, nil blocks the message (custom condition failed)
		}
	}

	return msg, nil
}
