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

func TestRouteMatchFromContextAbsent(t *testing.T) {
	if RouteMatchFromContext(context.Background()) != nil {
		t.Fatal("want nil when no holder is present")
	}
}
