# Streaming Chat Example

A simple real-time chat application demonstrating the Forge v2 Streaming Extension.

## Features

- ✅ Real-time messaging with WebSocket
- ✅ Multiple chat rooms
- ✅ Typing indicators
- ✅ Message history
- ✅ Room management
- ✅ User presence (basic)

## Running the Example

```bash
cd v2/extensions/streaming/examples/chat
go run main.go
```

Then open http://localhost:8080 in your browser.

## Architecture

```
┌─────────────────┐
│   Web Browser   │ ←→ WebSocket
└─────────────────┘
        ↓
┌─────────────────┐
│  Forge Router   │
└─────────────────┘
        ↓
┌─────────────────┐
│ Streaming Ext   │
└─────────────────┘
        ↓
┌─────────────────┐
│  Local Backend  │ (in-memory)
└─────────────────┘
```

## Code Structure

- **main.go** - Application entry point with HTTP server
- **WebSocket Handler** - Built-in handler from streaming extension
- **REST API** - Room management endpoints
- **Frontend** - Single-page HTML/JS chat interface

## API Endpoints

### WebSocket
- `GET /ws` - WebSocket connection

### REST API
- `POST /api/v1/rooms` - Create room
- `GET /api/v1/rooms` - List rooms
- `GET /api/v1/rooms/:id/history` - Get message history
- `GET /api/v1/rooms/:id/members` - Get room members

## Message Protocol

### Join Room
```json
{
  "type": "join",
  "room_id": "room-uuid",
  "user_id": "username"
}
```

### Send Message
```json
{
  "type": "message",
  "room_id": "room-uuid",
  "user_id": "username",
  "data": "Hello, world!",
  "timestamp": "2025-10-23T12:00:00Z"
}
```

### Typing Indicator
```json
{
  "type": "typing",
  "room_id": "room-uuid",
  "user_id": "username",
  "data": true
}
```

## Switching to Redis Backend

To use Redis for production:

```go
streamExt := streaming.NewExtension(
    streaming.WithRedisBackend("redis://localhost:6379"),
    streaming.WithFeatures(true, true, true, true, true),
)
```

## Production Enhancements

For production use, consider:

1. **Authentication** - Add auth middleware
2. **Rate Limiting** - Limit messages per user
3. **Persistence** - Use Redis/PostgreSQL
4. **Monitoring** - Add metrics and logging
5. **Load Balancing** - Multiple nodes with Redis
6. **Message Validation** - Sanitize user input
7. **Error Handling** - Better error messages

## Testing Multiple Users

Open multiple browser windows/tabs to simulate multiple users chatting in real-time.

## Troubleshooting

**WebSocket connection fails**
- Check that port 8080 is available
- Ensure firewall allows connections
- Verify WebSocket URL is correct

**Messages not appearing**
- Check browser console for errors
- Verify room is selected
- Confirm WebSocket is connected

**Typing indicators not working**
- Ensure typing feature is enabled in config
- Check message type is "typing"
- Verify room ID matches current room

