package telemetry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/xraph/forge/internal/logger"
)

// MetricsPusher pushes metrics over OTLP/HTTP on an interval between Start
// and Shutdown. It can be started again after a Shutdown, and the same
// producer serves every run.
type MetricsPusher struct {
	settings MetricsSettings
	res      *resource.Resource
	producer sdkmetric.Producer
	log      logger.Logger

	mu sync.Mutex
	mp *sdkmetric.MeterProvider // nil when not pushing
}

// NewMetricsPusher checks the endpoint and keeps what Start needs. Nothing is
// exported until Start.
func NewMetricsPusher(s MetricsSettings, res *resource.Resource, producer sdkmetric.Producer,
	log logger.Logger) (*MetricsPusher, error) {
	if err := validateEndpoint(s.Endpoint); err != nil {
		return nil, err
	}

	if s.Interval <= 0 {
		s.Interval = defaultMetricsInterval
	}

	return &MetricsPusher{settings: s, res: res, producer: producer, log: log}, nil
}

// Start builds the OTLP exporter and a periodic reader over the producer, and
// pushes every interval until Shutdown. A pusher that is already running is
// left as it is. The MeterProvider that holds them is never installed as the
// OTel global, so only what the producer returns is pushed and a push always
// carries the series a scrape shows.
//
// otlpmetrichttp reads its own OTEL_EXPORTER_OTLP_METRICS_* settings for what
// is not passed here, such as the timeout, compression and TLS certificates.
func (p *MetricsPusher) Start(ctx context.Context) error {
	if p == nil {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.mp != nil {
		return nil
	}

	exp, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithEndpointURL(p.settings.Endpoint),
		otlpmetrichttp.WithHeaders(p.settings.Headers),
	)
	if err != nil {
		return fmt.Errorf("telemetry: create OTLP metrics exporter for %q: %w", p.settings.Endpoint, err)
	}

	reader := sdkmetric.NewPeriodicReader(exp,
		sdkmetric.WithProducer(p.producer), sdkmetric.WithInterval(p.settings.Interval))

	opts := []sdkmetric.Option{sdkmetric.WithReader(reader)}
	if p.res != nil {
		opts = append(opts, sdkmetric.WithResource(p.res))
	}

	// Export failures (a backend down, a bad key) log at most once a minute,
	// even when tracing is off and hasn't installed this already.
	otel.SetErrorHandler(newRateLimitedErrorHandler(p.log, time.Minute))

	p.mp = sdkmetric.NewMeterProvider(opts...)

	return nil
}

// Settings returns the resolved settings the pusher was built with.
func (p *MetricsPusher) Settings() MetricsSettings {
	if p == nil {
		return MetricsSettings{}
	}

	return p.settings
}

// Shutdown collects and exports one last time and stops pushing, waiting at
// most five seconds or until ctx is done. It does nothing when the pusher
// isn't running, and Start may be called again afterwards.
func (p *MetricsPusher) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.mp == nil {
		return nil
	}

	mp := p.mp
	p.mp = nil

	ctx, cancel := context.WithTimeout(ctx, shutdownCap)
	defer cancel()

	return mp.Shutdown(ctx)
}
