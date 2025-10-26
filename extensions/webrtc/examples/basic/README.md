# WebRTC Video Call Example

This example demonstrates how to build a complete WebRTC video calling application using the Forge WebRTC extension.

## Features

- **Mesh and SFU Topologies**: Support for peer-to-peer mesh calling and Selective Forwarding Unit (SFU) routing
- **Quality Monitoring**: Real-time connection quality tracking and adaptive bitrate
- **Call Recording**: Built-in recording capabilities with multiple formats
- **Authentication**: JWT-based authentication system
- **Real-time Streaming**: Integration with Forge streaming for presence and messaging
- **Health Monitoring**: Built-in health checks and metrics

## Architecture

The example uses:
- **WebRTC Extension**: Core WebRTC functionality with mesh/SFU topologies
- **Streaming Extension**: Real-time presence, rooms, and messaging
- **Auth Extension**: JWT authentication and authorization
- **HTTP API**: REST endpoints for room management and call control

## API Endpoints

### Room Management
- `POST /call/create` - Create a new call room
- `GET /call/{roomID}` - Get room information and participants
- `GET /calls` - List all active call rooms

### Call Operations
- `POST /call/{roomID}/join` - Join a call room
- `POST /call/{roomID}/leave` - Leave a call room
- `GET /call/{roomID}/quality` - Get call quality metrics

### Recording
- `POST /call/{roomID}/record/start` - Start recording
- `POST /call/{roomID}/record/stop` - Stop recording

### System
- `GET /health` - Health check endpoint
- `WebSocket /webrtc/signal/{roomID}` - WebRTC signaling endpoint

## Configuration

The example is configured with:

### WebRTC Settings
- **Topology**: SFU (Selective Forwarding Unit)
- **STUN Servers**: Google STUN servers for NAT traversal
- **TURN Servers**: Configurable relay servers
- **Quality Monitoring**: Enabled with 5-second intervals
- **Recording**: Enabled with WebM format

### Streaming Settings
- **Backend**: Local (in-memory)
- **Features**: Rooms, channels, presence, typing indicators, message history
- **Connection Limits**: 5 connections per user
- **Message Limits**: 64KB max size, 100 messages per second

## Usage

1. **Build the example**:
   ```bash
   cd examples/basic
   go build .
   ```

2. **Run the server**:
   ```bash
   ./basic
   ```

3. **Create a call room**:
   ```bash
   curl -X POST http://localhost:8080/call/create \
     -d "room_id=test-room" \
     -d "room_name=Test Room" \
     -d "max_members=10"
   ```

4. **Join the call** (with authentication):
   ```bash
   curl -X POST http://localhost:8080/call/test-room/join \
     -H "Authorization: Bearer YOUR_JWT_TOKEN" \
     -d "display_name=John Doe"
   ```

5. **Check call quality**:
   ```bash
   curl http://localhost:8080/call/test-room/quality
   ```

## WebRTC Integration

The example includes a WebRTC signaling WebSocket endpoint at `/webrtc/signal/{roomID}`. Frontend clients should:

1. Connect to the signaling WebSocket
2. Exchange SDP offers/answers through the WebSocket
3. Use the configured STUN/TURN servers for ICE candidates
4. Implement video/audio capture and playback

## Security Considerations

- Authentication is required for all call operations
- Room access is controlled through the streaming extension
- Configurable rate limiting prevents abuse
- Input validation on all endpoints

## Development

The example demonstrates best practices for:
- Extension registration and dependency management
- HTTP route handling with proper error responses
- WebRTC configuration for production use
- Integration with other Forge extensions
- Health monitoring and observability

## Scaling

For production deployment:
- Switch from local streaming backend to Redis/NATS
- Configure multiple TURN servers for global reach
- Implement load balancing for the SFU topology
- Add monitoring and alerting for call quality metrics
