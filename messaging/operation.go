package messaging

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	gometrics "github.com/xraph/go-utils/metrics"

	"github.com/xraph/forge/internal/metrics/vec"
)

// Operation is one started create, send or process. End it once; a second
// End does nothing. A nil *Operation is valid and End on it does nothing.
type Operation struct {
	span   trace.Span
	hist   *vec.Vec[gometrics.Histogram]
	labels [4]string // system, destination, operation name, operation type
	start  time.Time
	ended  atomic.Bool
}

// End ends the span and records the duration. A non-nil err sets error.type,
// records the error on the span and gives it an Error status.
func (o *Operation) End(err error) {
	if o == nil || o.ended.Swap(true) {
		return
	}

	errType := ""
	if err != nil {
		errType = errorType(err)
	}

	if o.span != nil {
		if err != nil {
			o.span.RecordError(err)
			o.span.SetStatus(codes.Error, err.Error())
			o.span.SetAttributes(attribute.String("error.type", errType))
		}

		o.span.End()
	}

	if o.hist != nil {
		o.hist.With(o.labels[0], o.labels[1], o.labels[2], o.labels[3], errType).
			Observe(time.Since(o.start).Seconds())
	}
}

// errorType names err for error.type. Context errors get their own names;
// anything else is the type of the innermost error, so a wrapped broker error
// reports the broker's type rather than *fmt.wrapError.
func errorType(err error) string {
	// A panic keeps its own name even when the panic value is an error, which
	// PanicError.Unwrap would otherwise let win.
	var pe *PanicError
	if errors.As(err, &pe) {
		return "*messaging.PanicError"
	}

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "context.DeadlineExceeded"
	case errors.Is(err, context.Canceled):
		return "context.Canceled"
	}

	for next := errors.Unwrap(err); next != nil; next = errors.Unwrap(err) {
		err = next
	}

	return fmt.Sprintf("%T", err)
}
