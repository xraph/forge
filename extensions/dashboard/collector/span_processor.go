package collector

import (
	"context"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	// spanQueueSize is how many finished spans wait for the drain goroutine.
	// When it is full, OnEnd drops the span and counts it.
	spanQueueSize = 1024

	// maxSpanAttributes and maxSpanEvents bound one stored span.
	maxSpanAttributes = 64
	maxSpanEvents     = 32
)

// SpanProcessor feeds a TraceStore from the app's OpenTelemetry spans. OnEnd
// only checks the store's gate and queues the span; conversion and storage
// run on one goroutine, so the request path never waits on the store.
//
// It wants unsampled spans while the store's gate is open, so the dashboard
// shows every request while someone is looking, whatever the sample ratio.
type SpanProcessor struct {
	store    *TraceStore
	basePath string

	queue  chan sdktrace.ReadOnlySpan
	flush  chan chan struct{}
	stop   chan struct{}
	done   chan struct{}
	closed atomic.Bool
	once   sync.Once
}

var _ sdktrace.SpanProcessor = (*SpanProcessor)(nil)

// NewSpanProcessor starts a processor that writes into store. basePath is the
// dashboard's base path, used to leave its own plumbing out.
func NewSpanProcessor(store *TraceStore, basePath string) *SpanProcessor {
	return newSpanProcessor(store, basePath, true)
}

func newSpanProcessor(store *TraceStore, basePath string, start bool) *SpanProcessor {
	p := &SpanProcessor{
		store:    store,
		basePath: basePath,
		queue:    make(chan sdktrace.ReadOnlySpan, spanQueueSize),
		flush:    make(chan chan struct{}),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}

	if start {
		go p.run()
	} else {
		close(p.done)
	}

	return p
}

// WantsUnsampled reports whether the dashboard wants spans the sampler drops.
func (p *SpanProcessor) WantsUnsampled() bool {
	return !p.closed.Load() && p.store.Accepting()
}

// OnStart does nothing; the processor only reads finished spans.
func (p *SpanProcessor) OnStart(context.Context, sdktrace.ReadWriteSpan) {}

// OnEnd queues s for storage without blocking.
func (p *SpanProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	if p.closed.Load() || !p.store.Accepting() {
		return
	}

	select {
	case p.queue <- s:
	default:
		p.store.droppedSpans.Add(1)
	}
}

// ForceFlush returns once every span queued before the call is stored.
func (p *SpanProcessor) ForceFlush(ctx context.Context) error {
	if p.closed.Load() {
		return nil
	}

	ack := make(chan struct{})

	select {
	case p.flush <- ack:
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case <-ack:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Shutdown stores what is queued and stops the goroutine. It is safe to call
// more than once; the dashboard and the app's provider both call it.
func (p *SpanProcessor) Shutdown(ctx context.Context) error {
	p.once.Do(func() {
		p.closed.Store(true)
		close(p.stop)
	})

	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *SpanProcessor) run() {
	defer close(p.done)

	for {
		select {
		case s := <-p.queue:
			p.ingest(s)
		case ack := <-p.flush:
			p.drain()
			close(ack)
		case <-p.stop:
			p.drain()

			return
		}
	}
}

func (p *SpanProcessor) drain() {
	for {
		select {
		case s := <-p.queue:
			p.ingest(s)
		default:
			return
		}
	}
}

func (p *SpanProcessor) ingest(s sdktrace.ReadOnlySpan) {
	v := convertSpan(s)
	if v == nil || SkipTracePath(v.Attributes["url.path"], p.basePath) {
		return
	}

	p.store.AddSpan(v)
}

// convertSpan builds the stored view of a finished span. It returns nil for a
// span without a valid context.
func convertSpan(s sdktrace.ReadOnlySpan) *SpanView {
	sc := s.SpanContext()
	if !sc.IsValid() {
		return nil
	}

	v := &SpanView{
		TraceID:    sc.TraceID().String(),
		SpanID:     sc.SpanID().String(),
		Name:       Truncate(s.Name(), MaxAttrValueLen),
		Kind:       convertKind(s.SpanKind()),
		Status:     SpanStatusOK,
		StartTime:  s.StartTime(),
		EndTime:    s.EndTime(),
		Duration:   s.EndTime().Sub(s.StartTime()),
		Attributes: convertAttributes(s.Attributes()),
		Events:     convertEvents(s.Events()),
	}

	// A remote parent is in another process. In this store the span is a
	// root; the caller's span ID is kept for reference.
	if parent := s.Parent(); parent.IsValid() {
		if parent.IsRemote() {
			v.Attributes["parent.remote_span_id"] = parent.SpanID().String()
		} else {
			v.ParentSpanID = parent.SpanID().String()
		}
	}

	if st := s.Status(); st.Code == codes.Error {
		v.Status = SpanStatusError

		if st.Description != "" {
			v.Attributes["error"] = Truncate(st.Description, MaxAttrValueLen)
		}
	}

	if _, ok := v.Attributes["protocol"]; !ok {
		if proto := spanProtocol(v.Attributes); proto != "" {
			v.Attributes["protocol"] = proto
		}
	}

	return v
}

func convertKind(k trace.SpanKind) SpanKind {
	switch k {
	case trace.SpanKindInternal:
		return SpanKindInternal
	case trace.SpanKindServer:
		return SpanKindServer
	case trace.SpanKindClient:
		return SpanKindClient
	case trace.SpanKindProducer:
		return SpanKindProducer
	case trace.SpanKindConsumer:
		return SpanKindConsumer
	case trace.SpanKindUnspecified:
		return SpanKindUnspecified
	}

	return SpanKindUnspecified
}

func convertAttributes(kvs []attribute.KeyValue) map[string]string {
	out := make(map[string]string, min(len(kvs), maxSpanAttributes))

	for _, kv := range kvs {
		if len(out) == maxSpanAttributes {
			break
		}

		out[string(kv.Key)] = Truncate(kv.Value.String(), MaxAttrValueLen)
	}

	return out
}

func convertEvents(events []sdktrace.Event) []SpanEventView {
	n := min(len(events), maxSpanEvents)
	out := make([]SpanEventView, 0, n)

	for _, e := range events[:n] {
		out = append(out, SpanEventView{
			Name:       Truncate(e.Name, MaxAttrValueLen),
			Timestamp:  e.Time,
			Attributes: convertAttributes(e.Attributes),
		})
	}

	return out
}

// spanProtocol names the protocol shown in the trace list, from the
// attributes forge and OTel instrumentation set.
func spanProtocol(attrs map[string]string) string {
	switch attrs["forge.stream.kind"] {
	case "sse":
		return "SSE"
	case "websocket":
		return "WS"
	case "webtransport":
		return "WebTransport"
	}

	switch {
	case attrs["rpc.system"] == "grpc", attrs["rpc.system.name"] == "grpc":
		return "gRPC"
	case attrs["rpc.system"] != "", attrs["rpc.system.name"] != "":
		return "RPC"
	case attrs["messaging.system"] != "":
		return "Event"
	case attrs["http.request.method"] != "":
		return "REST"
	}

	return ""
}
