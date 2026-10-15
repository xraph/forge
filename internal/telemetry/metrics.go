package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/xraph/forge/internal/logger"
)

// MetricsPusher pushes metrics over OTLP/HTTP on an interval.
type MetricsPusher struct {
	mp       *sdkmetric.MeterProvider
	settings MetricsSettings
}

// NewMetricsPusher builds the OTLP exporter and a periodic reader over
// producer. The MeterProvider that holds them is never installed as the OTel
// global, so only what producer returns is pushed and a push always carries
// the series a scrape shows.
//
// otlpmetrichttp reads its own OTEL_EXPORTER_OTLP_METRICS_* settings for what
// is not passed here, such as the timeout, compression and TLS certificates.
func NewMetricsPusher(ctx context.Context, s MetricsSettings, res *resource.Resource,
	producer sdkmetric.Producer, log logger.Logger) (*MetricsPusher, error) {
	if err := validateEndpoint(s.Endpoint); err != nil {
		return nil, err
	}

	exp, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithEndpointURL(s.Endpoint),
		otlpmetrichttp.WithHeaders(s.Headers),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: create OTLP metrics exporter for %q: %w", s.Endpoint, err)
	}

	interval := s.Interval
	if interval <= 0 {
		interval = defaultMetricsInterval
	}

	reader := sdkmetric.NewPeriodicReader(exp, sdkmetric.WithProducer(producer), sdkmetric.WithInterval(interval))

	opts := []sdkmetric.Option{sdkmetric.WithReader(reader)}
	if res != nil {
		opts = append(opts, sdkmetric.WithResource(res))
	}

	// Export failures (a backend down, a bad key) log at most once a minute,
	// even when tracing is off and hasn't installed this already.
	otel.SetErrorHandler(newRateLimitedErrorHandler(log, time.Minute))

	return &MetricsPusher{mp: sdkmetric.NewMeterProvider(opts...), settings: s}, nil
}

// Settings returns the resolved settings the pusher was built with.
func (p *MetricsPusher) Settings() MetricsSettings { return p.settings }

// Shutdown collects and exports one last time, waiting at most five seconds
// or until ctx is done.
func (p *MetricsPusher) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, shutdownCap)
	defer cancel()

	return p.mp.Shutdown(ctx)
}
