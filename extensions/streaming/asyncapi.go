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
		Address: "/channels/{channelId}",
		Title:   "Pub/Sub Channel",
		Summary: "Receive and publish real-time events on specific channels",
		// Subscription is not an in-band operation. This channel used to
		// document a Subscribe message with an action verb, and nothing ever
		// read it: handleMessage dispatches on Message.Type and has no
		// subscribe case, so a client following that spec sent a frame the
		// server silently discarded and then waited forever for events that
		// were never routed to it.
		//
		// Subscribing is a REST call against the connection, which works for
		// any transport -- the handler resolves the connection by id and checks
		// the caller owns it, rather than caring how it was established.
		Description: "WebSocket channel for receiving and publishing messages on named channels. " +
			"Subscribe and unsubscribe out of band: POST {ssePath}/subscribe and {ssePath}/unsubscribe " +
			"with {\"conn_id\": \"<connection id>\", \"channels\": [\"<channel id>\"]}, where ssePath is " +
			"the path passed to RegisterRoutes. The same routes accept a \"rooms\" list and work for " +
			"WebSocket connections as well as SSE.",
		Parameters: map[string]*forge.AsyncAPIParameter{
			"channelId": {
				Description: "Unique identifier of the channel",
				Schema: &forge.Schema{
					Type: "string",
				},
			},
		},
		Messages: map[string]*forge.AsyncAPIMessage{
			"Publish": {
				MessageID:   "Publish",
				Name:        "Publish",
				Title:       "Publish to Channel",
				Summary:     "Publish a message to a channel",
				ContentType: "application/json",
				// Publishing to a channel is an ordinary message frame carrying a
				// channel_id -- that is what handleMessage routes to
				// BroadcastToChannel. The action verb documented here was not a
				// field on the envelope and was never read by anything, so a
				// client following this spec published into a void.
				Payload: &forge.Schema{
					Type: "object",
					Properties: map[string]*forge.Schema{
						"type":       {Type: "string", Enum: []any{"message"}},
						"channel_id": {Type: "string", Description: "Channel ID to publish to"},
						"event":      {Type: "string", Description: "Domain event name; required for the frame to be bindable by a generated client"},
						"data":       {Description: "Message payload"},
					},
					Required: []string{"type", "channel_id", "data"},
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

	// No subscribeChannel operation: subscribing is the out-of-band REST call
	// described on the channel above, not a frame on this socket.

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
				// Every frame on this socket is a Message, so the status rides in
				// data, exactly as the typing indicator's boolean does. This
				// previously documented top-level status and custom_status
				// fields, which the envelope has no room for and no producer
				// could emit -- a client validating against it would have
				// rejected every real presence frame.
				Payload: &forge.Schema{
					Type: "object",
					Properties: map[string]*forge.Schema{
						"type":    {Type: "string", Enum: []any{"presence"}},
						"user_id": {Type: "string", Description: "User ID (assigned by the server on inbound frames)"},
						"data": {
							Type:        "string",
							Enum:        []any{"online", "away", "busy", "offline"},
							Description: "Presence status",
						},
					},
					Required: []string{"type", "data"},
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
			Type: "string",
			// handleMessage overwrites this from the authenticated connection
			// before dispatching, so a value a client sends is decorative. Said
			// here because the schema is shared by both directions and reads as
			// a client-supplied field otherwise.
			Description: "User identifier. Authoritative on frames from the server; ignored and overwritten on frames from a client",
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
