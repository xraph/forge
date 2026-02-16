# HLS Extension for Forge

Production-ready HTTP Live Streaming (HLS) extension with adaptive bitrate streaming, transcoding, and distributed support.

## Features

- **Adaptive Bitrate Streaming**: Automatically switches between quality levels based on network conditions
- **Live Streaming**: Support for real-time live streams with DVR capabilities
- **Video on Demand (VOD)**: Convert video files to HLS format with multiple quality levels
- **Transcoding**: FFmpeg-based transcoding to multiple resolutions and bitrates
- **Segmentation**: Automatic video segmentation into HLS-compatible chunks
- **Multiple Storage Backends**: Uses Forge storage extension (Local, S3, GCS, Azure)
- **Distributed Support**: Run multiple HLS nodes with coordination
- **Auto Cleanup**: Automatic cleanup of old segments
- **CORS Support**: Built-in CORS headers for cross-origin playback
- **Health Checks**: Integrated health monitoring
- **Statistics**: Track viewer counts, bandwidth, and performance metrics

## Installation

```bash
go get github.com/xraph/forge/extensions/hls
```

### Prerequisites

- FFmpeg and FFprobe must be installed on your system
- For transcoding: FFmpeg with appropriate codecs (h264, h265, aac, etc.)
- Forge storage extension must be registered before HLS extension

```bash
# Install FFmpeg on Ubuntu/Debian
sudo apt-get install ffmpeg

# Install FFmpeg on macOS
brew install ffmpeg

# Verify installation
ffmpeg -version
ffprobe -version
```

## Quick Start

```go
package main

import (
    "context"
    "log"
    
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/hls"
    "github.com/xraph/forge/extensions/storage"
)

func main() {
    app := forge.New()
    
    // Register storage extension (required)
    app.UseExtension(storage.NewExtension(storage.Config{
        Backends: map[string]storage.BackendConfig{
            "default": {
                Type: "local",
                Config: map[string]interface{}{
                    "base_path": "./hls_storage",
                },
            },
        },
        Default: "default",
    }))
    
    // Register HLS extension
    app.UseExtension(hls.NewExtension(
        hls.WithBasePath("/hls"),
        hls.WithBaseURL("http://localhost:8080/hls"),
        hls.WithTargetDuration(6),
        hls.WithTranscoding(true, hls.DefaultProfiles()...),
    ))
    
    ctx := context.Background()
    if err := app.Start(ctx); err != nil {
        log.Fatal(err)
    }
    
    log.Println("HLS server started on http://localhost:8080")
    app.Listen(":8080")
}
```

## Configuration

### Basic Configuration

```go
hlsExt := hls.NewExtension(
    hls.WithBasePath("/hls"),
    hls.WithBaseURL("http://localhost:8080/hls"),
    hls.WithStorageBackend("default"),
    hls.WithStoragePrefix("hls"),
    hls.WithTargetDuration(6),           // 6 second segments
    hls.WithDVRWindow(10),               // Keep last 10 segments for DVR
    hls.WithCORS(true, "*"),
)
```

### Transcoding Configuration

```go
hlsExt := hls.NewExtension(
    hls.WithTranscoding(true,
        hls.Profile360p,   // 640x360, 800 Kbps
        hls.Profile480p,   // 854x480, 1.4 Mbps
        hls.Profile720p,   // 1280x720, 2.8 Mbps
        hls.Profile1080p,  // 1920x1080, 5 Mbps
    ),
    hls.WithFFmpegPaths("ffmpeg", "ffprobe"),
)
```

### Custom Transcoding Profile

```go
customProfile := hls.TranscodeProfile{
    Name:       "custom",
    Width:      1920,
    Height:     1080,
    Bitrate:    4000000, // 4 Mbps
    FrameRate:  30,
    VideoCodec: "h264",
    AudioCodec: "aac",
    Preset:     "medium",
}

hlsExt := hls.NewExtension(
    hls.WithTranscoding(true, customProfile),
)
```

### Storage Configuration

```go
// Use specific storage backend
hlsExt := hls.NewExtension(
    hls.WithStorageBackend("s3"),        // Use S3 backend
    hls.WithStoragePrefix("live-streams"), // Storage prefix
)
```

### Cleanup Configuration

```go
hlsExt := hls.NewExtension(
    hls.WithCleanup(
        24 * time.Hour,  // Keep segments for 24 hours
        1 * time.Hour,   // Run cleanup every hour
    ),
)
```

## API Endpoints

### Stream Management

#### Create Stream

```http
POST /hls/streams
Content-Type: application/json

{
    "title": "My Live Stream",
    "description": "Live stream description",
    "type": "live",
    "target_duration": 6,
    "dvr_window_size": 10,
    "transcode_profiles": [
        {
            "name": "720p",
            "width": 1280,
            "height": 720,
            "bitrate": 2800000,
            "frame_rate": 30,
            "video_codec": "h264",
            "audio_codec": "aac"
        }
    ]
}
```

#### List Streams

```http
GET /hls/streams
```

#### Get Stream

```http
GET /hls/streams/:streamID
```

#### Delete Stream

```http
DELETE /hls/streams/:streamID
```

### Live Streaming

#### Start Live Stream

```http
POST /hls/streams/:streamID/start
```

#### Stop Live Stream

```http
POST /hls/streams/:streamID/stop
```

#### Ingest Segment

```http
POST /hls/streams/:streamID/ingest
Content-Type: application/json

{
    "variant_id": "variant-uuid",
    "sequence_num": 123,
    "duration": 6.0,
    "data": "base64_encoded_segment_data"
}
```

### Playback

#### Master Playlist

```http
GET /hls/:streamID/master.m3u8
```

#### Media Playlist

```http
GET /hls/:streamID/variants/:variantID/playlist.m3u8
```

#### Segment

```http
GET /hls/:streamID/variants/:variantID/segment_:segmentNum.ts
```

### Statistics

```http
GET /hls/streams/:streamID/stats
```

Response:

```json
{
    "stream_id": "uuid",
    "current_viewers": 150,
    "total_views": 1500,
    "total_bandwidth": 750000000,
    "segments_served": 50000,
    "error_rate": 0.001,
    "variant_stats": {
        "variant-uuid": {
            "variant_id": "variant-uuid",
            "request_count": 25000,
            "bytes_served": 500000000,
            "current_viewers": 75
        }
    }
}
```

## Usage Examples

### Live Streaming

```go
// Get HLS service from DI
hlsSvc, _ := forge.Inject[hls.HLS](app.Container())

// Create live stream
stream, err := hlsSvc.CreateStream(ctx, hls.StreamOptions{
    Title:          "Live Event",
    Type:           hls.StreamTypeLive,
    TargetDuration: 6,
    DVRWindowSize:  20,
    TranscodeProfiles: []hls.TranscodeProfile{
        hls.Profile360p,
        hls.Profile720p,
    },
})

// Start streaming
hlsSvc.StartLiveStream(ctx, stream.ID)

// Ingest segments (from your video encoder)
for segment := range segments {
    hlsSvc.IngestSegment(ctx, stream.ID, &segment)
}

// Stop streaming
hlsSvc.StopLiveStream(ctx, stream.ID)
```

### Video on Demand (VOD)

```go
// Get manager (extends HLS interface)
hlsMgr, _ := forge.Inject[*hls.Manager](app.Container())

// Create VOD from file
stream, err := hlsMgr.CreateVODFromFile(ctx, "/path/to/video.mp4", hls.VODOptions{
    Title:          "Movie Title",
    Description:    "Movie description",
    TargetDuration: 10,
    TranscodeProfiles: []hls.TranscodeProfile{
        hls.Profile480p,
        hls.Profile720p,
        hls.Profile1080p,
    },
})

// Stream is ready (transcoding happens asynchronously)
log.Printf("Stream URL: http://localhost:8080/hls/%s/master.m3u8", stream.ID)
```

### Client-Side Playback

```html
<!DOCTYPE html>
<html>
<head>
    <script src="https://cdn.jsdelivr.net/npm/hls.js@latest"></script>
</head>
<body>
    <video id="video" controls width="800"></video>
    <script>
        const video = document.getElementById('video');
        const streamUrl = 'http://localhost:8080/hls/STREAM_ID/master.m3u8';
        
        if (Hls.isSupported()) {
            const hls = new Hls();
            hls.loadSource(streamUrl);
            hls.attachMedia(video);
            hls.on(Hls.Events.MANIFEST_PARSED, () => {
                video.play();
            });
        } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
            // Native HLS support (Safari)
            video.src = streamUrl;
            video.addEventListener('loadedmetadata', () => {
                video.play();
            });
        }
    </script>
</body>
</html>
```

## Architecture

### Components

1. **Extension**: Main entry point, registers routes and services
2. **Manager**: Core HLS logic, stream management
3. **Segmenter**: Video segmentation using FFmpeg
4. **Transcoder**: Video transcoding with multiple profiles
5. **Playlist Generator**: M3U8 playlist generation
6. **Storage**: HLS-specific storage wrapper around Forge storage extension
7. **Segment Tracker**: Tracks segments for live streams with DVR

### Storage Structure

```
hls/
├── {streamID}/
│   ├── master.m3u8
│   ├── metadata.json
│   └── {variantID}/
│       ├── playlist.m3u8
│       ├── segment_0.ts
│       ├── segment_1.ts
│       └── segment_N.ts
```

## Performance Considerations

### Segment Duration

- **6 seconds**: Good balance between latency and efficiency (default)
- **2-4 seconds**: Lower latency for live events
- **10 seconds**: Better efficiency for VOD

### DVR Window

- Determines how far back viewers can seek in live streams
- Larger window = more storage but better user experience
- Default: 10 segments (60 seconds with 6-second segments)

### Transcoding

- Transcoding is CPU-intensive
- Use hardware acceleration when available (CUDA, QSV, VideoToolbox)
- Limit concurrent transcoding jobs (default: 4)
- Consider pre-transcoding VOD content

### Caching

- Enable CDN caching for segments (Cache-Control headers)
- Master and media playlists can be cached with short TTL
- Segments are immutable and can be cached indefinitely

## Distributed Mode

HLS extension supports distributed deployment using the Forge consensus extension for high-availability and horizontal scaling.

### Requirements

1. **Consensus Extension**: Raft-based leader election and coordination
2. **Shared Storage**: S3, GCS, or Azure (local storage works for testing)
3. **Load Balancer**: Distribute requests across nodes (optional)

### Configuration

```go
// Register consensus extension first
consensusExt := consensus.NewExtension(
    consensus.WithNodeID("hls-node-1"),
    consensus.WithClusterID("hls-cluster"),
    consensus.WithBindAddress("0.0.0.0:7000"),
    consensus.WithTransportType("tcp"),
)

// Configure HLS with distributed mode
hlsExt := hls.NewExtension(
    hls.WithDistributed(true),
    hls.WithNodeID("hls-node-1"),
    hls.WithClusterID("hls-cluster"),
    hls.WithFailover(true),
)

app.UseExtension(storageExt)     // Storage extension
app.UseExtension(consensusExt)   // Consensus extension (required)
app.UseExtension(hlsExt)         // HLS extension
```

### Features

- **Leader Election**: Automatic leader election using Raft
- **Write Operations**: Stream creation/deletion only on leader
- **Read Operations**: Playlists and segments available on all nodes
- **Automatic Failover**: Re-election on leader failure
- **Cluster Awareness**: Headers showing node and leader info
- **Request Routing**: Automatic redirect to leader for write operations

### Running a Cluster

```bash
# Node 1 (Bootstrap)
./hls-server -node=node1 -port=8081 -raft=7001

# Node 2
./hls-server -node=node2 -port=8082 -raft=7002 -peers=node1:7001

# Node 3
./hls-server -node=node3 -port=8083 -raft=7003 -peers=node1:7001
```

### Cluster Headers

All responses include cluster information:

```http
X-HLS-Node: node1
X-HLS-Leader: node1
X-HLS-Is-Leader: true
```

### Leader-Only Operations

- `POST /hls/streams` - Create stream
- `DELETE /hls/streams/:id` - Delete stream
- `POST /hls/streams/:id/start` - Start live stream
- `POST /hls/streams/:id/stop` - Stop live stream

Followers return `307 Temporary Redirect` pointing to the leader.

### All-Node Operations

- `GET /hls/streams` - List streams
- `GET /hls/streams/:id` - Get stream
- `GET /hls/:id/master.m3u8` - Master playlist
- `GET /hls/:id/variants/:vid/playlist.m3u8` - Media playlist
- `GET /hls/:id/variants/:vid/segment_*.ts` - Segments

### Failover

When the leader fails:

1. Remaining nodes detect failure (heartbeat timeout)
2. New election is triggered
3. New leader is elected (requires quorum)
4. Write operations resume on new leader
5. Read operations continue uninterrupted

Typical failover time: 2-5 seconds (configurable)

### Production Setup

```
        Load Balancer (nginx/HAProxy)
                 |
    +------------+------------+
    |            |            |
  Node 1       Node 2       Node 3
 (Leader)    (Follower)   (Follower)
    |            |            |
    +------------+------------+
          Consensus Layer
           (Raft Protocol)
                 |
           Shared Storage
            (S3/GCS/Azure)
```

#### Load Balancer Configuration

```nginx
upstream hls_cluster {
    # Health check on /health
    server node1:8081 max_fails=3 fail_timeout=30s;
    server node2:8082 max_fails=3 fail_timeout=30s;
    server node3:8083 max_fails=3 fail_timeout=30s;
}

server {
    listen 80;
    
    location /hls/ {
        proxy_pass http://hls_cluster;
        proxy_next_upstream error timeout http_307;
        proxy_set_header Host $host;
    }
}
```

### Monitoring

Cluster metrics exposed via consensus extension:

- `hls_cluster_size` - Total nodes in cluster
- `hls_cluster_healthy_nodes` - Healthy nodes count
- `hls_cluster_has_quorum` - Whether cluster has quorum
- `hls_cluster_leader` - Current leader node
- `hls_cluster_streams` - Total streams in cluster

### Best Practices

1. **Odd Number of Nodes**: Use 3, 5, or 7 nodes for proper quorum
2. **Network Latency**: Keep Raft nodes in same region (< 50ms latency)
3. **Shared Storage**: Use S3/GCS for production (all nodes must access)
4. **Clock Sync**: Ensure NTP is configured on all nodes
5. **Health Checks**: Configure LB to remove unhealthy nodes
6. **Graceful Shutdown**: Drain connections before stopping nodes

### See Also

- [Distributed Example](./examples/distributed/) - Complete working example
- [Consensus Extension](../consensus/) - Consensus extension documentation

## Monitoring

### Metrics

The extension exposes Prometheus metrics:

- `hls_active_streams`: Number of active streams
- `hls_total_viewers`: Total concurrent viewers
- `hls_segments_served`: Total segments served
- `hls_bandwidth_bytes`: Total bandwidth used
- `hls_transcode_jobs`: Active transcoding jobs

### Health Checks

```go
// Health check endpoint is automatically registered
// GET /health will include HLS extension status
```

## Troubleshooting

### FFmpeg Not Found

```
Error: ffmpeg: executable file not found in $PATH
```

Solution: Install FFmpeg or specify path:

```go
hls.WithFFmpegPaths("/usr/local/bin/ffmpeg", "/usr/local/bin/ffprobe")
```

### Storage Extension Not Found

```
Error: failed to resolve storage manager
```

Solution: Register storage extension before HLS:

```go
app.UseExtension(storageExt) // Must come first
app.UseExtension(hlsExt)
```

### Segments Not Playing

- Check FFmpeg logs for encoding errors
- Verify segments are properly saved to storage
- Ensure CORS headers are set if cross-origin
- Check browser console for HLS.js errors

### High Memory Usage

- Reduce DVR window size
- Enable automatic cleanup
- Limit concurrent transcoding jobs
- Use streaming instead of buffering entire segments

## Examples

See the [examples](./examples) directory for complete working examples:

- **basic**: Simple live streaming server
- **vod**: Video on demand with transcoding
- **player.html**: HTML5 video player with HLS.js

Run examples:

```bash
# Basic live streaming
cd examples/basic
go run main.go

# VOD from file
cd examples/vod
go run main.go /path/to/video.mp4
```

## License

Same as Forge framework.

## Contributing

Contributions welcome! Please open an issue or PR on the main Forge repository.

## Credits

Built on top of:
- FFmpeg for transcoding and segmentation
- Forge framework
- HLS specification (RFC 8216)

