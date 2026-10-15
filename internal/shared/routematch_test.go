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

func TestEstablishRunsCallbacksOnce(t *testing.T) {
	_, m := NewRouteMatch(context.Background())

	var got []int

	m.OnEstablished(func(status int) { got = append(got, status) })
	m.OnEstablished(func(status int) { got = append(got, status+1) })

	if m.Established() {
		t.Fatal("Established before Establish")
	}

	m.Establish(101)
	m.Establish(200)

	if !m.Established() {
		t.Fatal("Established false after Establish")
	}

	if len(got) != 2 || got[0] != 101 || got[1] != 102 {
		t.Fatalf("callbacks ran as %v, want [101 102] once", got)
	}
}

func TestRouteMatchNilIsSafe(t *testing.T) {
	var m *RouteMatch

	m.OnEstablished(func(int) { t.Fatal("callback on nil holder") })
	m.Establish(200)

	if m.Established() {
		t.Fatal("nil holder reports established")
	}
}
