package telemetry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/xraph/forge/internal/logger"
)

func newTestProvider(t *testing.T, s Settings) *Provider {
	t.Helper()

	p, err := NewProvider(t.Context(), s, resource.Empty(), logger.NewNoopLogger())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = p.Shutdown(t.Context()) })

	return p
}

func TestProviderDisabledIsNoop(t *testing.T) {
	p := newTestProvider(t, Settings{Enabled: false})

	_, span := p.TracerProvider().Tracer("t").Start(t.Context(), "x")
	if span.SpanContext().IsValid() {
		t.Fatal("disabled provider produced a valid span")
	}

	if err := p.RegisterSpanProcessor(sdktrace.NewSimpleSpanProcessor(tracetest.NewInMemoryExporter())); !errors.Is(err, ErrTracingDisabled) {
		t.Fatalf("RegisterSpanProcessor err = %v, want ErrTracingDisabled", err)
	}
}

func TestProviderWithoutEndpointStillRecords(t *testing.T) {
	p := newTestProvider(t, Settings{Enabled: true, SamplerName: defaultSamplerName, SampleRatio: 1})

	exp := tracetest.NewInMemoryExporter()
	if err := p.RegisterSpanProcessor(sdktrace.NewSimpleSpanProcessor(exp)); err != nil {
		t.Fatal(err)
	}

	_, span := p.TracerProvider().Tracer("t").Start(t.Context(), "work")
	span.End()

	if got := exp.GetSpans(); len(got) != 1 || got[0].Name != "work" {
		t.Fatalf("spans = %v", got)
	}
}

func TestProviderExportsOverOTLPHTTP(t *testing.T) {
	var (
		mu       sync.Mutex
		gotPath  string
		gotAuth  string
		gotCType string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)

		mu.Lock()
		gotPath, gotAuth, gotCType = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p, err := NewProvider(t.Context(), Settings{
		Enabled:     true,
		Endpoint:    srv.URL + "/v1/traces",
		Headers:     map[string]string{"Authorization": "Bearer t0ken"},
		SamplerName: defaultSamplerName,
		SampleRatio: 1,
	}, resource.Empty(), logger.NewNoopLogger())
	if err != nil {
		t.Fatal(err)
	}

	_, span := p.TracerProvider().Tracer("t").Start(t.Context(), "shipped")
	span.End()

	if err := p.Shutdown(t.Context()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if gotPath != "/v1/traces" || gotAuth != "Bearer t0ken" || gotCType != "application/x-protobuf" {
		t.Fatalf("path %q auth %q content-type %q", gotPath, gotAuth, gotCType)
	}
}

func TestProviderShutdownDisabledIsNil(t *testing.T) {
	p := newTestProvider(t, Settings{Enabled: false})
	if err := p.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestProviderRejectsEndpointsThatAreNotHTTPURLs(t *testing.T) {
	for _, ep := range []string{"localhost:4318", "://bad"} {
		_, err := NewProvider(t.Context(), Settings{Enabled: true, Endpoint: ep}, resource.Empty(), logger.NewNoopLogger())
		if !errors.Is(err, ErrInvalidEndpoint) {
			t.Errorf("NewProvider(%q) err = %v, want ErrInvalidEndpoint", ep, err)
		}
	}

	p, err := NewProvider(t.Context(), Settings{Enabled: true, Endpoint: "http://localhost:4318/v1/traces"}, resource.Empty(), logger.NewNoopLogger())
	if err != nil {
		t.Fatalf("NewProvider(valid URL) err = %v", err)
	}

	_ = p.Shutdown(t.Context())
}

type wantingProcessor struct {
	sdktrace.SpanProcessor

	want atomic.Bool
}

func (w *wantingProcessor) WantsUnsampled() bool { return w.want.Load() }

func TestProviderRecordOnlySpansReachWantingProcessorButNotExporter(t *testing.T) {
	p := newTestProvider(t, Settings{Enabled: true, SamplerName: defaultSamplerName, SampleRatio: 0})

	exported := tracetest.NewInMemoryExporter()
	if err := p.RegisterSpanProcessor(sdktrace.NewSimpleSpanProcessor(exported)); err != nil {
		t.Fatal(err)
	}

	seen := &spanCollector{}
	wp := &wantingProcessor{SpanProcessor: seen}
	wp.want.Store(true)

	if err := p.RegisterSpanProcessor(wp); err != nil {
		t.Fatal(err)
	}

	_, span := p.TracerProvider().Tracer("t").Start(t.Context(), "unsampled-but-watched")
	span.End()

	if n := len(seen.names()); n != 1 {
		t.Fatalf("wanting processor saw %d spans, want 1", n)
	}

	if n := len(exported.GetSpans()); n != 0 {
		t.Fatalf("exporter got %d RecordOnly spans, want 0", n)
	}
}

// spanCollector records the names of every span that ends, sampled or not.
type spanCollector struct {
	mu   sync.Mutex
	list []string
}

func (c *spanCollector) OnStart(context.Context, sdktrace.ReadWriteSpan) {}
func (c *spanCollector) OnEnd(s sdktrace.ReadOnlySpan) {
	c.mu.Lock()
	c.list = append(c.list, s.Name())
	c.mu.Unlock()
}
func (c *spanCollector) Shutdown(context.Context) error   { return nil }
func (c *spanCollector) ForceFlush(context.Context) error { return nil }

func (c *spanCollector) names() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return slices.Clone(c.list)
}
