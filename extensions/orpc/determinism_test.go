package orpc

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/xraph/forge"
)

// Methods live in a map keyed by name, and Go randomises map iteration, so
// every walk of that map into a slice has to sort or it hands back a different
// order each call. Twelve entries because a map small enough for one bucket
// (<= 8) only rotates, which passes by luck too often to be a guard.
const determinismRuns = 64

func registerN(t *testing.T, s ORPC, n int) {
	t.Helper()

	for i := range n {
		name := fmt.Sprintf("svc.method%02d", (i*7)%n)
		err := s.RegisterMethod(&Method{
			Name:        name,
			Description: "d",
			Handler:     func(ctx any, params any) (any, error) { return nil, nil },
		})
		if err != nil {
			t.Fatalf("RegisterMethod(%q): %v", name, err)
		}
	}
}

func TestListMethods_Deterministic(t *testing.T) {
	server := NewORPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())
	registerN(t, server, 12)

	got := func() []string {
		out := make([]string, 0, 12)
		for _, m := range server.ListMethods() {
			out = append(out, m.Name)
		}

		return out
	}

	want := got()
	if len(want) != 12 {
		t.Fatalf("got %d methods, want 12", len(want))
	}

	if !slices.IsSorted(want) {
		t.Errorf("ListMethods is not sorted by name: %v", want)
	}

	for run := range determinismRuns {
		if !slices.Equal(got(), want) {
			t.Fatalf("run %d: ListMethods order is not stable\n got: %v\nwant: %v", run, got(), want)
		}
	}
}

// OpenRPC's `methods` is a JSON array, so its order is part of the served
// document, exactly like the AsyncAPI operation message list.
func TestOpenRPCDocument_Deterministic(t *testing.T) {
	server := NewORPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())
	registerN(t, server, 12)

	got := func() []string {
		doc := server.OpenRPCDocument()

		out := make([]string, 0, len(doc.Methods))
		for _, m := range doc.Methods {
			out = append(out, m.Name)
		}

		return out
	}

	want := got()
	if len(want) != 12 {
		t.Fatalf("got %d methods, want 12", len(want))
	}

	if !slices.IsSorted(want) {
		t.Errorf("OpenRPC methods are not sorted by name: %v", want)
	}

	for run := range determinismRuns {
		if !slices.Equal(got(), want) {
			t.Fatalf("run %d: OpenRPC method order is not stable\n got: %v\nwant: %v", run, got(), want)
		}
	}
}

// The query string is part of the request URL. An order that changes per call
// produces two cache entries for one logical request, and breaks any upstream
// that signs the raw query.
func TestBuildHTTPRequest_QueryOrderDeterministic(t *testing.T) {
	s, ok := NewORPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics()).(*server)
	if !ok {
		t.Fatal("NewORPCServer did not return *server")
	}

	route := forge.RouteInfo{Method: "GET", Path: "/items", Name: "items.list"}

	query := map[string]any{}
	for i := range 12 {
		query[fmt.Sprintf("p%02d", (i*7)%12)] = i
	}

	params := map[string]any{"query": query}

	raw := func() string {
		req, err := s.buildHTTPRequest(context.Background(), route, params)
		if err != nil {
			t.Fatalf("buildHTTPRequest: %v", err)
		}

		return req.URL.RawQuery
	}

	want := raw()

	keys := strings.Split(want, "&")
	if !slices.IsSorted(keys) {
		t.Errorf("query parameters are not in sorted order: %s", want)
	}

	for run := range determinismRuns {
		if got := raw(); got != want {
			t.Fatalf("run %d: query string is not stable\n got: %s\nwant: %s", run, got, want)
		}
	}
}
