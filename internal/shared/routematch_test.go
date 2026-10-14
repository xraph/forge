package shared

import (
	"context"
	"testing"
)

func TestEnsureRouteMatchReusesHolder(t *testing.T) {
	ctx, first := EnsureRouteMatch(context.Background())
	ctx2, second := EnsureRouteMatch(ctx)

	if first != second {
		t.Fatal("EnsureRouteMatch created a second holder on a context that had one")
	}

	if ctx2 != ctx {
		t.Fatal("EnsureRouteMatch should return the same context when a holder exists")
	}
}

func TestNewRouteMatchReplacesHolder(t *testing.T) {
	ctx, outer := EnsureRouteMatch(context.Background())
	ctx2, inner := NewRouteMatch(ctx)

	if inner == outer {
		t.Fatal("NewRouteMatch reused the holder already on the context")
	}

	if RouteMatchFromContext(ctx2) != inner {
		t.Fatal("the new context should carry the new holder")
	}

	if RouteMatchFromContext(ctx) != outer {
		t.Fatal("the original context should keep its own holder")
	}
}

func TestRouteMatchFromContextAbsent(t *testing.T) {
	if RouteMatchFromContext(context.Background()) != nil {
		t.Fatal("want nil when no holder is present")
	}
}
