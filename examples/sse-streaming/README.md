# SSE Streaming Example

This example demonstrates how to use Forge's SSE (Server-Sent Events) streaming helpers for real-time updates with full AsyncAPI documentation.

## Features

- **Automatic Headers**: `router.SSE()` automatically sets all required SSE headers
- **Simple API**: Use `ctx.WriteSSE()` to send events with automatic content type detection
- **Auto-flush**: Events are automatically flushed to clients
- **Context Cancellation**: Properly handles client disconnects via `ctx.Context().Done()`
- **AsyncAPI Documentation**: Full AsyncAPI 3.0 spec generation with event schemas

## Running the Example

```bash
cd examples/sse-streaming
go run main.go
```

The server will start on `http://localhost:8080`.

## Testing the Stream

### View AsyncAPI Documentation

Open http://localhost:8080/asyncapi in your browser to see the interactive AsyncAPI documentation. It shows:
- All event types (`connected`, `update`, `milestone`)
- Complete message schemas with descriptions
- Channel information and bindings

Or get the raw spec:
```bash
curl http://localhost:8080/asyncapi.json
```

### Using curl

```bash
curl -N http://localhost:8080/events
```

You'll see events streaming in real-time:

```
event: connected
data: Welcome to SSE stream

event: update
data: {"timestamp":1701234567,"message":"Periodic update #1","counter":1}

event: update
data: {"timestamp":1701234572,"message":"Periodic update #2","counter":2}

event: milestone
data: Milestone reached: 3 updates sent

event: update
data: {"timestamp":1701234577,"message":"Periodic update #3","counter":3}
```

### Using Browser

Open `http://localhost:8080/events` in your browser. You'll see the events streaming in real-time.

Or use this HTML file:

```html
<!DOCTYPE html>
<html>
<head>
    <title>SSE Example</title>
</head>
<body>
    <h1>Server-Sent Events Demo</h1>
    <div id="events"></div>

    <script>
        const eventSource = new EventSource('http://localhost:8080/events');
        const eventsDiv = document.getElementById('events');

        eventSource.addEventListener('connected', (e) => {
            eventsDiv.innerHTML += `<p><strong>Connected:</strong> ${e.data}</p>`;
        });

        eventSource.addEventListener('update', (e) => {
            const data = JSON.parse(e.data);
            eventsDiv.innerHTML += `<p><strong>Update:</strong> ${data.message} at ${new Date(data.timestamp * 1000).toLocaleString()}</p>`;
        });

        eventSource.addEventListener('milestone', (e) => {
            eventsDiv.innerHTML += `<p style="color: green;"><strong>Milestone:</strong> ${e.data}</p>`;
        });

        eventSource.onerror = () => {
            eventsDiv.innerHTML += `<p style="color: red;">Connection lost</p>`;
        };
    </script>
</body>
</html>
```

## Key Code Patterns

### 1. Define Event Schemas

```go
type UpdateEvent struct {
    Timestamp int64  `json:"timestamp" description:"Event timestamp"`
    Message   string `json:"message" description:"Update message"`
    Counter   int    `json:"counter" description:"Update counter"`
}
```

### 2. Register SSE Endpoint with AsyncAPI Documentation

```go
router.SSE("/events", sseHandler,
    // General route metadata
    forge.WithName("sse-events"),
    forge.WithTags("streaming"),
    forge.WithSummary("Server-Sent Events stream"),
    
    // AsyncAPI event documentation
    forge.WithSSEMessage("connected", ConnectedEvent{}),
    forge.WithSSEMessage("update", UpdateEvent{}),
    forge.WithSSEMessage("milestone", MilestoneEvent{}),
    forge.WithAsyncAPITags("real-time", "notifications"),
)
```

The `router.SSE()` method automatically sets these headers:
- `Content-Type: text/event-stream`
- `Cache-Control: no-cache`
- `Connection: keep-alive`
- `X-Accel-Buffering: no`

### 3. Send Typed Events

```go
// Send event matching the documented schema
updateData := UpdateEvent{
    Timestamp: time.Now().Unix(),
    Message:   "Periodic update",
    Counter:   42,
}
ctx.WriteSSE("update", updateData) // Automatically marshals to JSON
```

You can also send strings directly:
```go
ctx.WriteSSE("event-name", "Simple string message")
```

### 4. Handle Client Disconnects

```go
for {
    select {
    case <-ctx.Context().Done():
        return nil // Client disconnected
    case <-ticker.C:
        ctx.WriteSSE("update", data)
    }
}
```

### 5. Manual Flush (Optional)

While `WriteSSE()` auto-flushes, you can manually flush if needed:

```go
ctx.Flush() // Manually flush buffered data
```

## Comparison with Low-Level API

### Using `router.SSE()` (Recommended)

```go
router.SSE("/events", func(ctx forge.Context) error {
    // Headers already set automatically
    return ctx.WriteSSE("message", "Hello")
})
```

### Using `router.EventStream()` (Low-Level)

```go
router.EventStream("/events", func(ctx forge.Context, stream forge.Stream) error {
    // More control, but requires manual stream management
    return stream.SendJSON("message", data)
})
```

Use `router.SSE()` for most cases. Use `router.EventStream()` when you need low-level control over the stream lifecycle.

## Production Considerations

1. **Timeouts**: Add route-level timeouts for long-running connections
   ```go
   router.SSE("/events", handler, forge.WithTimeout(5*time.Minute))
   ```

2. **Rate Limiting**: Consider rate-limiting SSE endpoints to prevent abuse

3. **Load Balancing**: Ensure your load balancer supports long-lived connections and SSE

4. **Reverse Proxies**: 
   - Nginx: Set `proxy_buffering off;` and `proxy_read_timeout 3600s;`
   - Apache: Use `SetEnv proxy-nokeepalive 1`

5. **Error Handling**: Always check errors from `WriteSSE()` and handle disconnects gracefully

## References

- [Server-Sent Events Specification](https://html.spec.whatwg.org/multipage/server-sent-events.html)
- [MDN: Server-Sent Events](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events)

