# WebTransport Example

This example demonstrates the WebTransport API structure in Forge.

## What is WebTransport?

WebTransport is a new web API that provides low-latency, bidirectional, client-server messaging using HTTP/3 as the transport protocol. It offers several advantages over WebSockets:

- **Lower latency**: Uses QUIC protocol (UDP-based)
- **Multiple streams**: Supports multiple independent streams
- **Datagrams**: Unreliable, unordered message delivery for time-sensitive data
- **Better congestion control**: QUIC's built-in congestion control
- **Connection migration**: Survives network changes

## Features

- ✅ Bidirectional streams
- ✅ Unidirectional streams
- ✅ Datagram support
- ✅ Multiple concurrent streams
- ✅ HTTP/3 transport
- ✅ TLS 1.3 required

## Prerequisites

```bash
go get github.com/quic-go/quic-go
go get github.com/quic-go/webtransport-go
```

## Running the Example

```bash
cd v2/examples/webtransport
go run main.go
```

The server will start on:
- HTTP: `http://localhost:8080`

**Note:** This is a reference implementation showing the WebTransport API structure. The core WebTransport interfaces and router methods are implemented in `v2/internal/router/webtransport.go`. Full end-to-end support requires HTTP/3 server integration with the main app lifecycle.

## Implementation Status

### ✅ Completed

1. **Core Interfaces** (`v2/internal/router/webtransport.go`)
   - `WebTransportSession` interface
   - `WebTransportStream` interface
   - Session and stream wrappers

2. **Router Integration** (`v2/internal/router/router_webtransport.go`)
   - `router.WebTransport()` method
   - `router.EnableWebTransport()` configuration
   - `router.StartHTTP3()` server management
   - `router.StopHTTP3()` cleanup

3. **Configuration**
   - `WebTransportConfig` with sensible defaults
   - Route options (`WithWebTransport`, `WithWebTransportDatagrams`, `WithWebTransportStreams`)
   - Integration with `StreamConfig`

4. **Tests**
   - Interface tests
   - Configuration tests
   - Route option tests

### 🚧 Pending Integration

- HTTP/3 server lifecycle integration with `app.Run()`
- Certificate management helpers
- Full end-to-end example with running server

## Usage (When Fully Integrated)

### Basic Server Setup

```go
package main

import (
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/streaming"
)

func main() {
    config := forge.DefaultAppConfig()
    config.Name = "webtransport-server"
    app := forge.NewApp(config)

    // Register streaming extension
    app.RegisterExtension(streaming.NewExtension(
        streaming.WithLocalBackend(),
    ))

    router := app.Router()

    // Enable WebTransport
    wtConfig := forge.DefaultWebTransportConfig()
    router.EnableWebTransport(wtConfig)

    // Register WebTransport handler
    router.WebTransport("/wt/chat", handleChat)

    // Start app (HTTP/3 server starts automatically)
    app.Run()
}

func handleChat(ctx forge.Context, session forge.WebTransportSession) error {
    // Accept and handle streams
    stream, _ := session.AcceptStream(ctx.Context())

    // Read/write data
    data := make([]byte, 4096)
    n, _ := stream.Read(data)
    stream.Write(data[:n])

    // Send/receive datagrams
    session.SendDatagram([]byte("fast message"))
    msg, _ := session.ReceiveDatagram(ctx.Context())

    return nil
}
```

### JavaScript Client

```javascript
// Connect to WebTransport server
const url = 'https://localhost:4433/wt/chat';
const transport = new WebTransport(url);

await transport.ready;
console.log('Connected to WebTransport server');

// Send data via bidirectional stream
const stream = await transport.createBidirectionalStream();
const writer = stream.writable.getWriter();
const reader = stream.readable.getReader();

await writer.write(new TextEncoder().encode('Hello WebTransport!'));

// Read response
const { value, done } = await reader.read();
if (!done) {
  console.log('Received:', new TextDecoder().decode(value));
}

// Send datagram (unreliable, fast)
const dgWriter = transport.datagrams.writable.getWriter();
await dgWriter.write(new TextEncoder().encode('Quick message'));

// Close connection
await transport.close();
```

### Go Client

```go
package main

import (
    "context"
    "crypto/tls"
    "fmt"
    "log"

    "github.com/quic-go/quic-go/http3"
    "github.com/quic-go/webtransport-go"
)

func main() {
    roundTripper := &http3.RoundTripper{
        TLSClientConfig: &tls.Config{
            InsecureSkipVerify: true, // For testing only
        },
    }
    defer roundTripper.Close()

    rsp, session, err := webtransport.Dial(
        context.Background(),
        "https://localhost:4433/wt/chat",
        roundTripper,
    )
    if err != nil {
        log.Fatal(err)
    }
    defer session.Close()

    // Open bidirectional stream
    stream, _ := session.OpenStream()
    stream.Write([]byte("Hello!"))

    // Send datagram
    session.SendDatagram([]byte("Quick ping"))
}
```

## Configuration

### WebTransportConfig

```go
type WebTransportConfig struct {
    MaxBidiStreams          int64  // Default: 100
    MaxUniStreams           int64  // Default: 100
    MaxDatagramFrameSize    int64  // Default: 65536 (64KB)
    EnableDatagrams         bool   // Default: true
    StreamReceiveWindow     uint64 // Default: 6MB
    ConnectionReceiveWindow uint64 // Default: 15MB
    KeepAliveInterval       int    // Default: 30000ms
    MaxIdleTimeout          int    // Default: 60000ms
}
```

### Server Configuration

```go
wtConfig := forge.DefaultWebTransportConfig()
wtConfig.MaxBidiStreams = 200           // Max bidirectional streams
wtConfig.MaxUniStreams = 200            // Max unidirectional streams
wtConfig.EnableDatagrams = true         // Enable datagram support
wtConfig.MaxDatagramFrameSize = 65536   // 64KB max datagram size
wtConfig.KeepAliveInterval = 30000      // 30 seconds
wtConfig.MaxIdleTimeout = 60000         // 60 seconds

router.EnableWebTransport(wtConfig)
router.StartHTTP3(":4433", tlsConfig)
```

### TLS Requirements

WebTransport requires TLS 1.3:

```go
tlsConfig := &tls.Config{
    Certificates: []tls.Certificate{cert},
    MinVersion:   tls.VersionTLS13,
}
```

## Use Cases

### 1. Real-time Gaming

```go
// Use datagrams for player position (unreliable, fast)
session.SendDatagram(marshalPosition(player))

// Use streams for chat messages (reliable)
stream, _ := session.OpenStream()
stream.WriteJSON(chatMessage)
```

### 2. Video Streaming

```go
// Use unidirectional streams for video chunks
stream, _ := session.OpenUniStream()
for chunk := range videoChunks {
    stream.Write(chunk)
}
```

### 3. Live Data Feeds

```go
// Use datagrams for stock prices (latest value only)
session.SendDatagram(marshalStockPrice(symbol, price))

// Use streams for order confirmations (reliable)
stream.WriteJSON(orderConfirmation)
```

## Streams vs Datagrams

| Feature | Streams | Datagrams |
|---------|---------|-----------|
| Reliability | Reliable, ordered | Unreliable, unordered |
| Use case | Chat, file transfer | Gaming, live data |
| Latency | Higher | Lower |
| Ordering | Guaranteed | Not guaranteed |

## Browser Support

WebTransport is supported in:
- Chrome 97+
- Edge 97+
- Opera 83+

Check current support: [Can I use WebTransport](https://caniuse.com/webtransport)

## Performance Tips

1. **Use datagrams for time-sensitive data**: Stock prices, player positions
2. **Use streams for critical data**: Chat messages, file transfers
3. **Reuse streams**: Opening streams has overhead
4. **Batch small messages**: Combine multiple small messages
5. **Monitor congestion**: QUIC provides built-in congestion control

## Comparison with WebSockets

| Feature | WebTransport | WebSocket |
|---------|-------------|-----------|
| Protocol | HTTP/3 (QUIC/UDP) | HTTP/1.1 or HTTP/2 (TCP) |
| Latency | Lower | Higher |
| Streams | Multiple independent | Single stream |
| Head-of-line blocking | No | Yes (TCP) |
| Datagrams | Yes | No |
| Browser support | Limited | Wide |

## Implementation Files

- `v2/internal/router/webtransport.go` - Core interfaces and wrappers
- `v2/internal/router/router_webtransport.go` - Router integration
- `v2/internal/router/router_webtransport_opts.go` - Route options
- `v2/internal/router/webtransport_test.go` - Tests
- `v2/internal/router/streaming.go` - StreamConfig with WT support

## Resources

- [WebTransport Specification](https://w3c.github.io/webtransport/)
- [QUIC Protocol](https://www.rfc-editor.org/rfc/rfc9000.html)
- [HTTP/3 Specification](https://www.rfc-editor.org/rfc/rfc9114.html)
- [WebTransport API](https://developer.mozilla.org/en-US/docs/Web/API/WebTransport)
- [quic-go Documentation](https://github.com/quic-go/quic-go)

## License

Part of the Forge framework.
