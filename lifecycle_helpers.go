package forge

// OnStarted registers a hook that runs after the app has fully started —
// after all extensions are registered and started (PhaseAfterStart).
//
// This is a convenience wrapper around App.RegisterHookFn.
//
// Example:
//
//	forge.OnStarted(app, "log-ready", func(ctx context.Context, a forge.App) error {
//	    a.Logger().Info("Application is ready!")
//	    return nil
//	})
func OnStarted(app App, name string, fn LifecycleHook) error {
	return app.RegisterHookFn(PhaseAfterStart, name, fn)
}

// OnClose registers a hook that runs before the app stops —
// before extensions are stopped (PhaseBeforeStop).
//
// Use this for cleanup tasks that should run during graceful shutdown.
//
// Example:
//
//	forge.OnClose(app, "flush-cache", func(ctx context.Context, a forge.App) error {
//	    return cache.Flush()
//	})
func OnClose(app App, name string, fn LifecycleHook) error {
	return app.RegisterHookFn(PhaseBeforeStop, name, fn)
}

// OnBeforeRun registers a hook that runs after Start but before the HTTP
// server begins listening (PhaseBeforeRun).
//
// This is ideal for tasks that need all extensions to be ready but should
// complete before the app starts accepting requests (e.g., auto-migrations).
func OnBeforeRun(app App, name string, fn LifecycleHook) error {
	return app.RegisterHookFn(PhaseBeforeRun, name, fn)
}

// OnAfterRun registers a hook that runs after the HTTP server starts
// listening (PhaseAfterRun). Hooks at this phase run in a background
// goroutine and should be non-blocking.
func OnAfterRun(app App, name string, fn LifecycleHook) error {
	return app.RegisterHookFn(PhaseAfterRun, name, fn)
}

// OnAfterRegister registers a hook that runs after all extensions have been
// registered but before they start (PhaseAfterRegister).
func OnAfterRegister(app App, name string, fn LifecycleHook) error {
	return app.RegisterHookFn(PhaseAfterRegister, name, fn)
}
