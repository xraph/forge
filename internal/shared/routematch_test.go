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

// Tracing and HTTP metrics each register one callback per request, profiling
// a third on a labelled one, and they run in registration order. Three
// registrations must not allocate.
func TestOnEstablishedRunsInOrderWithoutAllocating(t *testing.T) {
	var order []int

	first := func(int) { order = append(order, 1) }
	second := func(int) { order = append(order, 2) }
	third := func(int) { order = append(order, 3) }

	m := &RouteMatch{}
	m.OnEstablished(first)
	m.OnEstablished(second)
	m.OnEstablished(third)
	m.Establish(101)

	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Fatalf("callbacks ran as %v, want [1 2 3]", order)
	}

	ctx := context.Background()

	holderOnly := testing.AllocsPerRun(100, func() {
		_, h := NewRouteMatch(ctx)
		_ = h
	})

	withCallbacks := testing.AllocsPerRun(100, func() {
		_, h := NewRouteMatch(ctx)
		h.OnEstablished(first)
		h.OnEstablished(second)
		h.OnEstablished(third)
	})

	if withCallbacks != holderOnly {
		t.Fatalf("three OnEstablished calls added %.0f allocations, want 0", withCallbacks-holderOnly)
	}
}

func TestMatchStampsAndRunsCallbacksInOrder(t *testing.T) {
	var got []string

	m := &RouteMatch{}
	m.OnMatched(func(p string) { got = append(got, "a:"+p) })
	m.OnMatched(func(p string) { got = append(got, "b:"+p) })
	m.Match("/users/:id", KindSSE)

	if m.Pattern != "/users/:id" || m.Kind != KindSSE {
		t.Fatalf("holder = %q %v, want /users/:id sse", m.Pattern, m.Kind)
	}

	if len(got) != 2 || got[0] != "a:/users/:id" || got[1] != "b:/users/:id" {
		t.Fatalf("callbacks ran as %v", got)
	}
}

func TestMatchNilHolderIsSafe(t *testing.T) {
	var m *RouteMatch

	m.OnMatched(func(string) { t.Fatal("callback ran on a nil holder") })
	m.Match("/x", KindHTTP)
}

// Profiling registers one OnMatched callback per labelled request; it must
// not allocate on top of the holder.
func TestOnMatchedOneCallbackDoesNotAllocate(t *testing.T) {
	ctx := context.Background()
	fn := func(string) {}

	holderOnly := testing.AllocsPerRun(100, func() {
		_, h := NewRouteMatch(ctx)
		_ = h
	})

	withCallback := testing.AllocsPerRun(100, func() {
		_, h := NewRouteMatch(ctx)
		h.OnMatched(fn)
	})

	if withCallback != holderOnly {
		t.Fatalf("one OnMatched call added %.0f allocations, want 0", withCallback-holderOnly)
	}
}
