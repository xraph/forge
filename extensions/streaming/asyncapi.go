package streaming

import (
	"github.com/xraph/forge"
)

// AsyncAPISpec generates AsyncAPI 3.0.0 specification for the streaming extension
// This documents all streaming channels, operations, and message types.
func (e *Extension) AsyncAPISpec() *forge.AsyncAPISpec {
	spec := &forge.AsyncAPISpec{
		AsyncAPI: "3.0.0",
		Info: forge.AsyncAPIInfo{
			Title:       "Streaming API",
			Description: "Real-time streaming with WebSocket support for rooms, channels, presence tracking, and typing indicators",
			Version:     e.Version(),
		},
		Channels:   make(map[string]*forge.AsyncAPIChannel),
		Operations: make(map[string]*forge.AsyncAPIOperation),
		Components: &forge.AsyncAPIComponents{
			Schemas:  make(map[string]*forge.Schema),
			Messages: make(map[string]*forge.AsyncAPIMessage),
		},
	}

	// Add channels and operations based on enabled features
	if e.config.EnableRooms {
		e.addRoomChannels(spec)
	}

	if e.config.EnableChannels {
		e.addChannelChannels(spec)
	}

	if e.config.EnablePresence {
		e.addPresenceChannels(spec)
	}

	if e.config.EnableTypingIndicators {
		e.addTypingChannels(spec)
	}

	// Add message schemas to components
	e.addMessageSchemas(spec)

	return spec
}

// addRoomChannels adds room-related channels and operations.
func (e *Extension) addRoomChannels(spec *forge.AsyncAPISpec) {
	// Room channel
	channelID := "rooms"
	spec.Channels[channelID] = &forge.AsyncAPIChannel{
		Address:     "/rooms/{roomId}",
		Title:       "Room Channel",
		Summary:     "Real-time communication within rooms",
		Description: "WebSocket channel for room-based messaging, including join/leave events and message broadcasting",
		Parameters: map[string]*forge.AsyncAPIParameter{
			"roomId": {
				Description: "Unique identifier of the room",
				Schema: &forge.Schema{
					Type: "string",
				},
			},
		},
		Messages: map[string]*forge.AsyncAPIMessage{
			"JoinRoom": {
				MessageID:   "JoinRoom",
				Name:        "JoinRoom",
				Title:       "Join Room Message",
				Summary:     "Client request to join a room",
				ContentType: "application/json",
				Payload: &forge.Schema{
					Type: "object",
					Properties: map[string]*forge.Schema{
						"type":    {Type: "string", Enum: []any{"join"}},
						"room_id": {Type: "string", Description: "Room ID to join"},
						"user_id": {Type: "string", Description: "User ID"},
					},
					Required: []string{"type", "room_id", "user_id"},
				},
			},
			"LeaveRoom": {
				MessageID:   "LeaveRoom",
				Name:        "LeaveRoom",
				Title:       "Leave Room Message",
				Summary:     "Client request to leave a room",
				ContentType: "application/json",
				Payload: &forge.Schema{
					Type: "object",
					Properties: map[string]*forge.Schema{
						"type":    {Type: "string", Enum: []any{"leave"}},
						"room_id": {Type: "string", Description: "Room ID to leave"},
					},
					Required: []string{"type", "room_id"},
				},
			},
			"SendMessage": {
				MessageID:   "SendMessage",
				Name:        "SendMessage",
				Title:       "Room Message",
				Summary:     "Message sent within a room",
				ContentType: "application/json",
				Payload: &forge.Schema{
					Type:       "object",
					Properties: messageSchemaProperties(),
					Required:   []string{"type", "room_id", "user_id", "data"},
				},
			},
			"ReceiveMessage": {
				MessageID:   "ReceiveMessage",
				Name:        "ReceiveMessage",
				Title:       "Receive Room Message",
				Summary:     "Message received from a room",
				ContentType: "application/json",
				Payload: &forge.Schema{
					Type:       "object",
					Properties: messageSchemaProperties(),
					Required:   []string{"id", "type", "room_id", "user_id", "data", "timestamp"},
				},
			},
		},
		Bindings: &forge.AsyncAPIChannelBindings{
			WS: &forge.WebSocketChannelBinding{
				Method:         "GET",
				BindingVersion: "latest",
			},
		},
	}

	// Operations
	spec.Operations["joinRoom"] = &forge.AsyncAPIOperation{
		Action: "send",
		Channel: &forge.AsyncAPIChannelReference{
			Ref: "#/channels/rooms",
		},
		Title:   "Join Room",
		Summary: "Client joins a room",
		Messages: []forge.AsyncAPIMessageReference{
			{Ref: "#/channels/rooms/messages/JoinRoom"},
		},
	}

	spec.Operations["leaveRoom"] = &forge.AsyncAPIOperation{
		Action: "send",
		Channel: &forge.AsyncAPIChannelReference{
			Ref: "#/channels/rooms",
		},
		Title:   "Leave Room",
		Summary: "Client leaves a room",
		Messages: []forge.AsyncAPIMessageReference{
			{Ref: "#/channels/rooms/messages/LeaveRoom"},
		},
	}

	spec.Operations["sendRoomMessage"] = &forge.AsyncAPIOperation{
		Action: "send",
		Channel: &forge.AsyncAPIChannelReference{
			Ref: "#/channels/rooms",
		},
		Title:   "Send Room Message",
		Summary: "Client sends a message to a room",
		Messages: []forge.AsyncAPIMessageReference{
			{Ref: "#/channels/rooms/messages/SendMessage"},
		},
	}

	spec.Operations["receiveRoomMessage"] = &forge.AsyncAPIOperation{
		Action: "receive",
		Channel: &forge.AsyncAPIChannelReference{
			Ref: "#/channels/rooms",
		},
		Title:   "Receive Room Message",
		Summary: "Client receives messages from a room",
		Messages: []forge.AsyncAPIMessageReference{
			{Ref: "#/channels/rooms/messages/ReceiveMessage"},
		},
	}
}

// addChannelChannels adds pub/sub channel operations.
func (e *Extension) addChannelChannels(spec *forge.AsyncAPISpec) {
	channelID := "channels"
	spec.Channels[channelID] = &forge.AsyncAPIChannel{
		Address:     "/channels/{channelId}",
		Title:       "Pub/Sub Channel",
		Summary:     "Subscribe to real-time events on specific channels",
		Description: "WebSocket channel for subscribing to and publishing messages on named channels",
		Parameters: map[string]*forge.AsyncAPIParameter{
			"channelId": {
				Description: "Unique identifier of the channel",
				Schema: &forge.Schema{
					Type: "string",
				},
			},
		},
		Messages: map[string]*forge.AsyncAPIMessage{
			"Subscribe": {
				MessageID:   "Subscribe",
				Name:        "Subscribe",
				Title:       "Subscribe to Channel",
				Summary:     "Subscribe to receive messages from a channel",
				ContentType: "application/json",
				Payload: &forge.Schema{
					Type: "object",
					Properties: map[string]*forge.Schema{
						"action":     {Type: "string", Enum: []any{"subscribe"}},
						"channel_id": {Type: "string", Description: "Channel ID to subscribe to"},
					},
					Required: []string{"action", "channel_id"},
				},
			},
			"Publish": {
				MessageID:   "Publish",
				Name:        "Publish",
				Title:       "Publish to Channel",
				Summary:     "Publish a message to a channel",
				ContentType: "application/json",
				Payload: &forge.Schema{
					Type: "object",
					Properties: map[string]*forge.Schema{
						"action":     {Type: "string", Enum: []any{"publish"}},
						"channel_id": {Type: "string", Description: "Channel ID to publish to"},
						"data":       {Type: "object", Description: "Message data"},
					},
					Required: []string{"action", "channel_id", "data"},
				},
			},
		},
		Bindings: &forge.AsyncAPIChannelBindings{
			WS: &forge.WebSocketChannelBinding{
				Method:         "GET",
				BindingVersion: "latest",
			},
		},
	}

	spec.Operations["subscribeChannel"] = &forge.AsyncAPIOperation{
		Action: "send",
		Channel: &forge.AsyncAPIChannelReference{
			Ref: "#/channels/channels",
		},
		Title:   "Subscribe to Channel",
		Summary: "Subscribe to channel updates",
		Messages: []forge.AsyncAPIMessageReference{
			{Ref: "#/channels/channels/messages/Subscribe"},
		},
	}

	spec.Operations["publishChannel"] = &forge.AsyncAPIOperation{
		Action: "send",
		Channel: &forge.AsyncAPIChannelReference{
			Ref: "#/channels/channels",
		},
		Title:   "Publish to Channel",
		Summary: "Publish message to channel",
		Messages: []forge.AsyncAPIMessageReference{
			{Ref: "#/channels/channels/messages/Publish"},
		},
	}
}

// addPresenceChannels adds presence tracking operations.
func (e *Extension) addPresenceChannels(spec *forge.AsyncAPISpec) {
	channelID := "presence"
	spec.Channels[channelID] = &forge.AsyncAPIChannel{
		Address:     "/presence",
		Title:       "Presence Channel",
		Summary:     "Real-time user presence updates",
		Description: "WebSocket channel for tracking user online/offline status and activity",
		Messages: map[string]*forge.AsyncAPIMessage{
			"PresenceUpdate": {
				MessageID:   "PresenceUpdate",
				Name:        "PresenceUpdate",
				Title:       "Presence Update",
				Summary:     "User presence status change",
				ContentType: "application/json",
				Payload: &forge.Schema{
					Type: "object",
					Properties: map[string]*forge.Schema{
						"type":          {Type: "string", Enum: []any{"presence"}},
						"user_id":       {Type: "string", Description: "User ID"},
						"status":        {Type: "string", Enum: []any{"online", "away", "busy", "offline"}, Description: "Presence status"},
						"custom_status": {Type: "string", Description: "Custom status message"},
					},
					Required: []string{"type", "user_id", "status"},
				},
			},
		},
		Bindings: &forge.AsyncAPIChannelBindings{
			WS: &forge.WebSocketChannelBinding{
				Method:         "GET",
				BindingVersion: "latest",
			},
		},
	}

	spec.Operations["updatePresence"] = &forge.AsyncAPIOperation{
		Action: "send",
		Channel: &forge.AsyncAPIChannelReference{
			Ref: "#/channels/presence",
		},
		Title:   "Update Presence",
		Summary: "Update user presence status",
		Messages: []forge.AsyncAPIMessageReference{
			{Ref: "#/channels/presence/messages/PresenceUpdate"},
		},
	}

	spec.Operations["receivePresence"] = &forge.AsyncAPIOperation{
		Action: "receive",
		Channel: &forge.AsyncAPIChannelReference{
			Ref: "#/channels/presence",
		},
		Title:   "Receive Presence Updates",
		Summary: "Receive presence updates from other users",
		Messages: []forge.AsyncAPIMessageReference{
			{Ref: "#/channels/presence/messages/PresenceUpdate"},
		},
	}
}

// addTypingChannels adds typing indicator operations.
func (e *Extension) addTypingChannels(spec *forge.AsyncAPISpec) {
	channelID := "typing"
	spec.Channels[channelID] = &forge.AsyncAPIChannel{
		Address:     "/typing/{roomId}",
		Title:       "Typing Indicators",
		Summary:     "Real-time typing status in rooms",
		Description: "WebSocket channel for tracking who is typing in a room",
		Parameters: map[string]*forge.AsyncAPIParameter{
			"roomId": {
				Description: "Unique identifier of the room",
				Schema: &forge.Schema{
					Type: "string",
				},
			},
		},
		Messages: map[string]*forge.AsyncAPIMessage{
			"TypingStart": {
				MessageID:   "TypingStart",
				Name:        "TypingStart",
				Title:       "Start Typing",
				Summary:     "User started typing",
				ContentType: "application/json",
				Payload: &forge.Schema{
					Type: "object",
					Properties: map[string]*forge.Schema{
						"type":    {Type: "string", Enum: []any{"typing"}},
						"room_id": {Type: "string", Description: "Room ID"},
						"user_id": {Type: "string", Description: "User ID"},
						"data":    {Type: "boolean", Enum: []any{true}, Description: "Typing status (true)"},
					},
					Required: []string{"type", "room_id", "user_id", "data"},
				},
			},
			"TypingStop": {
				MessageID:   "TypingStop",
				Name:        "TypingStop",
				Title:       "Stop Typing",
				Summary:     "User stopped typing",
				ContentType: "application/json",
				Payload: &forge.Schema{
					Type: "object",
					Properties: map[string]*forge.Schema{
						"type":    {Type: "string", Enum: []any{"typing"}},
						"room_id": {Type: "string", Description: "Room ID"},
						"user_id": {Type: "string", Description: "User ID"},
						"data":    {Type: "boolean", Enum: []any{false}, Description: "Typing status (false)"},
					},
					Required: []string{"type", "room_id", "user_id", "data"},
				},
			},
		},
		Bindings: &forge.AsyncAPIChannelBindings{
			WS: &forge.WebSocketChannelBinding{
				Method:         "GET",
				BindingVersion: "latest",
			},
		},
	}

	spec.Operations["startTyping"] = &forge.AsyncAPIOperation{
		Action: "send",
		Channel: &forge.AsyncAPIChannelReference{
			Ref: "#/channels/typing",
		},
		Title:   "Start Typing",
		Summary: "Indicate user started typing",
		Messages: []forge.AsyncAPIMessageReference{
			{Ref: "#/channels/typing/messages/TypingStart"},
		},
	}

	spec.Operations["stopTyping"] = &forge.AsyncAPIOperation{
		Action: "send",
		Channel: &forge.AsyncAPIChannelReference{
			Ref: "#/channels/typing",
		},
		Title:   "Stop Typing",
		Summary: "Indicate user stopped typing",
		Messages: []forge.AsyncAPIMessageReference{
			{Ref: "#/channels/typing/messages/TypingStop"},
		},
	}

	spec.Operations["receiveTyping"] = &forge.AsyncAPIOperation{
		Action: "receive",
		Channel: &forge.AsyncAPIChannelReference{
			Ref: "#/channels/typing",
		},
		Title:   "Receive Typing Updates",
		Summary: "Receive typing indicators from other users",
		Messages: []forge.AsyncAPIMessageReference{
			{Ref: "#/channels/typing/messages/TypingStart"},
			{Ref: "#/channels/typing/messages/TypingStop"},
		},
	}
}

// addMessageSchemas adds common message schemas to components.
func (e *Extension) addMessageSchemas(spec *forge.AsyncAPISpec) {
	// Add base Message type
	spec.Components.Schemas["Message"] = &forge.Schema{
		Type:        "object",
		Description: "Base message structure for streaming events",
		Properties:  messageSchemaProperties(),
		Required:    []string{"id", "type", "user_id", "data", "timestamp"},
	}

	// Error message
	spec.Components.Schemas["Error"] = &forge.Schema{
		Type:        "object",
		Description: "Error message",
		Properties: map[string]*forge.Schema{
			"type":    {Type: "string", Enum: []any{"error"}},
			"code":    {Type: "string", Description: "Error code"},
			"message": {Type: "string", Description: "Error message"},
		},
		Required: []string{"type", "code", "message"},
	}
}

// messageSchemaProperties returns the common message schema properties.
func messageSchemaProperties() map[string]*forge.Schema {
	return map[string]*forge.Schema{
		"id": {
			Type:        "string",
			Description: "Unique message identifier",
		},
		"type": {
			Type:        "string",
			Description: "Message type",
			Enum:        []any{"message", "presence", "typing", "system", "join", "leave", "error"},
		},
		"event": {
			Type:        "string",
			Description: "Optional event name",
		},
		"room_id": {
			Type:        "string",
			Description: "Room identifier (if applicable)",
		},
		"channel_id": {
			Type:        "string",
			Description: "Channel identifier (if applicable)",
		},
		"user_id": {
			Type:        "string",
			Description: "User identifier",
		},
		"data": {
			Description: "Message payload data",
		},
		"metadata": {
			Type:                 "object",
			Description:          "Additional metadata",
			AdditionalProperties: true,
		},
		"timestamp": {
			Type:        "string",
			Format:      "date-time",
			Description: "Message timestamp",
		},
		"thread_id": {
			Type:        "string",
			Description: "Thread identifier for threaded conversations",
		},
	}
}
