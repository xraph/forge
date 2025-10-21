package graphql

import (
	"context"
	"testing"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"github.com/xraph/forge/v2"
)

func TestObservabilityMiddleware(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()

	mw := observabilityMiddleware(logger, metrics, config)
	if mw == nil {
		t.Fatal("observabilityMiddleware returned nil")
	}

	// Create a test operation context
	ctx := context.Background()
	opCtx := &graphql.OperationContext{
		OperationName: "testOperation",
		Operation: &ast.OperationDefinition{
			Operation: ast.Query,
		},
		Stats: graphql.Stats{
			OperationStart: time.Now(),
		},
	}
	ctx = graphql.WithOperationContext(ctx, opCtx)

	// Test middleware wraps handler
	called := false
	handler := mw(ctx, func(ctx context.Context) graphql.ResponseHandler {
		called = true
		return func(ctx context.Context) *graphql.Response {
			return &graphql.Response{}
		}
	})

	if handler == nil {
		t.Fatal("middleware returned nil handler")
	}

	resp := handler(ctx)
	if resp == nil {
		t.Fatal("handler returned nil response")
	}

	if !called {
		t.Error("middleware did not call next handler")
	}
}

func TestResponseMiddleware(t *testing.T) {
	logger := forge.NewNoopLogger()
	config := DefaultConfig()
	config.LogSlowQueries = true
	config.SlowQueryThreshold = 1 * time.Millisecond

	mw := responseMiddleware(config, logger)
	if mw == nil {
		t.Fatal("responseMiddleware returned nil")
	}

	// Create a test operation context
	ctx := context.Background()
	opCtx := &graphql.OperationContext{
		OperationName: "testOperation",
		RawQuery:      "{ test }",
		Stats: graphql.Stats{
			OperationStart: time.Now().Add(-100 * time.Millisecond),
		},
	}
	ctx = graphql.WithOperationContext(ctx, opCtx)

	// Test middleware wraps handler
	handler := mw(ctx, func(ctx context.Context) *graphql.Response {
		return &graphql.Response{
			Data: []byte(`{"test":"value"}`),
		}
	})

	if handler == nil {
		t.Fatal("middleware returned nil response")
	}

	if handler.Data == nil {
		t.Error("response data is nil")
	}
}

func TestResponseMiddleware_WithErrors(t *testing.T) {
	logger := forge.NewNoopLogger()
	config := DefaultConfig()
	config.EnableLogging = true

	mw := responseMiddleware(config, logger)

	ctx := context.Background()
	opCtx := &graphql.OperationContext{
		OperationName: "testOperation",
		Stats: graphql.Stats{
			OperationStart: time.Now(),
		},
	}
	ctx = graphql.WithOperationContext(ctx, opCtx)

	// Test with errors in response
	handler := mw(ctx, func(ctx context.Context) *graphql.Response {
		return &graphql.Response{
			Errors: gqlerror.List{
				&gqlerror.Error{Message: "test error"},
			},
		}
	})

	if handler == nil {
		t.Fatal("middleware returned nil response")
	}

	if len(handler.Errors) == 0 {
		t.Error("expected errors in response")
	}
}
