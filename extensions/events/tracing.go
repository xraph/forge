package events

import (
	"context"
	"maps"

	"go.opentelemetry.io/otel/propagation"

	"github.com/xraph/forge/extensions/events/core"
	"github.com/xraph/forge/messaging"
)

// metadataCarrier adapts Event.Metadata to a TextMapCarrier. Values that
// aren't strings are ignored.
type metadataCarrier map[string]any

func (c metadataCarrier) Get(key string) string {
	s, _ := c[key].(string)

	return s
}

func (c metadataCarrier) Set(key, value string) { c[key] = value }

func (c metadataCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}

	return keys
}

// withTraceMetadata returns a copy of event whose Metadata carries ctx's trace
// context. The caller's event and its map are never modified, which matters
// because the memory broker shares Metadata between every handler.
func withTraceMetadata(ctx context.Context, event *core.Event) core.Event {
	added := propagation.MapCarrier{}
	messaging.Inject(ctx, added)

	out := *event
	if len(added) == 0 {
		return out
	}

	md := make(map[string]any, len(event.Metadata)+len(added))
	maps.Copy(md, event.Metadata)

	for k, v := range added {
		md[k] = v
	}

	out.Metadata = md

	return out
}

func (eb *EventBusImpl) newTelemetry(broker string) *messaging.Telemetry {
	opts := []messaging.Option{messaging.WithTracing(eb.config.EnableTracing)}
	if eb.config.EnableMetrics && eb.metrics != nil {
		opts = append(opts, messaging.WithMetrics(eb.metrics))
	}

	return messaging.New(broker, opts...)
}

// telemetryFor returns the broker's instrumenter, or nil (which still
// propagates) for a broker registered before tracing existed.
func (eb *EventBusImpl) telemetryFor(broker string) *messaging.Telemetry {
	v, _ := eb.telemetry.Load(broker)
	t, _ := v.(*messaging.Telemetry)

	return t
}

// tracedHandler runs a broker delivery inside a consumer span. Brokers match
// handlers by Name, so unsubscribe and rollback see no difference.
type tracedHandler struct {
	core.EventHandler

	telemetry *messaging.Telemetry
}

// LogicalName forwards to the wrapped handler, which redis streams uses for
// its consumer group, and falls back to Name as redis streams does.
func (h *tracedHandler) LogicalName() string {
	if logical, ok := h.EventHandler.(interface{ LogicalName() string }); ok {
		return logical.LogicalName()
	}

	return h.Name()
}

func (h *tracedHandler) Handle(ctx context.Context, event *core.Event) error {
	return h.telemetry.Process(ctx, metadataCarrier(event.Metadata), event.Type, "process",
		func(ctx context.Context) error { return h.EventHandler.Handle(ctx, event) })
}

func (eb *EventBusImpl) traced(broker string, handler core.EventHandler) core.EventHandler {
	return &tracedHandler{EventHandler: handler, telemetry: eb.telemetryFor(broker)}
}
