# Test Logger - Quick Start

Stop log bloat in your tests with one line.

## The Problem

```go
app := forge.NewApp(forge.AppConfig{...})
```
↓ Terminal Output:
```
2024-10-26 15:30:21.123 INFO  ▶ Starting Forge application...
2024-10-26 15:30:21.124 DEBUG ▶ Initializing router...
2024-10-26 15:30:21.124 DEBUG ▶ Setting up middleware...
(hundreds more lines)
```

## The Solution

```go
import forgetesting "github.com/xraph/forge/testing"

app := forgetesting.NewTestApp("test-app", "1.0.0")
```

↓ Terminal Output:
```
--- PASS: TestYourFeature (0.00s)
```

**That's it!** 🎉

---

## Copy-Paste Snippets

### Standard Test
```go
package mypackage_test

import (
    "testing"
    forgetesting "github.com/xraph/forge/testing"
)

func TestMyFeature(t *testing.T) {
    app := forgetesting.NewTestApp("test-app", "1.0.0")
    // ... rest of test
}
```

### Need Custom Config?
```go
app := forgetesting.NewTestAppWithConfig(forge.AppConfig{
    Name:        "test-app",
    Version:     "1.0.0",
    Environment: "test",
    // Logger auto-added
})
```

### Need to Assert Logs?
```go
import "github.com/xraph/forge/internal/logger"

testLogger := logger.NewTestLogger()
app := forgetesting.NewTestAppWithLogger("test-app", "1.0.0", testLogger)

// ... run test ...

tl := testLogger.(*logger.TestLogger)
assert.Equal(t, 1, tl.CountLogs("ERROR"))
```

---

## Migration

```bash
# Find tests that need migration
./testing/find_noisy_tests.sh

# Migrate: Add import + change one line per test
```

---

**Full docs:** `testing/README.md`

