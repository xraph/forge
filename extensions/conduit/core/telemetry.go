package core

import (
	"context"

	gometrics "github.com/xraph/go-utils/metrics"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge/messaging"
)

// RegisterMetrics records messaging.client.operation.duration and
// messaging.process.duration into m. Call it before Start; the forge
// extension does.
func (r *Runtime) RegisterMetrics(m gometrics.Metrics) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running || r.closed {
		return ErrConflict
	}

	r.metrics = m

	return nil
}

// buildTelemetry makes one instrumenter per configured provider, named by the
// provider's Name. Start calls it with r.mu held, before any worker or RPC
// server starts, so later reads need no lock.
func (r *Runtime) buildTelemetry() {
	r.telemetry = make(map[string]*messaging.Telemetry, len(r.providers))

	for key, provider := range r.providers {
		var opts []messaging.Option
		if r.metrics != nil {
			opts = append(opts, messaging.WithMetrics(r.metrics))
		}

		r.telemetry[key] = messaging.New(provider.Name(), opts...)
	}
}

// telemetryFor returns the instrumenter for a provider key. A nil result is
// valid and still propagates.
func (r *Runtime) telemetryFor(provider string) *messaging.Telemetry { return r.telemetry[provider] }

// creationLinks links a send to the create span recorded in the message's
// headers, when it is valid and not already ctx's span.
func creationLinks(ctx context.Context, headers map[string]string) []messaging.StartOption {
	created := trace.SpanContextFromContext(messaging.Extract(context.Background(), propagation.MapCarrier(headers)))
	if !created.IsValid() || created.SpanID() == trace.SpanContextFromContext(ctx).SpanID() {
		return nil
	}

	return []messaging.StartOption{messaging.WithLinks(trace.Link{SpanContext: created})}
}
