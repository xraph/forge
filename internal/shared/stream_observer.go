package shared

// StreamObserver records the life of stream connections. The router calls
// Opened once a stream is established and uses the returned handle until the
// stream ends. Implementations must be safe for concurrent use.
type StreamObserver interface {
	Opened(kind RouteKind, route string) StreamHandle
}

// StreamHandle receives one stream's events. Sent and Received may be called
// from several goroutines at once. Closed is called exactly once.
type StreamHandle interface {
	Sent()
	Received()
	Closed(err error)
}

type noopStreamHandle struct{}

func (noopStreamHandle) Sent()        {}
func (noopStreamHandle) Received()    {}
func (noopStreamHandle) Closed(error) {}

// NoopStreamHandle does nothing. The router uses it when no observer is set.
var NoopStreamHandle StreamHandle = noopStreamHandle{}
