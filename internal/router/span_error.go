package router

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge/internal/shared"
)

// recordSpanPanic adds a recovered panic to the request's span, with the
// stack trace of the goroutine that panicked.
func recordSpanPanic(ctx context.Context, rec any) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	span.RecordError(fmt.Errorf("panic: %v", rec), trace.WithStackTrace(true))
}

// recordSpanError adds err to the request's span when it maps to a 5xx.
// Client errors are part of normal traffic and would only add noise.
func recordSpanError(ctx context.Context, err error) {
	if err == nil {
		return
	}

	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	status := http.StatusInternalServerError

	var sc interface{ StatusCode() int }
	if errors.As(err, &sc) {
		status = sc.StatusCode()
	}

	if status >= http.StatusInternalServerError {
		span.RecordError(err)
	}
}

// spanErrorHandler records errors on the span before a custom ErrorHandler
// formats them. Wrapping once at construction covers every call site.
type spanErrorHandler struct {
	inner shared.ErrorHandler
}

func (h spanErrorHandler) HandleError(ctx context.Context, err error) error {
	recordSpanError(ctx, err)

	return h.inner.HandleError(ctx, err)
}

func wrapSpanErrors(h shared.ErrorHandler) shared.ErrorHandler {
	if h == nil {
		return nil
	}

	if _, already := h.(spanErrorHandler); already {
		return h
	}

	return spanErrorHandler{inner: h}
}
