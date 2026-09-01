package router

import (
	"fmt"
	"os"
	"testing"
)

// TestMain lets the whole package run against either backend:
//
//	go test ./internal/router/                          # forgemux, the default
//	FORGE_TEST_ADAPTER=bunrouter go test ./internal/router/
//
// Both must stay green. forgemux earned the default by passing every test
// bunrouter passes; keeping bunrouter runnable is what makes that claim
// checkable, and it is the oracle the differential fuzzer compares against.
func TestMain(m *testing.M) {
	switch adapter := os.Getenv("FORGE_TEST_ADAPTER"); adapter {
	case "", "forgemux":
		// The default assigned in router_impl.go.

	case "bunrouter":
		defaultAdapterFactory = func() RouterAdapter { return NewBunRouterAdapter() }

	default:
		fmt.Fprintf(os.Stderr, "unknown FORGE_TEST_ADAPTER %q\n", adapter)
		os.Exit(2)
	}

	os.Exit(m.Run())
}
