package telemetry

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/xraph/forge/internal/logger"
)

var errLogsPipelineShutDown = errors.New("telemetry: logs pipeline is shut down")

// LogsPipeline ships the lines of attached loggers over OTLP/HTTP. Lines are
// queued by a batch processor, so logging never waits on the collector; a full
// queue drops the oldest lines.
type LogsPipeline struct {
	settings LogsSettings
	lp       *sdklog.LoggerProvider
	sink     *logSink

	mu       sync.Mutex
	detaches []func()
	closed   bool
}

// NewLogsPipeline checks the endpoint and builds the exporter and the logger
// provider. Nothing is exported until a logger is attached.
//
// otlploghttp reads its own OTEL_EXPORTER_OTLP_LOGS_* settings for what is not
// passed here, such as the timeout, compression and TLS certificates.
func NewLogsPipeline(ctx context.Context, s LogsSettings, res *resource.Resource,
	l logger.Logger) (*LogsPipeline, error) {
	if err := validateEndpoint(s.Endpoint); err != nil {
		return nil, err
	}

	exp, err := otlploghttp.New(ctx,
		otlploghttp.WithEndpointURL(s.Endpoint),
		otlploghttp.WithHeaders(s.Headers),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: create OTLP logs exporter for %q: %w", s.Endpoint, err)
	}

	opts := []sdklog.LoggerProviderOption{sdklog.WithProcessor(sdklog.NewBatchProcessor(exp))}
	if res != nil {
		opts = append(opts, sdklog.WithResource(res))
	}

	// Export failures log at most once a minute. That warning goes through
	// the same logger and is exported too, which stays bounded.
	if l != nil {
		otel.SetErrorHandler(newRateLimitedErrorHandler(l, time.Minute))
	}

	lp := sdklog.NewLoggerProvider(opts...)

	return &LogsPipeline{settings: s, lp: lp, sink: newLogSink(lp, s.Level)}, nil
}

// Attach sends every line of l's logger tree to the pipeline. The go-utils
// error comes back unchanged, so errors.Is(err, logger.ErrSinksUnsupported)
// tells a logger that can't carry sinks.
func (p *LogsPipeline) Attach(l logger.Logger) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return errLogsPipelineShutDown
	}

	detach, err := logger.AttachSink(l, p.sink)
	if err != nil {
		return err
	}

	p.detaches = append(p.detaches, detach)

	return nil
}

// Flush exports queued lines, waiting at most two seconds.
func (p *LogsPipeline) Flush() {
	if p == nil {
		return
	}

	p.sink.Flush()
}

// Settings returns the resolved settings the pipeline was built with.
func (p *LogsPipeline) Settings() LogsSettings {
	if p == nil {
		return LogsSettings{}
	}

	return p.settings
}

// Shutdown detaches from every logger, then exports what is queued and stops
// the provider, waiting at most five seconds or until ctx is done. A nil
// pipeline's Shutdown does nothing.
func (p *LogsPipeline) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}

	p.mu.Lock()
	p.closed = true
	detaches := p.detaches
	p.detaches = nil
	p.mu.Unlock()

	for _, detach := range detaches {
		detach()
	}

	ctx, cancel := context.WithTimeout(ctx, shutdownCap)
	defer cancel()

	return p.lp.Shutdown(ctx)
}
