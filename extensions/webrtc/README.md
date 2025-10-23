# WebRTC Extension for Forge v2

Production-ready WebRTC extension that leverages the streaming extension for signaling, authentication, and room management.

## Features

✅ **Multiple Topologies**
- Mesh (P2P) - Best for 2-4 participants
- SFU (Selective Forwarding Unit) - Best for 5-50 participants  
- MCU (Multipoint Control Unit) - Best for 50+ participants (planned)

✅ **Signaling via Streaming**
- Uses streaming extension's WebSocket/SSE for signaling
- Automatic room management
- Distributed signaling across nodes

✅ **Media Management**
- Audio/Video tracks
- Screen sharing
- Data channels
- Simulcast support (SFU)

✅ **Quality Monitoring**
- Real-time connection quality metrics
- Packet loss, jitter, latency tracking
- Adaptive quality based on network conditions

✅ **Recording**
- Record audio/video streams
- Multiple formats (WebM, MP4)
- Per-room or per-participant recording

✅ **Security**
- Integrated with streaming auth
- Room-level permissions
- STUN/TURN server support

## Installation

```bash
go get github.com/xraph/forge/extensions/webrtc
```

## Quick Start

### Basic Video Call

```go
package main

import (
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/streaming"
    "github.com/xraph/forge/extensions/webrtc"
)

func main() {
    app := forge.NewApp(forge.AppConfig{
        Name: "Video Call App",
    })
    
    // Register streaming extension
    streamingExt := streaming.New(streaming.Config{
        RequireAuth: true,
        AuthProviders: []string{"jwt"},
    })
    app.Use(streamingExt)
    
    // Register WebRTC extension
    webrtcExt, _ := webrtc.New(streamingExt, webrtc.DefaultConfig())
    app.Use(webrtcExt)
    
    // Register routes
    router := app.Router()
    webrtcExt.RegisterRoutes(router)
    
    // Custom endpoint
    router.POST("/call/create", func(ctx forge.Context) error {
        roomID := ctx.PostValue("room_id")
        userID := ctx.Get("user_id").(string)
        
        // Create call room
        room, err := webrtcExt.CreateCallRoom(ctx, roomID, streaming.RoomOptions{
            Name:       "My Video Call",
            MaxMembers: 10,
        })
        if err != nil {
            return err
        }
        
        // Join call
        peer, err := room.Join(ctx, userID, &webrtc.JoinOptions{
            AudioEnabled: true,
            VideoEnabled: true,
        })
        if err != nil {
            return err
        }
        
        return ctx.JSON(map[string]any{
            "room_id": room.ID(),
            "peer_id": peer.ID(),
        })
    })
    
    app.Run(":8080")
}
```

## Configuration

### Mesh Topology (Default)

```go
config := webrtc.Config{
    Topology: webrtc.TopologyMesh,
    
    STUNServers: []string{
        "stun:stun.l.google.com:19302",
    },
    
    MediaConfig: webrtc.MediaConfig{
        AudioEnabled:    true,
        AudioCodecs:     []string{"opus"},
        VideoEnabled:    true,
        VideoCodecs:     []string{"VP8", "H264"},
        MaxVideoBitrate: 2500, // kbps
    },
}
```

### SFU Topology

```go
config := webrtc.Config{
    Topology: webrtc.TopologySFU,
    
    SFUConfig: &webrtc.SFUConfig{
        WorkerCount:      4,
        MaxBandwidthMbps: 100,
        SimulcastEnabled: true,
        QualityLayers: []webrtc.QualityLayer{
            {RID: "f", MaxWidth: 1920, MaxHeight: 1080, MaxFPS: 30, Bitrate: 2500},
            {RID: "h", MaxWidth: 1280, MaxHeight: 720,  MaxFPS: 30, Bitrate: 1200},
            {RID: "q", MaxWidth: 640,  MaxHeight: 360,  MaxFPS: 30, Bitrate: 500},
        },
    },
}
```

### With TURN Servers

```go
config := webrtc.Config{
    STUNServers: []string{
        "stun:stun.example.com:3478",
    },
    
    TURNServers: []webrtc.TURNConfig{{
        URLs:       []string{"turn:turn.example.com:3478"},
        Username:   "user",
        Credential: "pass",
    }},
}
```

### With Recording

```go
config := webrtc.Config{
    RecordingEnabled: true,
    RecordingPath:    "/var/recordings",
}

// Start recording
err := webrtcExt.GetRecorder().Start(ctx, roomID, &webrtc.RecordingOptions{
    Format:     "webm",
    VideoCodec: "VP8",
    AudioCodec: "opus",
    OutputPath: "/var/recordings/call-123.webm",
})
```

## Usage Examples

### 1. One-on-One Call

```go
// User A creates offer
router.POST("/call/offer", func(ctx forge.Context) error {
    roomID := ctx.PostValue("room_id")
    userID := ctx.Get("user_id").(string)
    
    room, _ := webrtcExt.GetCallRoom(roomID)
    peer, _ := room.Join(ctx, userID, &webrtc.JoinOptions{
        AudioEnabled: true,
        VideoEnabled: true,
    })
    
    offer, _ := peer.CreateOffer(ctx)
    peer.SetLocalDescription(ctx, offer)
    
    return ctx.JSON(offer)
})

// User B creates answer
router.POST("/call/answer", func(ctx forge.Context) error {
    roomID := ctx.PostValue("room_id")
    userID := ctx.Get("user_id").(string)
    offer := ctx.PostValue("offer") // SDP offer
    
    room, _ := webrtcExt.GetCallRoom(roomID)
    peer, _ := room.Join(ctx, userID, nil)
    
    peer.SetRemoteDescription(ctx, offer)
    answer, _ := peer.CreateAnswer(ctx)
    peer.SetLocalDescription(ctx, answer)
    
    return ctx.JSON(answer)
})
```

### 2. Group Video Call

```go
router.POST("/group/join", func(ctx forge.Context) error {
    roomID := ctx.PostValue("room_id")
    userID := ctx.Get("user_id").(string)
    
    room, err := webrtcExt.GetCallRoom(roomID)
    if err != nil {
        // Create room if doesn't exist
        room, _ = webrtcExt.CreateCallRoom(ctx, roomID, streaming.RoomOptions{
            Name:       "Group Call",
            MaxMembers: 50,
        })
    }
    
    peer, _ := room.Join(ctx, userID, &webrtc.JoinOptions{
        AudioEnabled: true,
        VideoEnabled: true,
        DisplayName:  ctx.PostValue("display_name"),
    })
    
    // Get all participants
    participants := room.GetParticipants()
    
    return ctx.JSON(map[string]any{
        "peer_id":      peer.ID(),
        "participants": participants,
    })
})
```

### 3. Screen Sharing

```go
router.POST("/screen/start", func(ctx forge.Context) error {
    roomID := ctx.PostValue("room_id")
    userID := ctx.Get("user_id").(string)
    
    room, _ := webrtcExt.GetCallRoom(roomID)
    
    // Create screen share track (would use actual WebRTC library)
    screenTrack := createScreenShareTrack()
    
    err := room.StartScreenShare(ctx, userID, screenTrack)
    if err != nil {
        return err
    }
    
    return ctx.JSON(map[string]any{
        "status": "screen_sharing_started",
    })
})
```

### 4. Quality Monitoring

```go
router.GET("/call/{roomID}/quality", func(ctx forge.Context) error {
    roomID := ctx.Param("roomID")
    
    room, _ := webrtcExt.GetCallRoom(roomID)
    quality, _ := room.GetQuality(ctx)
    
    return ctx.JSON(quality)
})

// Monitor quality changes
monitor := webrtcExt.GetQualityMonitor()
monitor.OnQualityChange(func(peerID string, quality *webrtc.ConnectionQuality) {
    if quality.Score < 50 {
        // Poor quality - notify user or reduce bitrate
        log.Printf("Poor quality for peer %s: score=%f", peerID, quality.Score)
    }
})
```

## Architecture

### Signaling Flow

```
┌─────────┐                    ┌─────────┐
│ Client  │                    │  Server │
│   A     │                    │ (Forge) │
└────┬────┘                    └────┬────┘
     │                              │
     │  WebSocket Connect           │
     │────────────────────────────>│
     │                              │
     │  Create Offer (SDP)          │
     │────────────────────────────>│
     │                              │
     │           Streaming Message  │
     │<────────────────────────────│
     │  (via streaming extension)   │
     │                              │
     │  ICE Candidates              │
     │<────────────────────────────│
     │                              │
     
┌─────────────────────────────────────────┐
│   After Signaling: P2P Connection       │
├─────────────────────────────────────────┤
│  Client A  <──────RTP────────>  Client B│
│  (direct peer-to-peer connection)       │
└─────────────────────────────────────────┘
```

### Topology Comparison

| Feature | Mesh | SFU | MCU |
|---------|------|-----|-----|
| **Participants** | 2-4 | 5-50 | 50+ |
| **Client Upload** | High (N streams) | Low (1 stream) | Low (1 stream) |
| **Client Download** | High (N streams) | Medium (N streams) | Low (1 stream) |
| **Server CPU** | None | Low | High |
| **Server Bandwidth** | Low | High | High |
| **Latency** | Lowest | Low | Medium |
| **Best For** | 1-on-1 calls | Webinars, meetings | Large conferences |

## Integration with Streaming Extension

The WebRTC extension leverages streaming for:

### 1. **Signaling Transport**
```go
// Signaling messages use streaming rooms
signaling.SendOffer(ctx, roomID, peerID, offer)
// Internally uses: streaming.SendToRoom()
```

### 2. **Authentication**
```go
// Inherit auth from streaming
streaming.Config{
    RequireAuth: true,
    AuthProviders: []string{"jwt"},
}
// WebRTC calls automatically authenticated
```

### 3. **Room Management**
```go
// Call rooms extend streaming rooms
type CallRoom interface {
    streaming.Room  // Inherits all room features
    // + WebRTC-specific methods
}
```

### 4. **Presence**
```go
// Participant presence tracked via streaming
participants := room.GetParticipants()
// Uses streaming.PresenceTracker under the hood
```

### 5. **Distributed Coordination**
```go
// Multi-node signaling via streaming coordinator
streaming.Config{
    Coordination: streaming.CoordinationConfig{
        Enabled: true,
        Backend: "redis",
    },
}
// WebRTC signaling works across nodes
```

## Client Integration

### JavaScript Client Example

```javascript
// Connect to signaling server
const ws = new WebSocket('wss://example.com/webrtc/signal/room-123');
const pc = new RTCPeerConnection({
    iceServers: [
        { urls: 'stun:stun.l.google.com:19302' }
    ]
});

// Add local media
const stream = await navigator.mediaDevices.getUserMedia({
    audio: true,
    video: true
});
stream.getTracks().forEach(track => pc.addTrack(track, stream));

// Create and send offer
const offer = await pc.createOffer();
await pc.setLocalDescription(offer);
ws.send(JSON.stringify({
    type: 'webrtc.offer',
    sdp: offer
}));

// Handle answer
ws.onmessage = async (event) => {
    const msg = JSON.parse(event.data);
    
    if (msg.type === 'webrtc.answer') {
        await pc.setRemoteDescription(msg.sdp);
    }
    
    if (msg.type === 'webrtc.ice_candidate') {
        await pc.addIceCandidate(msg.candidate);
    }
};

// Send ICE candidates
pc.onicecandidate = (event) => {
    if (event.candidate) {
        ws.send(JSON.stringify({
            type: 'webrtc.ice_candidate',
            candidate: event.candidate
        }));
    }
};

// Receive remote tracks
pc.ontrack = (event) => {
    const remoteVideo = document.getElementById('remote-video');
    remoteVideo.srcObject = event.streams[0];
};
```

## Performance Considerations

### Mesh Topology
- ✅ **Pros**: Lowest latency, no server cost
- ❌ **Cons**: Doesn't scale (N² connections), high client bandwidth

```go
// Good for: 1-on-1, 2-person calls
config := webrtc.Config{
    Topology: webrtc.TopologyMesh,
}
```

### SFU Topology
- ✅ **Pros**: Scales to 50+, moderate server cost
- ❌ **Cons**: Server bandwidth intensive

```go
// Good for: Team meetings, webinars
config := webrtc.Config{
    Topology: webrtc.TopologySFU,
    SFUConfig: &webrtc.SFUConfig{
        SimulcastEnabled: true,  // Client sends 3 quality layers
        AdaptiveBitrate:  true,  // Adjust based on network
    },
}
```

## Deployment

### Single Node
```yaml
# docker-compose.yml
services:
  forge:
    image: forge-app
    ports:
      - "8080:8080"
      - "3478:3478/udp"  # TURN
    environment:
      WEBRTC_TOPOLOGY: "sfu"
      STUN_SERVER: "stun:stun.l.google.com:19302"
```

### Multi-Node with Load Balancer
```yaml
services:
  forge-1:
    image: forge-app
    environment:
      NODE_ID: "node-1"
      STREAMING_BACKEND: "redis"
      REDIS_URL: "redis://redis:6379"
  
  forge-2:
    image: forge-app
    environment:
      NODE_ID: "node-2"
      STREAMING_BACKEND: "redis"
      REDIS_URL: "redis://redis:6379"
  
  redis:
    image: redis:7-alpine
  
  nginx:
    image: nginx
    # Sticky session load balancing for WebRTC
```

## Monitoring

### Metrics

The extension exposes metrics compatible with Prometheus:

```
# Connection metrics
webrtc_connections_total
webrtc_connections_active
webrtc_connections_failed

# Media metrics
webrtc_tracks_total{kind="audio|video"}
webrtc_bytes_sent_total
webrtc_bytes_received_total
webrtc_packet_loss_ratio

# Quality metrics
webrtc_connection_quality_score
webrtc_jitter_milliseconds
webrtc_round_trip_time_milliseconds
```

### Logging

```go
// Enable debug logging
logger := forge.NewLogger(forge.LogConfig{
    Level: forge.LogLevelDebug,
})

// WebRTC extension will log:
// - Connection state changes
// - Signaling messages
// - ICE candidate exchanges
// - Media track additions/removals
// - Quality warnings
```

## Security Best Practices

1. **Always use authentication**
```go
webrtc.Config{
    RequireAuth: true,
}
```

2. **Use TURN over TLS**
```go
TURNServers: []webrtc.TURNConfig{{
    URLs:       []string{"turns:turn.example.com:5349"},
    TLSEnabled: true,
}},
```

3. **Implement rate limiting**
```go
streaming.Config{
    RateLimitEnabled: true,
    RateLimit: streaming.RateLimitConfig{
        ConnectionsPerUser: 5,
        ConnectionsPerIP:   20,
    },
}
```

4. **Room-level permissions**
```go
// Use streaming room auth
room.CanJoin(ctx, userID) // Checks permissions
```

## Roadmap

- [ ] Full pion/webrtc integration
- [ ] MCU topology support
- [ ] End-to-end encryption (E2EE)
- [ ] Noise suppression / background blur
- [ ] Advanced recording features
- [ ] React/Vue client SDK
- [ ] Mobile SDK (iOS/Android)

## Contributing

See [CONTRIBUTING.md](../../CONTRIBUTING.md)

## License

MIT License - see [LICENSE](../../LICENSE)

