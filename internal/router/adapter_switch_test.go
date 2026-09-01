package router

import (
	"fmt"
	"os"
	"testing"

	"github.com/xraph/forge/internal/router/forgemux"
)

// TestMain lets the whole package run against a second backend:
//
//	FORGE_TEST_ADAPTER=forgemux go test ./internal/router/
//
// Every existing test becomes a forgemux test without being edited, which is
// the standard forgemux has to clear before it can become the default.
func TestMain(m *testing.M) {
	switch adapter := os.Getenv("FORGE_TEST_ADAPTER"); adapter {
	case "", "bunrouter":
		// The default assigned in router_impl.go.

	case "forgemux":
		defaultAdapterFactory = func() RouterAdapter { return forgemux.New() }

	default:
		fmt.Fprintf(os.Stderr, "unknown FORGE_TEST_ADAPTER %q\n", adapter)
		os.Exit(2)
	}

	os.Exit(m.Run())
}
