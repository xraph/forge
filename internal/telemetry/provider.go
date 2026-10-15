package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/xraph/forge/internal/logger"
)

// ErrTracingDisabled is returned by RegisterSpanProcessor when tracing is off.
var ErrTracingDisabled = errors.New("telemetry: tracing is disabled")

// ErrInvalidEndpoint is wrapped by NewProvider when the OTLP endpoint is not
// an http or https URL with a host.
var ErrInvalidEndpoint = errors.New("telemetry: invalid OTLP endpoint")

const shutdownCap = 5 * time.Second

// Provider owns the app's tracer provider.
type Provider struct {
	sdk      *sdktrace.TracerProvider // nil when tracing is disabled
	tp       trace.TracerProvider
	settings Settings
	resource *resource.Resource

	// wants holds WantsUnsampled of each processor that has one. The sampler
	// reads it on every dropped span; registration copies and swaps it.
	wants   atomic.Pointer[[]func() bool]
	wantsMu sync.Mutex
}

// NewProvider builds the tracer provider and installs it, with the
// propagators, as the OTel globals. With no endpoint the provider records
// spans but exports nothing; processors added later still see them.
func NewProvider(ctx context.Context, s Settings, res *resource.Resource, log logger.Logger) (*Provider, error) {
	p := &Provider{settings: s, resource: res}

	otel.SetTextMapPropagator(buildPropagator(s.Propagators))

	if !s.Enabled {
		p.tp = noop.NewTracerProvider()
		otel.SetTracerProvider(p.tp)

		return p, nil
	}

	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(NewForgeSampler(BuildSampler(s), SamplerOff(s), &p.wants)),
	}

	if s.Endpoint != "" {
		if err := validateEndpoint(s.Endpoint); err != nil {
			return nil, err
		}

		exp, err := otlptracehttp.New(ctx,
			otlptracehttp.WithEndpointURL(s.Endpoint),
			otlptracehttp.WithHeaders(s.Headers),
		)
		if err != nil {
			return nil, fmt.Errorf("telemetry: create OTLP exporter for %q: %w", s.Endpoint, err)
		}

		opts = append(opts, sdktrace.WithBatcher(exp))
	}

	p.sdk = sdktrace.NewTracerProvider(opts...)
	p.tp = p.sdk

	otel.SetTracerProvider(p.tp)
	otel.SetErrorHandler(newRateLimitedErrorHandler(log, time.Minute))

	return p, nil
}

// TracerProvider returns the provider spans should be started from.
func (p *Provider) TracerProvider() trace.TracerProvider { return p.tp }

// Settings returns the resolved settings the provider was built with.
func (p *Provider) Settings() Settings { return p.settings }

// Resource returns the shared resource.
func (p *Provider) Resource() *resource.Resource { return p.resource }

// unsampledWanter is a processor that also wants spans the sampler drops.
type unsampledWanter interface {
	WantsUnsampled() bool
}

// RegisterSpanProcessor adds a span processor after construction. A processor
// with a WantsUnsampled method also receives spans the sampler would drop, for
// as long as the method returns true; those spans are never exported. It
// returns ErrTracingDisabled when tracing is off.
func (p *Provider) RegisterSpanProcessor(sp sdktrace.SpanProcessor) error {
	if p.sdk == nil {
		return ErrTracingDisabled
	}

	if w, ok := sp.(unsampledWanter); ok {
		p.wantsMu.Lock()

		var next []func() bool
		if cur := p.wants.Load(); cur != nil {
			next = append(next, *cur...)
		}

		next = append(next, w.WantsUnsampled)
		p.wants.Store(&next)

		p.wantsMu.Unlock()
	}

	p.sdk.RegisterSpanProcessor(sp)

	return nil
}

// Shutdown flushes queued spans, waiting at most five seconds or until ctx
// is done.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.sdk == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, shutdownCap)
	defer cancel()

	return p.sdk.Shutdown(ctx)
}

// validateEndpoint rejects what otlptracehttp would accept and then export
// nowhere useful: a schemeless "localhost:4318" parses with "localhost" as
// the scheme and an empty host, and an unparsable URL is only logged.
func validateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
		return nil
	}

	return fmt.Errorf("telemetry: OTLP endpoint %q must be an http or https URL: %w", endpoint, ErrInvalidEndpoint)
}

func buildPropagator(names []string) propagation.TextMapPropagator {
	var list []propagation.TextMapPropagator

	for _, n := range names {
		switch n {
		case "tracecontext":
			list = append(list, propagation.TraceContext{})
		case "baggage":
			list = append(list, propagation.Baggage{})
		}
	}

	return propagation.NewCompositeTextMapPropagator(list...)
}
