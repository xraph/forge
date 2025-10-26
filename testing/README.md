# Forge Testing Utilities

This package provides utilities to make testing Forge applications cleaner and less noisy.

## Problem: Noisy Test Output

By default, Forge applications create a development logger that outputs colorful logs to the terminal. While great for development, this bloats test output and makes it hard to see actual test results.

## Solution: Silent Test Apps

Use the testing helpers to create apps with silent loggers:

### Quick Start

```go
import (
    "testing"
    forgetesting "github.com/xraph/forge/testing"
)

func TestYourFeature(t *testing.T) {
    // ✅ Silent - no log bloat
    app := forgetesting.NewTestApp("test-app", "1.0.0")
    
    // Continue testing as normal
}
```

### Capturing Logs for Assertions

When you need to test that specific logs were generated:

```go
import (
    "testing"
    "github.com/xraph/forge/internal/logger"
    forgetesting "github.com/xraph/forge/testing"
)

func TestLogging(t *testing.T) {
    // Create a test logger that captures logs
    testLogger := logger.NewTestLogger()
    
    // Use it in your app
    app := forgetesting.NewTestAppWithLogger("test-app", "1.0.0", testLogger)
    
    // ... run tests ...
    
    // Assert on captured logs
    logs := testLogger.(*logger.TestLogger).GetLogsByLevel("ERROR")
    assert.Len(t, logs, 1)
}
```

### Custom Configuration with Silent Logging

For complex test setups:

```go
func TestWithCustomConfig(t *testing.T) {
    app := forgetesting.NewTestAppWithConfig(forge.AppConfig{
        Name:        "test-app",
        Version:     "1.0.0",
        Environment: "test",
        // Logger is automatically set to NoopLogger if not provided
    })
}
```

## Available Functions

### `NewTestApp(name, version string) forge.App`
Creates a test app with a silent logger. **Use this by default.**

### `NewQuietApp(name, version string) forge.App`
Alias for `NewTestApp` - same behavior.

### `NewTestAppWithLogger(name, version string, log forge.Logger) forge.App`
Creates a test app with a custom logger. Use when you need to capture/assert logs.

### `NewTestAppWithConfig(config forge.AppConfig) forge.App`
Creates a test app with full config control. Automatically adds NoopLogger if none provided.

## Logger Types

### `logger.NewNoopLogger()`
Discards all logs - perfect for tests where you don't care about log output.

### `logger.NewTestLogger()`
Captures logs in memory for assertions. Methods:
- `GetLogs() []LogEntry` - Get all captured logs
- `GetLogsByLevel(level string) []LogEntry` - Filter by level
- `AssertHasLog(level, message string) bool` - Check if log exists
- `CountLogs(level string) int` - Count logs at level
- `Clear()` - Clear captured logs

## Migration Guide

### Before (Noisy)
```go
app := forge.NewApp(forge.AppConfig{
    Name:    "test-app",
    Version: "1.0.0",
})
```

### After (Silent)
```go
app := forgetesting.NewTestApp("test-app", "1.0.0")
```

## Best Practices

1. **Default to silent**: Use `NewTestApp()` for 95% of tests
2. **Assert logs when needed**: Use `NewTestLogger()` only when testing logging behavior
3. **One-line apps**: Keep test app creation concise
4. **Consistent naming**: Use descriptive app names in tests for debugging

## Example: Complete Test File

```go
package mypackage_test

import (
    "context"
    "testing"
    
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    forgetesting "github.com/xraph/forge/testing"
)

func TestFeatureA(t *testing.T) {
    app := forgetesting.NewTestApp("test-app", "1.0.0")
    
    err := app.Start(context.Background())
    require.NoError(t, err)
    
    // Your tests...
    
    err = app.Stop(context.Background())
    require.NoError(t, err)
}

func TestFeatureB_WithLogging(t *testing.T) {
    testLogger := logger.NewTestLogger()
    app := forgetesting.NewTestAppWithLogger("test-app", "1.0.0", testLogger)
    
    // ... run tests that should log ...
    
    tl := testLogger.(*logger.TestLogger)
    assert.True(t, tl.AssertHasLog("ERROR", "expected error"))
    assert.Equal(t, 1, tl.CountLogs("ERROR"))
}
```

## Performance Note

`NoopLogger` has zero allocation overhead - it's just empty function calls that get inlined by the compiler. Using it in tests has no performance impact compared to a real logger.

