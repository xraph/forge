// Package spanerror records errors and panics on spans without their text.
// Forge never puts an error message on a span: the status description is
// the error's class and the exception event carries exception.type only.
// It imports nothing from forge, so the router and the telemetry package can
// both use it.
package spanerror

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// PanicClass is what a recovered panic is recorded as. The panic value never
// goes on the span.
const PanicClass = "panic"

const (
	exceptionEvent      = "exception"
	exceptionType       = attribute.Key("exception.type")
	exceptionStacktrace = attribute.Key("exception.stacktrace")
)

// ErrorClass names err without its message: context.Canceled or
// context.DeadlineExceeded when err is or wraps one, otherwise the Go type of
// the innermost error in its Unwrap chain, so a wrapped driver error reports
// the driver's type rather than *fmt.wrapError. Error text can quote a DSN, a
// credential or a bound query value, so forge puts only this on a span.
func ErrorClass(err error) string {
	if err == nil {
		return ""
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

// RecordError gives span an Error status described by err's class and adds an
// exception event that carries only exception.type. It does nothing for a nil
// err. Use it in place of span.RecordError, which copies err.Error() onto the
// span as exception.message.
func RecordError(span trace.Span, err error) {
	if err == nil {
		return
	}

	class := ErrorClass(err)

	AddException(span, class, false)
	span.SetStatus(codes.Error, class)
}

// RecordPanic gives span an Error status described as PanicClass and adds an
// exception event of that type. With stack, the event also carries the
// goroutine's stack trace, which names code locations and holds no request
// data. Call it from the deferred function that recovered the panic, so the
// stack still includes the frames that panicked.
func RecordPanic(span trace.Span, stack bool) {
	AddException(span, PanicClass, stack)
	span.SetStatus(codes.Error, PanicClass)
}

// AddException adds an exception event whose only attribute is
// exception.type, plus exception.stacktrace when stack is true. It leaves the
// span's status alone.
func AddException(span trace.Span, class string, stack bool) {
	if !span.IsRecording() {
		return
	}

	if stack {
		span.AddEvent(exceptionEvent, trace.WithAttributes(
			exceptionType.String(class),
			exceptionStacktrace.String(string(debug.Stack())),
		))

		return
	}

	span.AddEvent(exceptionEvent, trace.WithAttributes(exceptionType.String(class)))
}
