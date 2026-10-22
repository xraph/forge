package forge

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	gulog "github.com/xraph/go-utils/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/proto"

	"github.com/xraph/forge/internal/logger"
)

// logsReceiver records OTLP log posts on /v1/logs and answers every other
// path (traces, metrics) with 200 so the other exporters stay quiet.
// ExportLogsServiceRequest decodes as LogsData: same field 1.
type logsReceiver struct {
	*httptest.Server

	mu    sync.Mutex
	got   []*logspb.LogsData
	paths []string
}

func newLogsReceiver(t *testing.T) *logsReceiver {
	t.Helper()

	r := &logsReceiver{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)

		r.mu.Lock()
		r.paths = append(r.paths, req.URL.Path)

		if req.URL.Path == "/v1/logs" {
			var ld logspb.LogsData
			if err := proto.Unmarshal(body, &ld); err == nil {
				r.got = append(r.got, &ld)
			}
		}
		r.mu.Unlock()

		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(r.Close)

	return r
}

// receivedLog is one exported record with the resource it came under.
type receivedLog struct {
	record      *logspb.LogRecord
	serviceName string
}

func (r *logsReceiver) records() []receivedLog {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []receivedLog

	for _, ld := range r.got {
		for _, rl := range ld.GetResourceLogs() {
			var name string

			for _, kv := range rl.GetResource().GetAttributes() {
				if kv.GetKey() == "service.name" {
					name = kv.GetValue().GetStringValue()
				}
			}

			for _, sl := range rl.GetScopeLogs() {
				for _, lr := range sl.GetLogRecords() {
					out = append(out, receivedLog{record: lr, serviceName: name})
				}
			}
		}
	}

	return out
}

func (r *logsReceiver) bodies() []string {
	recs := r.records()
	out := make([]string, 0, len(recs))

	for _, rec := range recs {
		out = append(out, rec.record.GetBody().GetStringValue())
	}

	return out
}

func (r *logsReceiver) sawPath(path string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Contains(r.paths, path)
}

// lockedBuffer is a bytes.Buffer the logger and the export goroutine can
// both write to.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func clearLogsEnv(t *testing.T) {
	t.Helper()

	clearOTELEnv(t)

	for _, k := range []string{
		"OTEL_LOGS_EXPORTER", "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "OTEL_EXPORTER_OTLP_LOGS_HEADERS",
	} {
		t.Setenv(k, "")
	}
}

// jsonLogger is a real go-utils logger. Under go test the default app logger
// is the noop one, which can't carry a sink.
func jsonLogger(out io.Writer) Logger {
	return gulog.New(gulog.Config{Format: gulog.FormatJSON, Output: out})
}

// The discriminator: the IDs on the exported record must be the live server
// span's, not ones made up anywhere else.
func TestAppLogLinesCarryServerSpanIDs(t *testing.T) {
	clearLogsEnv(t)

	recv := newLogsReceiver(t)

	a := New(WithAppName("logged"), WithAppLogger(jsonLogger(io.Discard)),
		WithAppLogsExportConfig(LogsExportConfig{Endpoint: recv.URL + "/v1/logs"}))

	exp := tracetest.NewInMemoryExporter()
	if err := RegisterSpanProcessor(a, sdktrace.NewSimpleSpanProcessor(exp)); err != nil {
		t.Fatal(err)
	}

	_ = a.Router().GET("/users/:id", func(c Context) error {
		a.Logger().WithContext(c.Request().Context()).Info("in handler")

		return c.String(http.StatusOK, "ok")
	})

	if err := a.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	serve(t, a, http.MethodGet, "/users/7", "")

	// Read the spans before Stop: shutting the tracer down resets the
	// in-memory exporter.
	var servers []tracetest.SpanStub

	for _, s := range exp.GetSpans() {
		if s.SpanKind == trace.SpanKindServer {
			servers = append(servers, s)
		}
	}

	if err := a.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	if len(servers) != 1 {
		t.Fatalf("got %d server spans, want 1", len(servers))
	}

	span := servers[0]

	var found *receivedLog

	recs := recv.records()
	for i := range recs {
		if recs[i].record.GetBody().GetStringValue() == "in handler" {
			found = &recs[i]

			break
		}
	}

	if found == nil {
		t.Fatalf("no record with body %q in %v", "in handler", recv.bodies())
	}

	traceID := span.SpanContext.TraceID()
	spanID := span.SpanContext.SpanID()

	if !bytes.Equal(found.record.GetTraceId(), traceID[:]) {
		t.Errorf("record trace ID = %x, want the server span's %s", found.record.GetTraceId(), traceID)
	}

	if !bytes.Equal(found.record.GetSpanId(), spanID[:]) {
		t.Errorf("record span ID = %x, want the server span's %s", found.record.GetSpanId(), spanID)
	}

	spanService, ok := span.Resource.Set().Value("service.name")
	if !ok {
		t.Fatal("the server span's resource has no service.name")
	}

	if found.serviceName != spanService.AsString() {
		t.Errorf("record service.name = %q, want the span's %q", found.serviceName, spanService.AsString())
	}
}

func TestAppLogsExportFromGenericEndpoint(t *testing.T) {
	clearLogsEnv(t)

	recv := newLogsReceiver(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", recv.URL)

	a := New(WithAppName("generic"), WithAppLogger(jsonLogger(io.Discard)))

	if err := a.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := a.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	if n := len(recv.records()); n == 0 {
		t.Fatal("no log records received from the generic endpoint")
	}

	if !recv.sawPath("/v1/logs") {
		t.Fatalf("paths = %v, want /v1/logs", recv.paths)
	}
}

func TestAppLogsExportOffWithExporterNone(t *testing.T) {
	clearLogsEnv(t)

	recv := newLogsReceiver(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", recv.URL)
	t.Setenv("OTEL_LOGS_EXPORTER", "none")

	a := New(WithAppName("none"), WithAppLogger(jsonLogger(io.Discard)))
	if a.(*app).logsExport != nil {
		t.Fatal("OTEL_LOGS_EXPORTER=none, so no logs pipeline should be built")
	}

	if err := a.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := a.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	if n := len(recv.records()); n != 0 {
		t.Fatalf("got %d log records, want none: %v", n, recv.bodies())
	}
}

func TestAppStopFlushesLogs(t *testing.T) {
	clearLogsEnv(t)

	recv := newLogsReceiver(t)

	a := New(WithAppName("flushed"), WithAppLogger(jsonLogger(io.Discard)),
		WithAppLogsExportConfig(LogsExportConfig{Endpoint: recv.URL + "/v1/logs"}))

	if err := a.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	a.Logger().Info("just before stop")

	if err := a.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	bodies := recv.bodies()
	for _, want := range []string{"just before stop", "application stopped"} {
		if !slices.Contains(bodies, want) {
			t.Errorf("no record %q by the time Stop returned: %v", want, bodies)
		}
	}
}

func TestAppForeignLoggerSkipsLogExport(t *testing.T) {
	clearLogsEnv(t)

	recv := newLogsReceiver(t)

	capture, ok := logger.NewTestLogger().(*logger.TestLogger)
	if !ok {
		t.Fatal("logger.NewTestLogger did not return a *logger.TestLogger")
	}

	a := New(WithAppName("foreign"), WithAppLogger(capture),
		WithAppLogsExportConfig(LogsExportConfig{Endpoint: recv.URL + "/v1/logs"}))
	if a.(*app).logsExport != nil {
		t.Fatal("the capture logger can't carry a sink, so no pipeline should be kept")
	}

	if !capture.AssertHasLog("WARN", "telemetry: log export needs a go-utils logger, so logs are not exported") {
		t.Fatalf("no foreign-logger warning in %v", capture.GetLogs())
	}

	if err := a.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	a.Logger().Info("not exported")

	if err := a.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	if n := len(recv.records()); n != 0 {
		t.Fatalf("got %d log records, want none: %v", n, recv.bodies())
	}
}

// A dead collector fails every export, the failure warning is logged through
// the exported logger, and that export fails too. The rate limit keeps it to
// one line.
func TestAppDeadLogEndpointWarnsOnce(t *testing.T) {
	clearLogsEnv(t)

	// Export often and give up fast, so a loop would show up as many lines
	// within the test.
	t.Setenv("OTEL_BLRP_SCHEDULE_DELAY", "50")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_TIMEOUT", "200")

	var buf lockedBuffer

	a := New(WithAppName("dead"), WithAppLogger(jsonLogger(&buf)),
		WithAppLogsExportConfig(LogsExportConfig{Endpoint: "http://127.0.0.1:1/v1/logs"}))
	if a.(*app).logsExport == nil {
		t.Fatal("the endpoint is valid, so a pipeline should be built")
	}

	if err := a.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()

	for i := range 200 {
		<-tick.C
		a.Logger().Info("line", F("i", i))
	}

	if err := a.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	// Exports start failing within a few hundred milliseconds, well inside
	// the two seconds of logging, so zero would mean the failure never
	// reached the handler.
	if n := strings.Count(buf.String(), "telemetry: export failed"); n != 1 {
		t.Fatalf("%d export failure warnings, want exactly one:\n%s", n, buf.String())
	}
}

// Review Focus 2: a goroutine that logs after Stop must not panic or reach
// the shut provider.
func TestAppLogAfterStopIsQuiet(t *testing.T) {
	clearLogsEnv(t)

	recv := newLogsReceiver(t)

	a := New(WithAppName("late"), WithAppLogger(jsonLogger(io.Discard)),
		WithAppLogsExportConfig(LogsExportConfig{Endpoint: recv.URL + "/v1/logs"}))

	pipeline := a.(*app).logsExport
	if pipeline == nil {
		t.Fatal("the endpoint is set, so a pipeline should be built")
	}

	if err := a.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := a.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	before := len(recv.records())

	a.Logger().Info("late")
	pipeline.Flush()

	if after := len(recv.records()); after != before {
		t.Fatalf("records went from %d to %d after Stop, want no change", before, after)
	}

	if slices.Contains(recv.bodies(), "late") {
		t.Fatal("a line logged after Stop was exported")
	}
}

// A Start that fails before the app counts as started exports the lines that
// explain the failure, because a caller that exits on the error never reaches
// Stop. Stop would do nothing for that app anyway, so Start detaches the sink
// itself: a supervisor that retries New and Start with one logger must not
// export every line once per attempt.
func TestAppFailedStartFlushesLogs(t *testing.T) {
	clearLogsEnv(t)

	recv := newLogsReceiver(t)

	a := New(WithAppName("failed-start"), WithAppLogger(jsonLogger(io.Discard)),
		WithAppLogsExportConfig(LogsExportConfig{Endpoint: recv.URL + "/v1/logs"}))

	pipeline := a.(*app).logsExport
	if pipeline == nil {
		t.Fatal("the endpoint is set, so a pipeline should be built")
	}

	t.Cleanup(func() { _ = pipeline.Shutdown(context.Background()) })

	errRefused := errors.New("refused")

	if err := a.RegisterHookFn(PhaseBeforeStart, "refuse", func(_ context.Context, app App) error {
		app.Logger().Error("before start hook refused")

		return errRefused
	}); err != nil {
		t.Fatal(err)
	}

	if err := a.Start(t.Context()); !errors.Is(err, errRefused) {
		t.Fatalf("Start error = %v, want the hook's", err)
	}

	bodies := recv.bodies()
	for _, want := range []string{"telemetry: exporting logs over OTLP", "starting application", "before start hook refused"} {
		if !slices.Contains(bodies, want) {
			t.Errorf("no record %q by the time Start returned: %v", want, bodies)
		}
	}

	before := len(recv.records())

	// Flush would export a queued line before it returns, so a line that
	// still isn't there after it never reached the pipeline.
	a.Logger().Info("after the failed start")
	pipeline.Flush()

	if after := len(recv.records()); after != before {
		t.Fatalf("records went from %d to %d after the failed Start, want no change: %v",
			before, after, recv.bodies())
	}

	// Stop returns early for an app that never started, so it can't be what
	// detaches the sink.
	if err := a.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	a.Logger().Info("after stop")
	pipeline.Flush()

	if slices.Contains(recv.bodies(), "after stop") {
		t.Fatal("a line logged after the failed Start and Stop was exported")
	}
}

// An after-start hook failure leaves the app started, so the Stop that
// follows still runs in full. Start only flushes, and Stop exports its own
// lines before it detaches.
func TestAppAfterStartHookFailureKeepsLogExport(t *testing.T) {
	clearLogsEnv(t)

	recv := newLogsReceiver(t)

	a := New(WithAppName("after-start-failed"), WithAppLogger(jsonLogger(io.Discard)),
		WithAppLogsExportConfig(LogsExportConfig{Endpoint: recv.URL + "/v1/logs", Enabled: new(true)}))

	pipeline := a.(*app).logsExport
	if pipeline == nil {
		t.Fatal("the endpoint is set, so a pipeline should be built")
	}

	t.Cleanup(func() { _ = pipeline.Shutdown(context.Background()) })

	errRefused := errors.New("refused")

	if err := a.RegisterHookFn(PhaseAfterStart, "refuse", func(_ context.Context, app App) error {
		app.Logger().Error("after start hook refused")

		return errRefused
	}); err != nil {
		t.Fatal(err)
	}

	if err := a.Start(t.Context()); !errors.Is(err, errRefused) {
		t.Fatalf("Start error = %v, want the hook's", err)
	}

	bodies := recv.bodies()
	for _, want := range []string{"starting application", "after start hook refused"} {
		if !slices.Contains(bodies, want) {
			t.Errorf("no record %q by the time Start returned: %v", want, bodies)
		}
	}

	if err := a.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(recv.bodies(), "application stopped") {
		t.Fatalf("no record %q after Stop: %v", "application stopped", recv.bodies())
	}
}
