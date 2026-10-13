package forge

import (
	"context"
	"strings"
	"testing"
	"time"
)

type nilMiddlewareExt struct{ *BaseExtension }

func (e *nilMiddlewareExt) Middlewares() []Middleware {
	return []Middleware{func(next Handler) Handler { return next }, nil}
}

// An extension that hands back a nil middleware, usually one its library
// builds later, has to stop Start with an error that names it. It used to
// panic inside the router, or hang if anything recovered that panic.
func TestStart_FailsOnNilExtensionMiddleware(t *testing.T) {
	app := newStartupApp(t, false, &nilMiddlewareExt{NewBaseExtension("nilmw", "1.0.0", "test extension")})

	type outcome struct {
		err      error
		panicked any
	}

	done := make(chan outcome, 1)

	go func() {
		var o outcome

		defer func() {
			o.panicked = recover()
			done <- o
		}()

		o.err = app.Start(context.Background())
	}()

	select {
	case o := <-done:
		if o.panicked != nil {
			t.Fatalf("Start panicked: %v", o.panicked)
		}

		if o.err == nil {
			_ = app.Stop(context.Background())

			t.Fatal("Start accepted a nil middleware")
		}

		if msg := o.err.Error(); !strings.Contains(msg, "extension nilmw") || !strings.Contains(msg, "middleware 2 of 2 is nil") {
			t.Fatalf("error does not say which middleware: %v", o.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Start hung on a nil middleware")
	}
}
