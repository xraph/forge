# Streaming Extension

Real-time streaming extension for Forge with WebSocket/SSE support, rooms, channels, presence tracking, typing indicators, and distributed coordination.

## Features

- ✅ **WebSocket & SSE** - Built on top of Forge's core router streaming support
- ✅ **Rooms** - Create chat rooms with members, roles, and permissions
- ✅ **Channels** - Pub/sub channels with filters and subscriptions
- ✅ **Presence** - Track online/offline/away status across users
- ✅ **Typing Indicators** - Real-time typing indicators per room
- ✅ **Message History** - Persist and retrieve message history
- ✅ **Distributed** - Redis/NATS backends for multi-node deployments
- ✅ **Authorization** - Room and message policies wired by default, not opt-in
- ✅ **Gap-free reconnect** - Per-room sequences and cursor-based replay over SSE
- ✅ **Backpressure** - Bounded per-connection send queue; one slow client cannot stall a broadcast
- ✅ **Interface-First** - All major components are interfaces for testability

## Installation

```go
import "github.com/xraph/forge/extensions/streaming"
```

## Quick Start

### Basic Setup (Local Backend)

```go
package main

import (
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/streaming"
)

func main() {
    // Create container and app
    container := forge.NewContainer()
    app := forge.NewApp(container)
    router := forge.NewRouter(forge.WithContainer(container))

    // Create streaming extension with local backend
    streamExt := streaming.NewExtension(
        streaming.WithLocalBackend(),
        streaming.WithFeatures(true, true, true, true, true), // rooms, channels, presence, typing, history
    )

    // Register and start
    app.Use(streamExt)
    app.Start(context.Background())

    // Register streaming routes
    streamExt.RegisterRoutes(router, "/ws", "/sse")

    // Start server
    http.ListenAndServe(":8080", router)
}
```

### With Redis Backend (Distributed)

```go
streamExt := streaming.NewExtension(
    streaming.WithRedisBackend("redis://localhost:6379"),
    streaming.WithFeatures(true, true, true, true, true),
    streaming.WithNodeID("node-1"), // Optional, auto-generated if not set
)
```

## Architecture

### Interface-First Design

All major components are defined as interfaces with multiple implementations:

```
streaming (interfaces)
├── Manager              - Central orchestrator
├── EnhancedConnection   - WebSocket connection with metadata
├── Room                 - Room management
├── RoomStore           - Room persistence backend
├── Channel             - Pub/sub channel
├── ChannelStore        - Channel persistence backend
├── PresenceTracker     - Presence tracking
├── PresenceStore       - Presence persistence backend
├── TypingTracker       - Typing indicators
├── TypingStore         - Typing persistence backend
├── MessageStore        - Message history
└── DistributedBackend  - Cross-node coordination
```

### Package Structure

```
v2/extensions/streaming/
├── streaming.go         # Core interfaces (Manager, EnhancedConnection)
├── room.go             # Room interfaces
├── channel.go          # Channel interfaces
├── presence.go         # Presence interfaces
├── typing.go           # Typing interfaces
├── persistence.go      # Message store interfaces
├── distributed.go      # Distributed backend interfaces
├── config.go           # Configuration
├── errors.go           # Domain errors
├── manager.go          # Manager implementation
├── connection.go       # Enhanced connection implementation
├── extension.go        # Extension entry point
├── backends/
│   ├── factory.go      # Store factory
│   ├── local/          # In-memory implementations
│   ├── redis/          # Redis implementations (TODO)
│   └── nats/           # NATS implementations (TODO)
└── trackers/
    ├── presence_tracker.go  # Presence tracker implementation
    └── typing_tracker.go    # Typing tracker implementation
```

## Usage Examples

### Custom WebSocket Handler

```go
router.WebSocket("/chat", func(ctx forge.Context, conn forge.Connection) error {
    // Get streaming manager from DI
    var manager streaming.Manager
    ctx.Container().Resolve(&manager)

    // Get user from auth
    userID := ctx.Get("user_id").(string)

    // Create enhanced connection
    enhanced := streaming.NewEnhancedConnection(conn)
    enhanced.SetUserID(userID)
    enhanced.SetSessionID(uuid.New().String())

    // Register
    manager.Register(enhanced)
    defer manager.Unregister(conn.ID())

    // Set online
    manager.SetPresence(ctx.Request().Context(), userID, streaming.StatusOnline)
    defer manager.SetPresence(ctx.Request().Context(), userID, streaming.StatusOffline)

    // Message loop
    for {
        var msg streaming.Message
        if err := conn.ReadJSON(&msg); err != nil {
            return err
        }

        reqCtx := ctx.Request().Context()

        // Identity comes from the connection, never from the client. Without
        // this a client can set user_id to anything and have the server
        // broadcast and persist it under that name.
        msg.UserID = enhanced.GetUserID()

        // The inbound gate: size cap, per-user rate limit, target
        // authorization, content validation. Skipping it leaves an unmetered
        // path from any socket straight to a broadcast.
        gated, err := manager.ProcessInbound(reqCtx, &msg, enhanced)
        if err != nil {
            // Tell the client why, so it can back off rather than retrying
            // into the same limit forever. NewLifecycleMessage rather than a
            // literal Event: a lifecycle name in Event is a name no generated
            // manifest binds, and the client reports it as an unknown message
            // instead of dropping it. The name rides in Metadata instead.
            rejected := streaming.NewLifecycleMessage(streaming.MessageTypeError, "message.rejected")
            rejected.Data = map[string]any{"error": err.Error()}

            _ = conn.WriteJSON(rejected)

            continue
        }

        switch gated.Type {
        case streaming.MessageTypeMessage:
            if gated.RoomID != "" {
                // Save first: this assigns gated.Sequence, which is what lets a
                // reconnecting client resume from exactly this point.
                manager.SaveMessage(reqCtx, gated)
                manager.BroadcastToRoom(reqCtx, gated.RoomID, gated)
            }
        case streaming.MessageTypeJoin:
            manager.JoinRoom(reqCtx, conn.ID(), gated.RoomID)
        case streaming.MessageTypeLeave:
            manager.LeaveRoom(reqCtx, conn.ID(), gated.RoomID)
        }
    }
})
```

<!-- `enhanced` above is the streaming.Connection registered with the manager;
     see the full example in examples/chat/main.go. -->

Using `RegisterRoutes` instead of a hand-written handler does all of the above for you.

### Room Management REST API

```go
api := router.Group("/api/v1")

// Create room
api.POST("/rooms", func(ctx forge.Context, req *CreateRoomRequest) error {
    var manager streaming.Manager
    ctx.Container().Resolve(&manager)

    userID := ctx.Get("user_id").(string)

    room := streaming.RoomOptions{
        ID:          uuid.New().String(),
        Name:        req.Name,
        Description: req.Description,
        Owner:       userID,
    }

    if err := manager.CreateRoom(ctx.Request().Context(), room); err != nil {
        return err
    }

    return ctx.JSON(200, room)
})

// Get room history
api.GET("/rooms/:id/history", func(ctx forge.Context) error {
    var manager streaming.Manager
    ctx.Container().Resolve(&manager)

    roomID := ctx.Param("id")

    messages, err := manager.GetHistory(ctx.Request().Context(), roomID, streaming.HistoryQuery{
        Limit: 100,
    })
    if err != nil {
        return err
    }

    return ctx.JSON(200, messages)
})
```

## Configuration

### Complete Configuration Example

```go
streamExt := streaming.NewExtension(
    // Backend
    streaming.WithBackend("redis"),
    streaming.WithBackendURLs("redis://localhost:6379"),
    streaming.WithAuthentication("username", "password"),

    // Features
    streaming.WithFeatures(true, true, true, true, true),

    // Limits
    streaming.WithConnectionLimits(5, 50, 100), // conns/user, rooms/user, channels/user
    streaming.WithMessageLimits(64*1024, 100),  // max size, max/second

    // Timeouts
    streaming.WithTimeouts(30*time.Second, 10*time.Second, 10*time.Second), // ping, pong, write

    // Retention
    streaming.WithMessageRetention(30 * 24 * time.Hour), // 30 days

    // Distributed
    streaming.WithNodeID("node-1"),

    // TLS
    streaming.WithTLS("cert.pem", "key.pem", "ca.pem"),
)
```

### Configuration from File

```yaml
# config.yaml
extensions:
  streaming:
    backend: redis
    backend_urls:
      - redis://localhost:6379
    enable_rooms: true
    enable_channels: true
    enable_presence: true
    enable_typing_indicators: true
    enable_message_history: true
    max_connections_per_user: 5
    max_rooms_per_user: 50
    max_message_size: 65536
    message_retention: 720h # 30 days
```

```go
// Automatically loads from config
streamExt := streaming.NewExtension()
```

## Message Protocol

### Message Structure

```go
type Message struct {
    ID        string         `json:"id"`
    Type      string         `json:"type"`      // "message", "presence", "typing", "system"
    Event     string         `json:"event,omitempty"`
    RoomID    string         `json:"room_id,omitempty"`
    ChannelID string         `json:"channel_id,omitempty"`
    UserID    string         `json:"user_id"`
    Data      any            `json:"data"`
    RawData   []byte         `json:"-"`                      // Binary payload
    ContentType string       `json:"content_type,omitempty"` // MIME type of Data
    Metadata  map[string]any `json:"metadata,omitempty"`
    Timestamp time.Time      `json:"timestamp"`
    ThreadID  string         `json:"thread_id,omitempty"`
    Sequence  int64          `json:"sequence,omitempty"`     // Per-room, assigned on save
}
```

`Type` is the transport kind; `Event` is the domain name (`order.created`). The generated TypeScript
client binds on `Event`, so a frame an application is expected to handle must set it — build those
with `NewEventMessage` rather than by hand.

`Sequence` is assigned by the message store when a room message is saved, and is what lets a
reconnecting client be sent exactly what it missed.

### Message Types

- `MessageTypeMessage` - Regular chat message
- `MessageTypePresence` - Presence update (online/offline/away)
- `MessageTypeTyping` - Typing indicator
- `MessageTypeSystem` - System notification
- `MessageTypeJoin` - User joined room
- `MessageTypeLeave` - User left room
- `MessageTypeError` - Error message

## Backend Comparison

| Feature | Local | Redis | NATS |
|---------|-------|-------|------|
| Single Node | ✅ | ✅ | ✅ |
| Multi-Node | ❌ | ✅ | ✅ |
| Persistence | Memory | Disk | Disk |
| Message History | Limited | Full | Full |
| Presence Sync | ❌ | ✅ | ✅ |
| Performance | Fastest | Fast | Fastest |
| Setup | None | Redis | NATS Server |

## Security

Authorization is wired by default rather than offered as an opt-in.

- **Room joins** consult a `RoomAuthorizer`. The default admits members, admits anyone to a public
  room, and refuses non-members entry to a private one. Replace it with `WithRoomAuthorizer`.
- **Sends** require the connection to have joined the target room, pass the `MessageAuthorizer`, and
  not be muted or banned there.
- **Inbound messages** must go through `Manager.ProcessInbound` before broadcast — size cap, per-user
  rate limit, target authorization, then content validation. `RegisterRoutes` does this for you; a
  custom socket handler must call it, or it leaves an unmetered path from any socket to a broadcast.
- **Identity** is stamped from the authenticated connection. A client cannot set `user_id`.
- **Session resumption** binds a snapshot to the user who created it.
- **Anonymous connections** are capped separately, since no per-user limit can bound them.

## Migration

Several previously inert settings are now enforced, and a few semantics changed. See the
[migration guide](https://xraph.com/docs/forge/extensions/streaming/migration) — in particular
`StartTyping`/`StopTyping` take `(ctx, userID, roomID)`, which is the one change that compiles either
way.

## Production Considerations

### Scaling

For distributed deployments:

1. Use Redis or NATS backend
2. Set unique node IDs per instance
3. Configure proper timeouts and limits
4. Enable message persistence
5. Monitor metrics

### Security

- Always use authentication middleware before WebSocket routes
- Validate user permissions for room/channel access
- Rate limit connections per user
- Use TLS in production
- Never log sensitive message content

### Monitoring

Key metrics to monitor:

- `streaming.connections.active` - Active connections
- `streaming.connections.total` - Total connections created
- `streaming.messages.broadcast` - Messages broadcast
- `streaming.rooms.joins` - Room joins
- `streaming.presence.updates` - Presence updates

### Health Checks

```go
// Extension provides health check
if err := streamExt.Health(ctx); err != nil {
    log.Error("streaming unhealthy", err)
}
```

## Testing

### Mock Implementations

All interfaces can be easily mocked for testing:

```go
type mockManager struct {
    streaming.Manager
    registerCalls int
}

func (m *mockManager) Register(conn streaming.EnhancedConnection) error {
    m.registerCalls++
    return nil
}

func TestMyHandler(t *testing.T) {
    manager := &mockManager{}
    // Test with mock manager
}
```

### Integration Tests

```go
// Use local backend for tests
streamExt := streaming.NewExtension(streaming.WithLocalBackend())
// Run tests against real implementation
```

## Roadmap

- [ ] Redis backend implementation
- [ ] NATS backend implementation  
- [ ] Message compression for old messages
- [ ] Advanced filtering and search
- [ ] WebRTC signaling support
- [ ] GraphQL subscriptions integration
- [ ] Admin dashboard for monitoring

## License

Part of Forge framework.

