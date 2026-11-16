package forge

import (
	"context"
	"fmt"
	"os"
	"time"
)

// telemetryFlushTimeout is the budget each telemetry flush gets at shutdown:
// the last metrics push, the span export and the log export. They run after
// the extensions have stopped, when the stop budget may already be spent, and
// they are what explains a slow or failed shutdown afterwards, so each gets a
// small budget of its own instead of whatever is left.
const telemetryFlushTimeout = 5 * time.Second

// forceExit ends the process when a second shutdown signal arrives. A
// variable so tests can observe the call instead of losing the test binary.
var forceExit = os.Exit

// errStepAbandoned reports a shutdown step that did not return in time. The
// step keeps running in its goroutine; shutdown just stops waiting for it.
type errStepAbandoned struct {
	step  string
	cause error
}

func (e *errStepAbandoned) Error() string {
	return fmt.Sprintf("%s did not finish before the stop deadline (%v); abandoned", e.step, e.cause)
}

func (e *errStepAbandoned) Unwrap() error { return e.cause }

// runStep runs fn with ctx and waits for it only until ctx is done.
//
// Shutdown code is the code least likely to honour its context: a driver's
// Close, a consumer draining its queue, a hook someone wrote years ago. Called
// inline, one of those that blocks takes the whole shutdown with it and the
// process never exits. Run in a goroutine, it can only cost the time the
// budget allows. A panic is returned as an error rather than crashing a
// shutdown that is otherwise going fine.
//
// A ctx that is already done gets fn started anyway, so a step that returns
// promptly still runs, but nothing waits for it.
func runStep(ctx context.Context, step string, fn func(context.Context) error) error {
	done := make(chan error, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("%s panicked: %v", step, r)
			}
		}()

		done <- fn(ctx)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		// A step that finished at the same moment the budget ran out still
		// counts as finished.
		select {
		case err := <-done:
			return err
		default:
		}

		return &errStepAbandoned{step: step, cause: context.Cause(ctx)}
	}
}

// stopBudget returns the context the stop steps share. It is bounded by
// StopTimeout even when the caller's has no deadline, so a stuck step cannot
// hold Stop forever. A caller context that is already done (typically one an
// earlier http.Server.Shutdown spent) does not cancel the steps outright:
// they still get StopTimeout, as they did when they ran inline and ignored
// the context.
func (a *app) stopBudget(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx.Err() != nil {
		ctx = context.WithoutCancel(ctx)
	}

	return context.WithTimeout(ctx, a.stopTimeout())
}

// stopTimeout is StopTimeout, defaulting to ShutdownTimeout.
func (a *app) stopTimeout() time.Duration {
	if a.config.StopTimeout > 0 {
		return a.config.StopTimeout
	}

	if a.config.ShutdownTimeout > 0 {
		return a.config.ShutdownTimeout
	}

	return 30 * time.Second
}

// flushContext returns a fresh telemetryFlushTimeout budget that keeps ctx's
// values but not its cancellation or deadline.
func flushContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), telemetryFlushTimeout)
}
