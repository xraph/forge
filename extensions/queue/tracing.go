package queue

import (
	"context"

	"go.opentelemetry.io/otel/propagation"

	"github.com/xraph/forge"
	"github.com/xraph/forge/messaging"
)

// messagingSystem names the driver for messaging.system.
func messagingSystem(driver string) string {
	if driver == "inmemory" {
		return "memory"
	}

	return driver
}

func newTelemetry(cfg Config, m forge.Metrics) *messaging.Telemetry {
	opts := []messaging.Option{messaging.WithTracing(cfg.EnableTracing)}
	if cfg.EnableMetrics && m != nil {
		opts = append(opts, messaging.WithMetrics(m))
	}

	return messaging.New(messagingSystem(cfg.Driver), opts...)
}

// traceHandler runs each delivery inside a consumer span that continues the
// publisher's trace from the message headers.
func (s *QueueService) traceHandler(queue string, handler MessageHandler) MessageHandler {
	return func(ctx context.Context, msg Message) error {
		return s.telemetry.Process(ctx, propagation.MapCarrier(msg.Headers), queue, "process",
			func(ctx context.Context) error { return handler(ctx, msg) })
	}
}
