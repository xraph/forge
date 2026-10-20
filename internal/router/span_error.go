package router

import (
	"context"
	"errors"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge/internal/shared"
	"github.com/xraph/forge/internal/telemetry/spanerror"
)

// recordSpanPanic adds a recovered panic to the request's span as an
// exception event of type spanerror.PanicClass, with the stack trace of the
// goroutine that panicked. The panic value stays off the span; the recovery
// logs it.
func recordSpanPanic(ctx context.Context, _ any) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	spanerror.AddException(span, spanerror.PanicClass, true)
}

// recordSpanError adds an exception event to the request's span when err
// maps to a 5xx. The event carries the error's class (spanerror.ErrorClass)
// and not its message, since a handler error often wraps a driver error that
// quotes bound values. Client errors are part of normal traffic and would
// only add noise. The status is the tracing middleware's to set.
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
		spanerror.AddException(span, spanerror.ErrorClass(err), false)
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
