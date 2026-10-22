package telemetry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/proto"

	"github.com/xraph/forge/internal/logger"
)

// logsReceiver records OTLP/HTTP log posts. ExportLogsServiceRequest has the
// wire shape of LogsData (field 1, repeated ResourceLogs), so the test decodes
// into LogsData and keeps the collector package out of the build.
type logsReceiver struct {
	srv     *httptest.Server
	mu      sync.Mutex
	got     []*logspb.LogsData
	paths   []string
	headers []http.Header
}

func newLogsReceiver(t *testing.T) *logsReceiver {
	t.Helper()

	r := &logsReceiver{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)

		var ld logspb.LogsData
		if err := proto.Unmarshal(body, &ld); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		r.mu.Lock()
		r.got = append(r.got, &ld)
		r.paths = append(r.paths, req.URL.Path)
		r.headers = append(r.headers, req.Header.Clone())
		r.mu.Unlock()

		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(r.srv.Close)

	return r
}

// bodies returns the body of every record received, in order.
func (r *logsReceiver) bodies() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []string

	for _, ld := range r.got {
		for _, rl := range ld.GetResourceLogs() {
			for _, sl := range rl.GetScopeLogs() {
				for _, lr := range sl.GetLogRecords() {
					out = append(out, lr.GetBody().GetStringValue())
				}
			}
		}
	}

	return out
}

// newHungReceiver accepts every request and never answers until the test
// ends.
func newHungReceiver(t *testing.T) string {
	t.Helper()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		select {
		case <-release:
		case <-req.Context().Done():
		}
	}))
	// Cleanups run last in, first out: release the handlers, then close.
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	return srv.URL + "/v1/logs"
}

func newTestLogsPipeline(t *testing.T, endpoint string, headers map[string]string) *LogsPipeline {
	t.Helper()

	res := resource.NewSchemaless(attribute.String("service.name", "logs-test"))

	p, err := NewLogsPipeline(t.Context(), LogsSettings{Enabled: true, Endpoint: endpoint, Headers: headers},
		res, logger.NewNoopLogger())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_ = p.Shutdown(ctx)
	})

	return p
}

func newDiscardLogger() logger.Logger {
	return logger.New(logger.Config{Format: logger.FormatJSON, Output: io.Discard})
}

func TestLogsPipelineExportsToEndpoint(t *testing.T) {
	recv := newLogsReceiver(t)
	p := newTestLogsPipeline(t, recv.srv.URL+"/v1/logs", map[string]string{"x-k": "v"})
	l := newDiscardLogger()

	if err := p.Attach(l); err != nil {
		t.Fatal(err)
	}

	l.Info("hello")

	if err := p.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}

	if got := recv.bodies(); len(got) != 1 || got[0] != "hello" {
		t.Fatalf("bodies = %q, want [hello]", got)
	}

	recv.mu.Lock()
	defer recv.mu.Unlock()

	if recv.paths[0] != "/v1/logs" {
		t.Errorf("path = %q, want /v1/logs", recv.paths[0])
	}

	if recv.headers[0].Get("X-K") != "v" {
		t.Errorf("headers = %v, want X-K: v", recv.headers[0])
	}

	var serviceName string

	for _, kv := range recv.got[0].GetResourceLogs()[0].GetResource().GetAttributes() {
		if kv.GetKey() == "service.name" {
			serviceName = kv.GetValue().GetStringValue()
		}
	}

	if serviceName != "logs-test" {
		t.Errorf("service.name = %q, want logs-test", serviceName)
	}
}

func TestLogsPipelineAttachUnsupported(t *testing.T) {
	recv := newLogsReceiver(t)
	p := newTestLogsPipeline(t, recv.srv.URL+"/v1/logs", nil)

	err := p.Attach(logger.NewNoopLogger())
	if !errors.Is(err, logger.ErrSinksUnsupported) {
		t.Fatalf("err = %v, want ErrSinksUnsupported", err)
	}
}

func TestLogsPipelineShutdownDetaches(t *testing.T) {
	recv := newLogsReceiver(t)
	p := newTestLogsPipeline(t, recv.srv.URL+"/v1/logs", nil)
	l := newDiscardLogger()

	if err := p.Attach(l); err != nil {
		t.Fatal(err)
	}

	l.Info("before")

	if err := p.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}

	// A named line that still reached the sink would put its name in the
	// scope cache, whatever the shut provider then did with the record.
	l.Named("after.shutdown").Info("after")

	p.sink.mu.RLock()
	_, reached := p.sink.scopes["after.shutdown"]
	p.sink.mu.RUnlock()

	if reached {
		t.Error("a line logged after Shutdown reached the sink")
	}

	if got := recv.bodies(); len(got) != 1 || got[0] != "before" {
		t.Fatalf("bodies = %q, want [before]", got)
	}
}

func TestLogsPipelineRejectsSchemelessEndpoint(t *testing.T) {
	_, err := NewLogsPipeline(t.Context(), LogsSettings{Enabled: true, Endpoint: "localhost:4318"},
		resource.Empty(), logger.NewNoopLogger())
	if !errors.Is(err, ErrInvalidEndpoint) {
		t.Fatalf("err = %v, want ErrInvalidEndpoint", err)
	}
}

func TestLogsPipelineNilShutdown(t *testing.T) {
	var p *LogsPipeline

	if err := p.Shutdown(t.Context()); err != nil {
		t.Fatalf("nil Shutdown = %v, want nil", err)
	}
}

func TestLogsPipelineHungCollectorDoesNotBlockLogging(t *testing.T) {
	p := newTestLogsPipeline(t, newHungReceiver(t), nil)
	l := newDiscardLogger()

	if err := p.Attach(l); err != nil {
		t.Fatal(err)
	}

	start := time.Now()

	for range 5000 {
		l.Info("line", logger.String("k", "v"))
	}

	if took := time.Since(start); took >= 500*time.Millisecond {
		t.Fatalf("5000 lines took %v against a hung collector, want under 500ms", took)
	}
}

func TestLogsPipelineFlushIsCapped(t *testing.T) {
	p := newTestLogsPipeline(t, newHungReceiver(t), nil)
	l := newDiscardLogger()

	if err := p.Attach(l); err != nil {
		t.Fatal(err)
	}

	l.Info("stuck")

	start := time.Now()

	p.Flush()

	took := time.Since(start)
	if took >= 2500*time.Millisecond {
		t.Fatalf("Flush took %v against a hung collector, want under 2.5s", took)
	}

	// A Flush that returned at once never waited on the hung export, and the
	// cap went untested.
	if took < time.Second {
		t.Fatalf("Flush took %v, want it to wait on the hung export until the cap", took)
	}
}
